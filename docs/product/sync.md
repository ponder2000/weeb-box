# Sync plan

The cross-device contract for weeb-box's opt-in sync, and the only spec of the Firestore data model. Local-first is the default state of every client; nothing here applies until the user turns sync on in Settings.

## Scope: what syncs, what never does

| Data | Syncs? | Rule |
|---|---|---|
| `watch_progress` rows | **Yes** — the core user datum (resume positions, watched marks, history) | One doc per `(provider, episode_url)` row, **per profile** |
| `provider="local"` rows | **Never** | `episode_url` is an absolute file path — meaningless elsewhere and leaks filesystem layout. Exclusion rule: `provider == "local"` (field check, not doc-ID prefix, so the rule is enforceable in security rules) |
| `search_history` | **Yes, capped at 50** as a single doc | The cap is the bloat guard, per profile; see [§ Firestore model](#firestore-model) |
| `settings` | **Whitelist only: `quality`, `theme`, `autoplay`** | Cross-device preferences, **per profile** |
| `hide_adult` | **Never — device-local, hard rule** | An adult-enabled desktop must never silently unlock a phone or the TV; TV has no toggle at all ([features.md § TV mode](features.md#tv-mode-android-tv--google-tv--fire-tv)) |
| `download_dir` | **Never** | A macOS path is invalid on Android; device-specific by nature |
| `provider_state.enabled` | **Yes** (one small doc) | Which adapters the user wants is a preference |
| `provider_state.last_ok` / `last_error` | **Never** | Per-device network diagnostics — wrong on every other machine |
| `downloads` table | **Never** | Paths are device-specific; filesystem is truth ([structure.md § On-disk contracts](structure.md#on-disk-format-contracts)) |
| `anime_meta` + `filler:` caches | **Never** | Regenerable per-device caches (7-day / 30-day TTL); each device pays its own AniList/animefillerlist quota — trivial and by design |

## Accounts & auth

- **Local-only is the default and needs no Firebase code path at all** — sync stays completely inert until the user flips "Sync across devices" in Settings and signs in. Everything works exactly as before in SQLite.
- **Sign-in: Google everywhere; Sign in with Apple on iOS builds** (Apple guideline 4.8 requires SIWA when Google login is offered on iOS, with equal prominence). No email/password.
- **No anonymous auth.** Anonymous UIDs can't sign in from a second device, so they can't sync — the thing sync exists for. The local-only default already is the no-account experience.
- **First-login merge, per row, newest `updated_at` wins:** full pull (paged 500) → one SQLite transaction applying LWW (cloud-newer ⇒ upsert local; local-newer or cloud-missing ⇒ enqueue for push; cloud tombstone newer ⇒ delete local) → `search` union capped to 50 by `last_used` → flush the push queue. The merge runs **once per profile**; the profile list itself merges by pid — cloud profiles materialize locally, local-only profiles upload on their first flush. `provider="local"` rows are exempt from comparison entirely.
- **Sign-out** reverts the device to local-only, cloud data intact. A **"Delete cloud data"** action deletes the `users/{uid}` subtree and nothing local (the GDPR-shaped exit). Account switching re-runs the first-login merge against the new uid.

## Firestore model

```
users/{uid}                                  # shell doc {created_at} — written once, ever
users/{uid}/profiles/{pid}/progress/{docId}  # one doc per (provider, episode_url) per profile
users/{uid}/profiles/{pid}/state/settings    # whitelisted per-profile settings + schema stamp
users/{uid}/profiles/{pid}/state/search      # capped search history map, per profile
users/{uid}/state/providers                 # {gogoanime: true, hianime: false, ...} — account-level
```

**Profile scoping.** `pid` is the profile's local UUID (created on-device, stable across sync), so profiles are local-first and the cloud only mirrors them. The CLI has no profiles and maps to pid `default`, keeping old rows valid. Provider toggles stay account-level; everything a profile owns lives under its `profiles/{pid}` subtree. The kid profile is a UI-level lock ([features.md § Profiles & account](features.md#profiles--account)) — its rows look identical in the cloud.

No per-anime rollups, no stats docs, no device-registration docs — Home and Stats are computed by existing SQLite queries after merge. The shell doc is written once; **no `last_sync_at` heartbeat** (that would be a per-pull write tax).

### Doc IDs

**`docId = base64url(provider + "\n" + episode_url)`** — Firestore IDs cannot contain `/`; base64url's `[A-Za-z0-9_-]` is safe, reversible, collision-free (same key as the SQLite PK), and debuggable (`base64 -d` prints `provider\nurl`). The URL therefore never needs to be stored as a field (~50 B/doc saved).

### Field semantics

Both clients must write exactly this shape:

```jsonc
{
  "provider":       "gogoanime",
  "anime_url":     "https://…/steins-gate-0",
  "anime_title":    "Steins;Gate 0",
  "episode_number": 1,
  "position":       754.2,     // 0 with duration > 0 = FINISHED (the watched sentinel,
                               //  shared with the CLI's `w` toggle — never invent a second encoding)
  "duration":       1420.1,
  "updated_at":     1763894400,// unix seconds, the LWW clock (matches SQLite)
  "deleted":        true,       // present ONLY on tombstones
  "dev":            "mbp16"     // short device label, conflict debugging only
}
```

**Watched/finished is always `position: 0` + `duration > 0`.** A watched toggle without a known duration writes `duration: 1` (CLI rule). Because both clients share the encoding, LWW never lets them disagree about what is watched.

**Rewatch:** opening a finished episode starts at 0 and overwrites the sentinel with a live position — a resurrected row is a legitimate newer write, not a conflict.

**Key stability policy:** doc IDs embed `episode_url`, domain included. When a provider domain migrates, old-domain rows orphan and re-key as duplicates — **accepted loss**; the cleanup runbook is [operations.md § Provider death](operations.md#provider-death-playbook). Re-keying on migration would require mapping tables for no user-visible gain.

### Search doc

Single doc, map of queries → `{last_used, uses}`, **capped at 50, eviction = lowest `last_used` first**, enforced identically on every write (local and merge), query strings truncated at 128 chars at the sync boundary. Two devices racing lose only tail entries past 50 — convenience data by definition.

## Schema versioning

Field names above are frozen (`snake_case`, unix-second integers). Readers **ignore unknown fields**, so adding one is backward-compatible. `state/settings` carries a `schema: 1` stamp; a client seeing a higher stamp than it understands syncs nothing and says so in the Sync settings row rather than guessing. Dropping/renaming a field requires a stamp bump + a one-time migration note in this section's change history — expected frequency: never.

## Conflict resolution

**Last-write-wins per row, keyed identically to SQLite.** Higher `updated_at` wins; a same-second tie resolves to whichever flush lands later (same-second positions are near-identical, harmless). Clock skew is bounded by "watched more recently wins" — the semantics users expect. Two devices watching concurrently ping-pong until one stops, then converge to the last watcher.

**The active-playback rule:** a remote write for the episode *currently playing* is **deferred, not applied** — the local 5s SQLite poll owns that row until playback exits and flushes with a newer `updated_at`. Sync must never yank a position out from under a running player.

**Deletes are tombstones** (`{deleted: true, updated_at: …}` at the same doc ID): un-watching, `x`-remove-a-history. Other devices delete their local row on pull. **TTL: 30 days**, enforced server-side via a Firestore TTL policy on an `expires` field present only on tombstones. Known edge: a device offline > 30 days that still holds the row resurrects it only if the user interacts with that episode again — accepted, self-heals on the next pull.

## Write path

All mutations flow through the local store (the only SQL owner), which also writes a **local outbox** row (`sync_outbox(doc_id, op, updated_at, tries)`) inside the same SQLite transaction. The 5s playback poll touches **SQLite only** — cloud writes are never per-tick.

**Flush triggers:** episode exit / player quit (the main trigger, ≤1–2 writes per episode) · app exit/backgrounding (phone) · a 60-second tick that fires **only when the outbox is non-empty** · settings/provider/search changes (5s debounce, single small doc). Flushes batch ≤500 ops. Steady state ≈ 3 writes per episode viewed.

## Pull path

Incremental cursor: on start, foreground/resume, and every ~5 minutes while open, query `progress` `where updated_at > local_cursor` (automatic single-field index), paged 500, apply LWW in one transaction, advance the cursor. **Full pull happens only on first login per device.** The outbox — not Firestore's local persistence — is the single source of truth for pending writes, so the semantics are identical for every client including a future Go REST one.

## Offline behavior

Offline = the outbox is the queue: it survives restarts, retries with exponential backoff, and all ops are idempotent upserts/tombstones so the mobile SDK's retry layer double-firing is harmless. At launch with no network, everything renders from local truth; sync silently defers; nothing blocks.

## Security rules

```js
rules_version = '2';
service cloud.firestore {
  match /databases/{db}/documents {
    match /users/{uid} {
      // A user only ever touches their own subtree.
      allow read, write: if request.auth != null && request.auth.uid == uid;

      match /progress/{docId} {
        allow read: if request.auth != null && request.auth.uid == uid;
        allow create, update: if request.auth != null && request.auth.uid == uid
          && request.resource.data.keys().hasOnly(
               ['provider','anime_url','anime_title','episode_number',
                'position','duration','updated_at','deleted','dev'])
          && request.resource.data.provider is string
          && request.resource.data.provider.size() <= 16
          && request.resource.data.provider != 'local'   // local rows never leave the device
          && request.resource.data.anime_url.size() <= 512
          && request.resource.data.anime_title.size() <= 256
          && request.resource.data.updated_at is int
          && request.resource.data.position is number
          && request.resource.data.duration is number;
      }

      match /state/{doc} {
        allow read: if request.auth != null && request.auth.uid == uid;
        allow write: if request.auth != null && request.auth.uid == uid
          && request.resource.data.size() <= 100;   // search cap ≈ 50 + settings keys
      }
    }
  }
}
```

The field whitelist is also the blob guard: an accidental cover/base64 write is rejected at the database, not caught in review.

## CLI ↔ app interop

**Firestore is the only bridge between the Go CLI and the Flutter app** — no direct store import. The CLI stays single-profile and reads/writes the pid `default` subtree, so its rows interoperate untouched. Both compute the identical `base64url(provider + "\n" + episode_url)` doc IDs and write identical fields, so a watched episode in the TUI shows watched in the app after its next pull, and vice versa. The CLI later adopts this via a `cli/internal/sync` package speaking Firestore **REST** with a Firebase ID token (Google OAuth device-authorization flow; refresh token in the OS keychain; no Admin SDK) — the security rules see it as the same signed-in user. CLI sync status: `v1.x` ([features.md § Sync](features.md#sync)).

**One-machine coexistence:** a CLI and an app on the same device both syncing is simply two clients of one account — the same LWW rules as two devices; the shared-download-root contract is [structure.md § On-disk contracts](structure.md#on-disk-format-contracts).

## Cost & limits

Per-doc ≈ 300–400 B all-in (fields + overhead + index entries). Heavy user (150 anime, 6,000 episodes, 200 searches): **≈ 1.9 MB stored — 0.18% of the 1 GiB free tier.** Steady ops ≈ 1,000 writes + 1,100 reads/month ≈ **5% of a single day's** free write quota (Spark plan quotas are daily: 50k reads / 20k writes / 20k deletes). First-login one-time merge: 6,000 reads + ≤6,000 writes ≈ 12%/30% of one day's quota — fits in a day up to ~15k rows; beyond that the outbox spreads the flush across days on quota errors automatically.

## Failure modes

| Failure | Behavior |
|---|---|
| Token expiry / sign-in failure mid-session | Sync **silently stops**; the Settings > Sync row shows "Sync paused — sign in again"; playback/history never interrupted; re-auth only from Settings |
| Quota exceeded | Flush fails; the outbox backs off and retries over following days — nothing is lost, only delayed |
| Write rejected by rules (bug) | The op parks in the outbox with its error; the Sync row names it; never retried blindly |
| Remote schema stamp higher than local | Client syncs nothing, Sync row explains "Cloud data is from a newer app version — update the app" |
| Two devices, same episode, same second | Harmless — near-identical positions, last flush wins |

User-visible states for every row above live in [ui-design.md § States](ui-design.md#error-empty-loading--offline-states).
