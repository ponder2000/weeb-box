# Decision log

One paragraph per decision: context → decision → consequence. **Supersession adds a pointer; history is never rewritten** — the latest un-superseded entry is the only current reason. Other docs state the *what* and link here for the *why*.

## D-001 — Local-first, no server ever

Scraped-content products drift toward proxies, accounts, and eventually legal exposure. weeb-box commits to the opposite: every client does its own HTTP on-device, stores only in local SQLite, and treats Firebase as an opt-in relay of the user's own rows. Consequence: no CORS walls, no server bills, no takedown surface — and Tizen/webOS-style TV platforms (which require a server) are automatically out.

## D-002 — Go TUI first, Flutter app second

The terminal app shipped first to prove the hard parts — scrapers, MegaPlay resolver, resume semantics, download queue — in a codebase with zero UI-framework churn. The Flutter app now mirrors `cli/internal/` module-for-module with the Go code as reference implementation. Consequence: two scrapers to maintain (see [D-011](#d-011--cli-goes-maintenance-only-after-app-m1)), but every product decision was validated against working code before a line of Flutter existed.

## D-003 — Players: mpv on CLI, media_kit (libmpv) on app

The CLI already depends on mpv's tolerance — Referer headers, WebVTT, decoy HLS segment extensions that break stricter demuxers, and the cache tuning. media_kit wraps the same libmpv core on all six platforms with the largest cross-platform community (919 likes, 429k downloads, actively maintained in 2026), so all of that carries over 1:1 as mpv properties, and one backend means one set of playback behaviors everywhere. Consequence: a hard dependency on one Flutter package; mitigated by a thin `PlayerService` seam so a backend swap stays mechanical.

## D-004 — Opt-in Firebase sync; LWW with tombstones

Sync had to exist (multi-device is the point of a Flutter app) without a weeb-box server or forced accounts. Decision: local-only by default; opt-in sign-in (Google + Apple); last-write-wins per row on `updated_at`; deletes as 30-day-TTL tombstones. Consequence: a heavy user stores ~1.9 MB and stays comfortably inside Firestore's free tier; the one-time first-login merge is the only quota-sensitive operation.

## D-005 — Progress keyed by provider + episode_url (base64url doc IDs)

Progress identity must match the SQLite PK exactly so both clients agree on what a row is. Firestore doc IDs can't contain `/`, so `docId = base64url(provider + "\n" + episode_url)` — reversible, collision-free, debuggable, and it saves storing the URL as a field. Consequence: provider domain migrations fork rows (accepted loss; runbook in [operations.md](operations.md#provider-death-playbook)) and dub/sub are separate rows by design.

## D-006 — Mobile downloads fMP4-only; no ffmpeg on device

ffmpeg does not exist on iOS and its Android packaging is unmaintained. Decision: a Dart HLS downloader supporting fMP4 playlists only, with clear per-episode errors on TS streams; desktop uses real ffmpeg when on PATH (CLI-identical command). Consequence: some streams won't download on phones — stated honestly in the UI — but downloads never block playback, which is the product's actual pillar.

## D-007 — Plain-HTTP scraping; no browser automation

Everything is HTTP + HTML parsing; no headless browsers, no JS execution. This keeps clients lightweight and the legal/complexity surface minimal. Consequence: any provider that adds a JS challenge we can't parse is **dead** — the constraint wins (stop condition in [operations.md](operations.md#the-stop-condition)). animefillerlist's Cloudflare already demonstrates the accepted degradation: badges vanish, nothing else breaks.

## D-008 — Personal/sideload distribution; no stores, no multi-user

Scraped content fails store review and this is not a business. Decision: sideloaded builds, single-user charter, no telemetry, no monetization. Consequence: iOS free-provisioning means weekly re-signing (accepted reality, procedure in [operations.md](operations.md#release--sideload-procedure)); zero store-compliance work ever.

## D-009 — TV mode after phone and desktop; Android TV only

TV is a viewing surface and its value depends on phone/desktop existing first (downloads live there). Android TV/Fire TV are Android, so one Flutter APK covers them via a leanback entry; tvOS is not an official Flutter target and Tizen/webOS require the server D-001 forbids. Consequence: TV ships as v1.2 in the same app, and Apple/Samsung/LG TVs are permanently out.

## D-010 — Best-effort metadata with a fail-open age gate

Metadata (AniList/Kitsu/Jikan) enriches but never gates: unreachable APIs cost covers and scores, never screens. Search is never pre-filtered for adult content; opening a result is blocked only when loaded metadata says Adult — and a metadata **failure fails open** (the list renders), because a dead API must not hard-block borderline titles. The safe default (hidden) lives in a device-local setting, so the gate's worst case is one accidental tap, not a locked catalog.

## D-011 — CLI goes maintenance-only after app M1

Two full feature frontiers double every test burden. Decision: once the app's watch path (M1) ships, the CLI takes bug fixes, provider-selector fixes, and MegaPlay key rotations only; provider breakage is dual-mandatory, everything else lands app-first. Consequence: the CLI keeps 100% of today's features forever as a stable trailing client; the app is the frontier.

## D-012 — App v1 cut

v1 ships **search, slim browse, episodes, player, downloads, library, providers, settings** — find, watch, keep, control. Stats and sync move to v1.1, TV to v1.2. Everything cut is a whole subsystem with its own failure modes; everything kept is the loop a single user runs daily. Consequence: v1.0 is reachable and each later milestone is a coherent release instead of a v1 that ships half-tested.

## D-013 — Search results deduped by normalized title (app)

Five providers returning the same show as five rows is noise in a GUI. The app merges by normalized title (the same matcher the CLI's fallback already trusts), first-in-registry-order wins, and tags the row with its provider. Consequence: the TUI's per-provider rows disappear on app platforms; fallback stays automatic, so the tag is informational and the safety of the CLI's exact-match rule is preserved underneath.

## D-014 — TV mode permanently adult-locked; hide_adult is device-local

A living-room device is hand-off-safe by requirement, so TV has no adult toggle at all. And because a synced `hide_adult` would let an unlocked desktop silently unlock a phone or TV, the setting is excluded from sync and stays device-local. Consequence: one safe default everywhere, one owner-of-truth per device, and no cross-device surprise — at the cost of setting the toggle per device.

## D-015 — The one state rule

Every screen renders local truth (SQLite: progress, history, library, toggles) first and treats all network data as optional enrichment — a failure may cost a badge or a cover, never the screen; the culprit is named in human words with exactly one retry. Consequence: offline feels like a mode, not an error; provider rot never reads as "app broken". Full per-screen matrix in [ui-design.md § States](ui-design.md#error-empty-loading--offline-states).

## D-016 — No onboarding tour: working Home instead

Tours are what apps-with-servers use to justify themselves. First run is a working Home — AniList Trending (needs no scraping, so day one is never blank) plus an honest empty Continue-watching line — and sync is never prompted anywhere. Consequence: zero onboarding code, and the app explains itself by being usable; discovery of sync is the user's job in Settings.

## D-017 — Milestone-anchored SemVer; tag releases on `main`

A personal app needs no channels or codenames. `0.x` during development; `v1.0` = M3 complete; a MINOR per milestone (v1.1 sync, v1.2 TV); PATCH for provider fixes. A release is a git tag on `main` with a one-paragraph note and built artifacts attached; `dev` builds stay untagged. Consequence: changelogs read themselves ("v1.1 added sync") and rollback is a tag checkout.
