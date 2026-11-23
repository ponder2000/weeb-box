# Roadmap

Sequencing and done-ness. Milestone contents reference [features.md](features.md) rows; acceptance criteria are user-visible and verifiable without reading code.

## Acceptance criteria convention

A milestone is done when every features.md row it claims shows the target status **and** each criterion below passes on real hardware (emulators don't decode HLS). "User-visible" means a person can verify it by using the app.

## M0 — Baseline: CLI shipped (today)

The complete terminal product: search, playback with resume/subtitles/quality, downloads, library, filler badges + filter, stats, settings, providers, 4 themes. Acceptance: full unit suite green, daily provider-status CI green, and the features.md `shipped-CLI` column matches reality.

## M1 — App skeleton + watch path

Flutter skeleton on all five OS targets (theme, 5-tab navigation, honest empty states), Dart adapters for all five providers + MegaPlay resolver, merged search, episodes with filler badges + filter, media_kit player (subs, quality, resume, autoplay-next, pre-play and mid-play fallback).

**Done when:**
- Debug builds navigate with honest empty states on Android, iOS, macOS, Linux, Windows; CI produces an APK.
- Cold app → episode-playing-with-subs takes <30s on a phone over Wi-Fi.
- All five Dart adapters pass gated live tests (mirroring the CLI's `TestLive` pattern — the drift-detection gap closes here, see [operations.md](operations.md#detection-how-breakage-is-noticed)).
- Kill and reopen the app mid-episode: it resumes at the exact position.
- A provider failure mid-search is named in the UI; a stream failure mid-play falls back with a toast.
- Keyboard-only full path works on desktop (search → play → next episode).

## M2 — Offline

Download queue (Dart fMP4 mobile / ffmpeg-on-PATH desktop), library scan, local playback with sidecar subs, local resume.

**Done when:**
- A downloaded episode plays start-to-finish in airplane mode with subtitles.
- A paused download survives app restart; a failed segment retries then stops the queue with the error named; retry re-queues the batch.
- Skip-existing works across a shared download root with the CLI (same files, no re-download).

## M3 — Polish & discovery — **this is v1.0**

Slim browse (Trending + seasons + genres), first-run experience, providers screen with health + diagnose, settings parity, the full state matrix from [ui-design.md](ui-design.md#error-empty-loading--offline-states).

**Done when:**
- With every provider deliberately broken, the app stays coherent and useful: library + history + named errors + the "Providers need attention" chip.
- A fresh install reaches first play with zero instructions.
- All four themes render correctly on phone + desktop; filler filter drives autoplay and Download rest.

## M4 — Accounts, profiles & sync — v1.1

Opt-in Firebase sync ([sync.md](sync.md)) + Netflix-style profiles (up to five, kid-lockable — [ui-design.md § Profiles & account](ui-design.md#profiles--account)) + local DB export/import as its no-cloud twin.

**Done when:**
- Profiles create/switch/delete works **fully signed-out** (local-first), and a kid profile exists with adult content unreachable from every screen.
- After sign-in on a second device: the same profiles appear, and each profile's resume points match.
- Watch on one device, open another: the same profile shows the same resume point after refresh; and vice versa.
- A watched toggle propagates both directions; an un-watch (tombstone) propagates.
- Opting out leaves all local data intact; "Delete cloud data" removes only the cloud subtree.
- Export/import moves a full library between two devices with no account.

## M5 — TV mode — v1.2

Leanback entry in the same APK, 10-foot home, D-pad everywhere, remote key map, voice/grid search, adult permanently hidden ([features.md § TV mode](features.md#tv-mode-android-tv--google-tv--fire-tv)).

**Done when:**
- A Fire TV stick completes an episode plus "next episode" using only the remote (channel-up/down included).
- Voice search on a Google TV deep-links to results; the grid keyboard works without voice.
- No adult content is reachable from any TV screen.

## Deferred bucket

v1.x, no milestone until promoted. Each entry names the revival trigger.

| Feature | Revival trigger |
|---|---|
| PiP | Player stable across a milestone with zero playback regressions |
| Backdrops/artwork variants | TV mode shipped (only the lean-back hero earns the payload) |
| Gamepad support | TV mode shipped — falls out of the D-pad focus layer |
| Intro-skip markers | First polish pass after v1.0 (one AniSkip-style fetch) |
| Speed control | Same polish pass (one media_kit call; the cost is UI + persistence) |
| RSS/simulcast calendars | Pairs with the notifications milestone |
| New-episode notifications | Sync shipped (needs a followed-shows set) + honest OS background handling |
| Anime list tracking (MAL/AniList, read-only first) | After sync; reuses the AniList path, no new infra |
| Recommendations engine | Profiles shipped ([decisions.md D-018](decisions.md#d-018--netflix-style-profiles)) plus a few months of per-profile watch data — the training signal; AniList's own recs fill detail pages until then ([D-019](decisions.md#d-019--recommendations-deferred-built-on-profile-data)) |
| Stats on app | First polish pass after v1.0 (CLI has them today) |

## Rejected

Nothing lives here twice — the never-list with reasons is [overview.md § Non-goals](overview.md#non-goals), plus per-feature rows marked `rejected` in [features.md](features.md) (cast, watch parties, torrents, per-episode thumbnails, screenshot, A-B loop, multiple download dirs, downloads on TV). Profiles and recommendations left this list via [decisions.md D-018](decisions.md#d-018--netflix-style-profiles) and [D-019](decisions.md#d-019--recommendations-deferred-built-on-profile-data).
