# Feature inventory

The **only** place a feature's platform availability, milestone, and status are stated. Every other doc links to rows here.

## How to read this

Statuses: `shipped-CLI` (in the terminal app today) · `planned-M{n}` (app milestone, see [roadmap.md](roadmap.md)) · `v1.x` (backlog, no milestone) · `rejected` (see [overview.md § Non-goals](overview.md#non-goals)).

Platform columns: **C**LI · **A**ndroid · **i**OS · **D**esktop · **T**V. `✓` = has it, `—` = never (reason in Notes).

## Discovery

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Multi-provider search (5 adapters) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Duplicate-result merging (normalized title, registry order wins, provider tag) | — (CLI shows per-provider rows) | ✓ | ✓ | ✓ | ✓ | M1 | planned-M1 |
| Progressive results (rows appear as each provider returns; slow ones stream in after) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Genre browse (AniList) | ✓ | ✓ | ✓ | ✓ | ✓ | M3 | shipped-CLI |
| Season rails (current / next / previous) | ✓ | ✓ | ✓ | ✓ | ✓ | M3 | shipped-CLI |
| Trending row (AniList, no scraping — first-run Home filler) | — | ✓ | ✓ | ✓ | ✓ | M3 | planned-M3 |
| Recent searches | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Metadata preview on results / detail header (cover, score, genres, rating, duration) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |

**Search semantics.** The app merges results across providers by normalized title (lowercase, alphanumeric-only, trailing Dub/Sub stripped — identical to the CLI's `bestTitleMatch` normalization); the first hit in registry order (gogoanime → animekai → anizone → kissanime → hianime) wins the row, and the row is tagged with that provider. Merging is presentation-only: fallback is automatic, so the tag is informational.

## Providers

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| 5 adapters (gogoanime, animekai, anizone, kissanime, hianime) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Enable/disable per provider (persisted) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Health display (`last_ok` / `last_error`, per device) | ✓ | ✓ | ✓ | ✓ | ✓ | M3 | shipped-CLI |
| Diagnose: fixed-title search through every adapter | ✓ | ✓ | ✓ | ✓ | ✓ | M3 | shipped-CLI |
| Cross-provider stream fallback **before** play (normalized exact title match + episode number; adopt winner for the session) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Cross-provider stream fallback **mid-play** (stall/error → retry same episode on other enabled providers, keeping position, toast on switch) | — | ✓ | ✓ | ✓ | ✓ | M1 | planned-M1 |
| "Providers need attention" chip on Home when any adapter has `last_error` | — | ✓ | ✓ | ✓ | ✓ | M3 | planned-M3 |

**Mid-play fallback** is app-only in v1 (the CLI's player is an external mpv process; a mid-play retry would kill and relaunch it — not worth it there). If all providers fail mid-play: exit to the episode list with the error named and one Retry.

## Metadata

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| AniList: covers, genres, score, duration, isAdult; browse/season queries | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Age rating (Kitsu first, Jikan fallback: `G`/`PG-13`/`R`/`18+`/`Not rated`) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Per-device 7-day metadata cache (never synced) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Best-effort rendering (metadata unreachable ⇒ provider titles still render; covers/genres/scores absent) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |

## Filler

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Badges per episode: canon / mixed / filler | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Hide-filler filter (hides filler+mixed; autoplay, next/prev, and "download rest" follow visible rows) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Trust rule: a list applies only when its Max() equals that adapter's last episode number | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| AniList English-title retry when the provider title misses | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Per-device 30-day filler cache (never synced) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |

**Trust-rule scope.** The Max() check is per provider-adapter, not global: if gogoanime's list matches gogoanime's episode count, gogoanime rows get badges even while kissanime's count disagrees and kissanime gets none. animefillerlist sits behind Cloudflare and can fail at any time — that only costs the badges; the episode list is untouched.

## Playback

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Stream playback (CLI: external mpv/ffplay · app: media_kit/libmpv) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Resume (seek to saved position; within 5s of end stored as finished) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Subtitles: default WebVTT fetched with the stream Referer; HLS-embedded tracks | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Quality: auto (bandwidth probe) or 1080p/720p/480p variant pick | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Autoplay next (follows the hide-filler filter) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Next/prev episode from the player | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Watched toggle (writes position 0, duration > 0 — the shared sentinel, see [sync.md § Field semantics](sync.md#field-semantics)) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Background audio (lock-screen/media-notification controls) | n/a | ✓ | ✓ | n/a | n/a | M1 | planned-M1 |
| Local-file playback (library files + sidecar `.vtt`, resume keyed by path) | ✓ | ✓ | ✓ | ✓ | ✓ | M2 | shipped-CLI |
| Speed control | — | ✓ | ✓ | ✓ | ✓ | v1.x | v1.x |

**Rewatch semantics.** Opening a finished episode starts from position 0 (no resume); watching it again simply overwrites the finished sentinel with a live position — there is no gate, and LWW carries the new state to the cloud.

## Downloads

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| HLS download queue (serial, **stops on first error**; cancel batch; retry batch) | ✓ | ✓ | ✓ | ✓ | — | M2 | shipped-CLI |
| Skip existing files (filesystem check, not DB) | ✓ | ✓ | ✓ | ✓ | — | M2 | shipped-CLI |
| Sidecar captions: `<video>.vtt` next to the file | ✓ | ✓ | ✓ | ✓ | — | M2 | shipped-CLI |
| Desktop engine: ffmpeg when found on PATH (CLI-identical command incl. `-f mp4`, `-c copy`, Referer headers, `-allowed_segment_extensions ALL`) | ✓ | ✓ | ✓ | ✓ | — | M2 | shipped-CLI |
| Mobile/fallback engine: Dart HLS downloader, **fMP4 playlists only**; TS streams fail with a clear per-episode error | — | ✓ | ✓ | ✓ | — | M2 | planned-M2 |
| Background downloads | ✓ | paused | paused | ✓ | — | M2 | shipped-CLI (desktop) |
| Download dir setting | ✓ | ✓ (SD card allowed) | — (app sandbox, fixed) | ✓ | — | M2 | shipped-CLI |

**Failure flows.** A network drop mid-segment retries the segment a bounded number of times, then fails the episode (queue stops, per CLI rule); the partial `.part` file is removed; retry re-queues. Backgrounding on mobile pauses the queue in v1 (OS limits; honest behavior over flaky background services) and resumes on foreground. **iOS sandbox:** downloads live in the app's `Documents` (visible in the Files app via `UIFileSharingEnabled`); deleting the app deletes the files — accepted and stated in the UI.

**Desktop dependency story.** media_kit bundles libmpv, so **playback has zero external dependencies**. Downloads prefer system ffmpeg on PATH (full format support); when absent, the app falls back to the Dart fMP4 downloader with a visible notice. No `-check` boot screen in the app — missing-ffmpeg surfaces inline in Downloads settings.

**Windows notes.** `safeName` collapses non-ASCII titles to `_`, which can collide two anime into one directory on Windows (CLI gotcha, inherited); the scanner treats the filesystem as truth so a collision shows up as merged episodes, not data loss. In-app progress polling runs in-process via media_kit — the CLI's no-IPC-on-Windows limitation does not apply.

## Library

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Filesystem scan (`mp4/mkv/webm/avi/mov/m4v/ts`, skips `.part`) | ✓ | ✓ | ✓ | ✓ | — | M2 | shipped-CLI |
| Local resume (`provider="local"`, keyed by absolute path — never synced) | ✓ | ✓ | ✓ | ✓ | — | M2 | shipped-CLI |
| Rescan (never deletes DB rows) | ✓ | ✓ | ✓ | ✓ | — | M2 | shipped-CLI |
| Delete a file from the library (DB row may go stale — accepted) | — | ✓ | ✓ | ✓ | — | M2 | planned-M2 |

**Cross-client contract.** If the CLI and app share one download root, both scanners parse the same filename schema (see [structure.md § On-disk contracts](structure.md#on-disk-format-contracts)); each client re-indexes what it finds. The downloads DB table is a hint, never truth.

## Sync

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Opt-in Firebase sync (progress minus local rows, capped search, whitelisted settings, provider toggles) | planned | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |
| LWW conflict resolution + delete tombstones (30-day TTL) | planned | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |
| Local DB export/import (the no-cloud twin of sync) | — | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |

Full contract: [sync.md](sync.md). CLI-side sync is v1.x (REST client, same doc IDs — see [sync.md § CLI ↔ app interop](sync.md#cli--app-interop)).

## Stats

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Watch summary by title + counts | ✓ | ✓ | ✓ | ✓ | — | v1.x | shipped-CLI |

Stats are **deferred from app v1.0** to keep the MVP cut lean (see [decisions.md D-012](decisions.md#d-012--app-v1-cut)). Computed client-side from SQLite; never aggregated in the cloud.

## TV mode (Android TV / Fire TV)

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Leanback entry, same APK, runtime TV detection | n/a | n/a | n/a | n/a | ✓ | M5 | planned-M5 |
| D-pad-only navigation (focus rings, 10-foot type, overscan-safe margins) | n/a | n/a | n/a | n/a | ✓ | M5 | planned-M5 |
| Remote key map: center=play/pause · media keys=seek · channel up/down=next/prev episode | n/a | n/a | n/a | n/a | ✓ | M5 | planned-M5 |
| Voice/system search deep-link (`ACTION_SEARCH` → `/search?q=`) + on-screen grid keyboard fallback | n/a | n/a | n/a | n/a | ✓ | M5 | planned-M5 |
| **Adult content: permanently hidden, no toggle** (living-room device is hand-off-safe) | n/a | n/a | n/a | n/a | ✓ | M5 | planned-M5 |
| Downloads on TV | n/a | n/a | n/a | n/a | — | never | rejected — a TV is a playback surface, not a storage target; queue lives on phone/desktop |

**Packaging:** one APK with a leanback launcher entry; the same APK installs on phones (TV mode activates only on leanback devices). No separate build flavor.

## Settings & theming

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Quality: auto / 1080p / 720p / 480p (synced) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Themes: Carbon Dark (default), Carbon Light, Tokyo Night, Mono (synced) | ✓ | ✓ | ✓ | ✓ | ✓ | M1 | shipped-CLI |
| Autoplay next toggle (synced) | ✓ | ✓ | ✓ | ✓ | ✓ | M3 | shipped-CLI |
| Hide adult (browse filters `isAdult`; search never pre-filtered; opening blocked only when metadata says Adult; metadata-fetch failure ⇒ **fail-open** (not blocked) — borderline titles are not worth a hard gate on a dead API) | ✓ | ✓ | ✓ | ✓ | locked ON | M3 | shipped-CLI |
| Download dir | ✓ | ✓ | — sandbox | ✓ | — | M2 | shipped-CLI |
| Sync opt-in + account | planned | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |
| Provider enable/disable | see Providers row | | | | | M1 | shipped-CLI |

**`hide_adult` is device-local and excluded from sync** — an adult-enabled desktop must never silently unlock a phone or the TV. See [sync.md § Scope](sync.md#scope-what-syncs-what-never-does).

## Profiles & account

| Feature | C | A | i | D | T | Milestone | Status |
|---|---|---|---|---|---|---|---|
| Profiles — up to 5, signed-out/local-first; own progress, history, search, stats, settings | — (CLI = the implicit `default` profile) | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |
| "Who's watching" switcher + profile page + manage (rename, tint, delete) | — | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |
| Kid profile (adult force-hidden, no toggle) | — | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |
| Account sign-in (Google; Apple on iOS) for per-profile cross-device sync | planned (v1.x, REST) | ✓ | ✓ | ✓ | ✓ | M4 | planned-M4 |

The CLI stays single-profile forever; when it adopts sync (v1.x) it maps to the `default` profile subtree ([sync.md § CLI ↔ app interop](sync.md#cli--app-interop)). Full UI spec: [ui-design.md § Profiles & account](ui-design.md#profiles--account).

## Feature-status change log

The audit trail — a one-liner here whenever a row above flips.

| Date | Change |
|---|---|
| 2026-11-24 | Profiles & account added to M4; recommendations moved never → deferred ([decisions.md D-018](decisions.md#d-018--netflix-style-profiles), [D-019](decisions.md#d-019--recommendations-deferred-built-on-profile-data)) |
| 2026-11-23 | Initial inventory from the product planning rounds; CLI rows reflect shipped behavior, app rows follow the M1–M5 cut in [roadmap.md](roadmap.md) |
