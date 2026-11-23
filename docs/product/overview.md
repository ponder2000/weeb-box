# Product overview

This is the only doc allowed to define what weeb-box is. Everything else links here.

## What weeb-box is

A **personal, local-first anime streaming client**. It searches scraped, ad-funded anime sites, resolves their HLS streams, and plays them in a proper player — with subtitles, resume, downloads, and a Netflix-grade catalog experience. It is one owner-user's tool, not a service: **there is no weeb-box server, and there never will be one.** All scraping, playback, and downloading happen on the user's own devices via plain HTTP.

Today weeb-box ships as a terminal app (Go, `cli/`). A native Flutter client (`app/`) is planned for phone, desktop, and Android TV.

## Who it is for

One person: the owner-user. Every design decision optimizes for a single user who knows what anime is, wants to watch it without ads or a browser, and does not want an account. Secondarily, developers contributing to the repo — the docs exist so anyone (including a future agent or contributor) can build without re-deriving decisions.

## Form factors

| Form factor | State | Notes |
|---|---|---|
| Terminal UI (Go) | **Shipped** | The complete product today; see [structure.md § IA — terminal](structure.md#information-architecture--terminal) |
| Flutter phone (Android, iOS) | Planned | v1.0 at roadmap M3; see [roadmap.md](roadmap.md) |
| Flutter desktop (macOS, Linux, Windows) | Planned | Same app, same v1.0 |
| Android TV / Google TV / Fire TV | Planned | Same APK, leanback entry, v1.2 (M5) |
| Apple TV (tvOS), Samsung Tizen, LG webOS | **Never** | tvOS is not an official Flutter target; Tizen/webOS are web-app platforms whose CORS rules make scraped-stream playback impossible without a server — a server is a non-goal. See [decisions.md D-009](decisions.md#d-009--tv-mode-after-phone-and-desktop-android-tv-only) |

## Product principles

1. **Play in seconds.** Search to episode-playing-with-subtitles in under 30 seconds, resume always on. Every other feature decorates this loop.
2. **Local-first, private by default.** SQLite on device; sync strictly opt-in (see [sync.md](sync.md)); no weeb-box server ever.
3. **Never a dead end.** Provider failures fall back automatically; metadata/covers/filler degrade quietly; local truth (progress, library, history) always renders. Scraped sources rot constantly; the app outlives each outage.
4. **One shelf on every screen.** Phone, desktop, and TV are modes of one app with one library identity. TV is a mode, not a second product.

## Non-goals

- **No public app-store presence or store-compliance work.** Scraped content fails review; this is not a business.
- **No weeb-box-operated server of any kind** — no scraping proxy, no accounts backend. Firebase (opt-in sync only) relays the user's own rows; see [sync.md](sync.md).
- **No torrent/P2P sources.** Legal exposure with zero upside over HLS.
- **No social features** (watch parties, feeds, chat) and **no multi-user profiles.** The charter is single-user.
- **No monetization surface and no telemetry.**
- **No required account at any point.** Sign-in exists only inside opt-in sync.
- **No encoding/subtitle toolbox.** weeb-box plays and downloads; it does not remux, transcode, or edit.

The full deferred/rejected feature list with per-item reasons lives in [roadmap.md § Deferred](roadmap.md#deferred-bucket) and [features.md](features.md).

## Distribution stance

weeb-box is **sideloaded, personal-use software**. Builds reach devices directly; nothing is published to public stores.

| Platform | Delivery reality |
|---|---|
| macOS / Linux / Windows | Direct binary releases attached to git tags |
| Android / Fire TV | `adb install` (or any file manager); unknown-sources permission once |
| iOS | Free Apple provisioning signs for **7 days** and caps at 3 devices — resigning weekly is the accepted distribution reality; a paid developer account (~$99/yr) is optional, not assumed. The procedure lives in [operations.md § Release & sideload](operations.md#release--sideload-procedure) |

## The CLI's long-term role

**The Go CLI goes maintenance-only once the Flutter app's M1 (watch path) ships.** Bug fixes, provider-selector fixes, and MegaPlay key rotations continue; no new CLI features, no removals.

- **Dual-mandatory changes:** provider breakage fixes land in **both** Go and Dart adapters — scrapers are the only code that rots on both sides of the split. See [operations.md](operations.md).
- **Parity policy (a floor, not a chase):** the CLI keeps 100% of today's features forever; the app reaches parity at v1.0 (M3); after that the CLI may trail permanently, documented per release.
- **Data relationship:** the CLI and app share no code and no local database. Firestore (opt-in) is the only bridge — see [sync.md § CLI ↔ app interop](sync.md#cli--app-interop).
