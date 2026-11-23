# Operations

Runbooks for the parts designed to break. This doc owns *procedures*; behavior and contracts live in their owning docs and are linked, never restated.

## Detection: how breakage is noticed

| Signal | Source | Cadence |
|---|---|---|
| CLI adapter health | Gated live tests (`WEEB_LIVE=1 go test ./internal/provider/... -run Live`) | Locally on demand |
| Provider-status table | `.github/workflows/provider-live.yml` runs `cli/scripts/provider-status.sh` daily; a failing provider turns the run red and rewrites the root README table | Daily CI |
| App adapter health | **Planned:** gated Dart live tests mirroring the CLI's `TestLive`, required by M1 acceptance ([roadmap.md](roadmap.md#m1--app-skeleton--watch-path)) | On app dev + release |
| User-visible ambient signal | "Providers need attention" chip ([ui-design.md](ui-design.md#error-empty-loading--offline-states)) | Continuous |
| Sync quota health | Flush errors surface in the Sync settings row; outbox retry counts as the alert | Continuous |

## MegaPlay key rotation runbook

**Symptom:** every stream on every provider fails at resolve time (410 / empty sources) while searches and episode lists still work — the resolver, not the scrapers, is stale.

1. Confirm scope: run the CLI live tests. `megaplay` (and therefore all five adapters) failing while HTML parsing tests pass = keys.
2. Read the current key/IV from `https://megaplay.buzz/lib/app.main.js` (the player JS; the AGENTS.md note applies).
3. Update **both** constant sites: `cli/internal/provider/megaplay/megaplay.go` (`sourcesKey`/`sourcesIV`) today; `app/lib/megaplay/` when the Flutter port exists. They are duplicated constants by design (no shared source — two languages, one value); this runbook is the sync mechanism.
4. Verify: CLI `WEEB_LIVE=1` live tests green, then a real play through the TUI; same via the Dart live tests when they exist.
5. Release a PATCH ([decisions.md D-017](decisions.md#d-017--milestone-anchored-semver-tag-releases-on-main)) — a key rotation is exactly what PATCH exists for.

## Provider death playbook

**Triage:** if a fix needs browser automation or JS execution, the provider is **dead** — the constraint wins ([decisions.md D-007](decisions.md#d-007--plain-http-scraping-no-browser-automation)); do not be tempted.

- **Fix path (normal):** selectors/hosts rotate — patch the adapter (Go + Dart), gated live test, PATCH release.
- **Domain migration:** update the adapter's base URL (`-base` for gogo on the CLI). Existing progress/download rows keyed to the old domain **orphan by design** ([sync.md § Key stability policy](sync.md#doc-ids)) — accepted loss; the user's local rows keep working until they age out of history. Do not build a re-keying migration.
- **Retirement:** keep the adapter in the registry for one release with `last_error` visible, then remove it; progress rows pointing at it are harmless and stay until the user deletes them.
- **Replacement onboarding:** copy the closest existing adapter (all five share the MegaPlay tail), add gated live tests before merging, enable by default ([ui-design.md § First-run](ui-design.md#first-run-experience) rationale: a dead provider feeds the fallback system).

## Metadata & filler dependency outages

Degradation matrix (product behavior is [ui-design.md § States](ui-design.md#error-empty-loading--offline-states); this is the ops view):

| Dependency | Outage effect | Action |
|---|---|---|
| AniList | No covers/genres/scores/season rails/Trending; search + playback unaffected | Wait it out; per-device cache (7d) softens it |
| Kitsu + Jikan | No age ratings ("Not rated") | Wait; nothing gates on it (fail-open, [decisions.md D-010](decisions.md#d-010--best-effort-metadata-with-a-fail-open-age-gate)) |
| animefillerlist (Cloudflare) | Filler badges/filter absent; episodes unaffected | Check from another network; usually transient; caches (30d) bridge |
| MegaPlay | See the rotation runbook above | Immediate PATCH |

## Firebase project setup & rules deploy

One-time, personal project:

1. Create a Firebase project (Spark plan is sufficient — [sync.md § Cost & limits](sync.md#cost--limits)).
2. Enable Authentication → Google; add Sign in with Apple on iOS builds ([sync.md § Accounts & auth](sync.md#accounts--auth)).
3. Create Firestore (region: pick the one closest; note it — the same project serves CLI + app).
4. Deploy the security rules from [sync.md § Security rules](sync.md#security-rules) via `firebase deploy --only firestore:rules` with the rules file kept in the repo (`app/firebase/firestore.rules` when the app lands).
5. No client secrets ship with the apps: web/Firebase config keys are public identifiers; access is controlled by the rules.

**Quota overrun:** first-login merges beyond ~15k rows exceed a single day's free quota — the outbox backs off and spreads the flush across days automatically; no data is lost. Monitor via the Sync row's retry counter.

**Re-auth / token expiry:** sessions expire; the Sync row shows "Sync paused — sign in again"; nothing else changes. Never interrupt playback for auth.

## Backup & export

- **Local:** the DB export/import shipping in M4 ([features.md § Sync](features.md#sync)) doubles as the backup: one file, restores anywhere, no account.
- **Cloud:** before exercising "Delete cloud data" or an account switch, export locally first — LWW cannot distinguish a wipe from a legitimate write, so the cloud copy has no undo.
- **SQLite file locations:** documented in AGENTS.md (`store.DefaultPath()`); copy the file only with the app closed (WAL).

## Release & sideload procedure

A release = tag on `main` + one-paragraph note + built artifacts attached ([decisions.md D-017](decisions.md#d-017--milestone-anchored-semver-tag-releases-on-main)). Promote from `dev` per the AGENTS.md branch workflow. Version numbering covers CLI + app jointly (one product, one tag).

| Platform | Delivery |
|---|---|
| macOS | Signed-notarized (or ad-hoc) `.app` + `.dmg`; Gatekeeper note in the release text if unsigned |
| Linux | `tar.zst` of the bundled build; libmpv ships via `media_kit_libs_linux_video` |
| Windows | Zip of the build; libmpv bundled; SmartScreen note in the release text |
| Android / Fire TV | One APK, `adb install` (or file manager); unknown-sources once; the same APK sideloads on Fire TV sticks |
| iOS | Free provisioning signs 7 days for ≤3 devices — **resign weekly**; a paid account removes the cadence but is not assumed ([overview.md § Distribution](overview.md#distribution-stance)) |

**Rollback:** the tag is the rollback — check out the previous tag, rebuild, reinstall.

## The stop condition

If a provider, or the ecosystem generally, moves to challenges that plain HTTP + HTML parsing cannot pass, **the provider is dead and the constraint wins** ([decisions.md D-007](decisions.md#d-007--plain-http-scraping-no-browser-automation)). No browser automation, no headless Chrome, ever — the product's legality and simplicity depend on it. When all providers reach that state, the app remains a local-library player and the docs stay true.
