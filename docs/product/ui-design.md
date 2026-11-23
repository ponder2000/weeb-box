# UI design plan

The experience spec for the Flutter client: every screen's layout, every state it can be in, input per form factor. Layout and states live here; what ships where lives in [features.md](features.md). The terminal app's visuals are not re-specified — AGENTS.md documents the TUI.

## Design language & tokens

Heritage: **IBM Carbon**, inherited from the CLI's themes. This table is the **source of truth** for the palette — a color change must update all three surfaces that carry it (TUI styles in `cli/internal/tui`, the Flutter theme in `app/lib/theme`, and `cli/themes/mpv/uosc.conf`) in one commit.

| Token | Carbon Dark (default) | Carbon Light | Tokyo Night | Mono |
|---|---|---|---|---|
| background / surface | `#161616` / `#252525` | `#f4f4f4` / `#ffffff` | `#1a1b26` / `#24283b` | `#161616` / `#252525` |
| primary (focus, buttons) | `#78a9ff` (Blue 40) | `#0f62fe` (Blue 60) | `#7aa2f7` | `#f4f4f4` |
| text / dim | `#f4f4f4` / `#a8a8a8` | `#161616` / `#6f6f6f` | `#c0caf5` / `#565f89` | `#f4f4f4` / `#8d8d8d` |
| success / warning / danger | `#42be65` / `#f1c21b` / `#fa4d56` | `#007d79` / `#b28600` / `#da1e28` | `#9ece6a` / `#e0af68` / `#f7768e` | `#f4f4f4` / `#c6c6c6` / `#8d8d8d` |
| selection / outline | `#393939` | `#e0e0e0` | `#414868` | `#393939` |

Mono drops all color: badges and charts distinguish by glyph/label only.

**Typography:** IBM Plex Sans for UI text (Regular/Medium/SemiBold); **IBM Plex Mono for every number** — episode numbers, timestamps, progress, sizes. Scale: hero display 28sp · title 20sp · section header 14sp · body 14–16sp · labels/badges 12sp mono.

**Cover art rules:** posters are always 2:3, radius 4dp, `CachedNetworkImage` with a `#393939` placeholder block showing the title; hero/backdrops are 16:9 (AniList `bannerImage`, fallback = blurred zoomed poster); never crop posters to 16:9; adult-hidden titles show no art; disk cache cap 250 MB.

**Cards:** surface color background, 1dp outline, 4dp radius; focused/hover gets a 2dp primary-color outline + 2% lift. Section headers 14sp SemiBold with a hairline rule.

## Screen inventory

| # | Route | Purpose | Owning section |
|---|---|---|---|
| 1 | `/home` | Resume hero, continue-watching rail, season rails, trending, genre chips | [Home](#home) |
| 2 | `/search` | Multi-provider search with progressive results | [Search](#search) |
| 3 | `/browse` | Genre grid + season rails | [Browse](#browse) |
| 4 | `/title` | Metadata header + episode list with filler badges/filter | [Title detail & episodes](#title-detail--episodes) |
| 5 | `/player` | Fullscreen playback (opaque route) | [Player](#player) |
| 6 | `/library` | Two segments: **Queue \| Files** | [Library & downloads](#library--downloads) |
| 7 | `/settings` | Settings incl. sub-pages: Providers, Sync, Diagnostics | [Settings](#settings--providers) |
| 8 | Cover viewer (overlay) | Full-res art, tap to close | inline in Title detail |
| 9 | Help (desktop `?` overlay) | Keyboard map / gestures | [Desktop](#desktop--keyboard-parity) |

Downloads+Library merge into one tab with two segments; the CLI's Setup screen is dropped (see [structure.md § Module map](structure.md#module-map)).

## Navigation

**Tabs (5): Home · Browse · Search · Library · Settings.** Providers and Sync live under Settings.

- **Phone:** Material 3 bottom tab bar (60dp, surface color); go_router push stack; Android back / iOS edge-swipe pops; pickers are modal bottom sheets; long-press an episode row toggles watched; row overflow icon opens the action sheet (download / watched / quality).
- **Tablet/desktop:** left navigation rail (icons+labels; extends ≥1100px); two-pane title detail ≥900px; context menus replace long-press; no bottom bar ever.
- **Deep links:** `weeb-box://search?q=…` and `weeb-box://title/…` — also the TV voice-search entry ([§ TV mode](#10-foot-tv-mode-android-tv--google-tv--fire-tv)).

## Home

Vertical order (16dp screen padding, 24dp between sections):

1. **Hero — Continue watching.** Full-bleed 16:9 backdrop (240dp phone / 360dp tablet) with bottom scrim; eyebrow `CONTINUE WATCHING`; title 28sp; meta line `E012 · 45% watched · gogoanime` (mono); 4dp progress bar with `10:48 / 24:00`; **Resume** (filled) + **Episodes** (outlined) buttons.
2. **Continue-watching rail.** 16:9 landscape cards 280×158dp, 12dp gap, progress bar on the card, one card per anime (same grouping rule as the CLI's `RecentProgress`).
3. **Trending rail** (AniList, no scraping — guarantees first-run content). Poster cards 124×186dp, score badge, 10–12 per rail.
4. **Season rails** (This / Next / Previous season). Same card size.
5. **Genre chips row** + "All genres →".
6. **Stats strip** (desktop/tablet only): `42 files · 128 GB on disk · 87 watched`, from a fresh scan.
7. **Recent searches** as chips (phone: on the Search tab instead — Home stays lean).
8. **"Providers need attention" chip** when any adapter has `last_error` → links to Providers.

## Search

Single input; results stream in **progressively** — each provider's rows appear as that adapter returns, in registry order; slow providers append late with no skeleton block per provider (a per-provider latency line replaces it: "hianime still searching…" then resolves or reports). **Duplicate titles are merged** by normalized title, first-in-registry-order wins, row tagged with its provider ([features.md § Search semantics](features.md#search-semantics)). Each row: 2:3 poster thumb, title, provider tag, meta (type/episodes). Tap → title detail.

## Browse

Genre chip row (top AniList genres) + grid of poster cards; three season rails (This/Next/Previous) with `n/p`-equivalent pagination buttons; `isAdult` filtered when hide-adult is on, Hentai genre appears only when unhidden ([features.md § Settings & theming](features.md#settings--theming)). Tap → title detail.

## Title detail & episodes

**Header** (scrolls away): 2:3 poster 140×210dp; title 24sp; badges — score (mono, `/100`), age rating, duration, provider chip; genre chips; 3-line synopsis with expand. Actions: **Play next unwatched**, **Download rest** (queues all remaining *visible* rows — the filler filter applies), cover viewer, share.

**Adult gate:** when hide-adult is on and loaded metadata says Adult, the episode list is replaced by an "Adult content hidden — change in Settings" panel. Metadata-fetch failure ⇒ fail-open (list renders) — see [features.md](features.md#settings--theming).

**Episode rows** (64dp): `E012` mono + title (fallback "Episode 12") + status badges — `canon` success-color / `filler` danger / `mixed` warning (list applied only under the [Max() trust rule](features.md#filler)); `✓ done` / progress % / `Started` / `↓` downloaded. Trailing play + overflow. 2dp progress bar under the row.

**Filler filter:** a chip pinned above the list, `Hide filler & mixed` (CLI `f` semantics) — drives visible rows, autoplay, and Download rest ([features.md § Filler](features.md#filler)).

**Dub/sub rows are not merged** in detail lists: each provider's episodes are one list (the merged row pinned the provider), and cross-provider duplicates simply don't appear — resume is per provider row by design.

**Wide layout** (≥900px): episode list left, 320dp inspector right — selected episode's variant heights (cached per URL incl. failures, port of `variantsByURL`), art, actions.

## Player

Fullscreen opaque route; media_kit. Tap toggles the OSD (auto-hide 4s).

**OSD:** top gradient `Anime — E012 · Episode title`; bottom: seek bar with buffered track + position, play/pause, ±10s, **prev/next episode**, then sheets: subtitles (labeled tracks + Off), quality (`Auto/1080p/720p/480p` from the cached variants), autoplay toggle.

- **Resume:** seek when stored position > 1s; poll to SQLite every 5s; within 5s of end store as finished. **Rewatch:** finished episodes start at 0; new position overwrites the sentinel ([features.md § Playback](features.md#playback)).
- **Autoplay next:** at natural end advance to the next *visible* episode (filler filter applies); at list end, pop to detail with a toast.
- **Mid-play failure:** stall/error → automatic same-episode retry on other enabled providers, keeping position, toast "Streaming via animekai"; all failed → exit to the episode list, culprit named, one Retry ([features.md § Providers](features.md#providers)).
- **Local files:** same player, no Referer/HLS flags, sibling `.vtt` auto-attached.
- **Exit flush:** position writes to SQLite continuously; if sync is on, the episode-exit flush uploads it ([sync.md § Write path](sync.md#write-path)).
- **Background (phone):** audio continues via the OS media session; lock-screen play/pause; progress keeps writing.

## Library & downloads

One tab, two segments.

**Queue:** serial queue; row = poster thumb, `E012 · 34% · 2.1 MB/s · ETA 1:40`, cancel; failed rows get retry; cancel-the-batch marks active+queued failed and keeps them for re-queue; **the queue stops on the first error** with a sticky banner (CLI rule). TS-stream failures on mobile show "This episode's stream format can't download on mobile — try the desktop app" (fMP4-only rule, [features.md § Downloads](features.md#downloads)).

**Files:** scan on open + pull-to-refresh; grid of anime groups (poster, `12 episodes · 4.2 GB · 3 watched`); tap → local episode list with resume + watched badges; long-press/right-click → delete file (DB rows may go stale — accepted). Empty root = empty library with "Downloads land here" copy.

## Settings

Quality · Theme (4) · Download dir (Android/desktop; iOS shows the fixed sandbox path) · Hide adult · Autoplay · Providers sub-page (toggles, health, Diagnose) · **Sync** (off by default; sign-in lives here, never prompted) · Diagnostics (bundled libmpv version, desktop ffmpeg presence).

## 10-foot TV mode (Android TV / Google TV / Fire TV)

**Entry:** leanback launcher (320×180dp banner), runtime detection via `UI_MODE_TYPE_TELEVISION` → a `tv` app shell; layout enum `mobile | desktop | tv` chosen at startup, never mixed.

**10-foot rules:** 5% overscan-safe margins; type scale ~1.75× phone (body 24sp, headers 28sp, hero 44sp); poster cards 200×300dp, **8 per rail max**; every selectable has a 3dp primary focus ring + ~4% scale-up; the focused card's title/meta render in a label row beneath the rail (Android TV requires visible focused text); no hover, no swipe — every action has a D-pad path.

**Traversal.** Home: hero is first focus; up from any rail reaches the top tab row (Home · Browse · Search · Library · Settings); left/right within a rail; up/down between rails; Back pops a level, Back on Home root exits. Detail: header static, episode list is up/down rows, center = play, long-press center = action sheet (a D-pad list). Settings: single column, center activates, left/right changes values, focused text announces state ("Hide adult: On" — on TV it is always On and unchangeable). Sheets become centered D-pad lists focused on the current selection.

**Remote key map:** center = play/pause · `MEDIA_PLAY/PAUSE` = same · `MEDIA_FAST_FORWARD/REWIND` = ±10s · `CHANNEL_UP/DOWN` = next/prev episode · Back = exit with resume saved.

**Autoplay card:** at ~20s remaining: `Next: E013 · canon` with countdown, Back cancels — hidden filler is never auto-played.

**Search on TV:** voice/system search (`ACTION_SEARCH` → deep link) is primary; an on-screen grid keyboard with a recent-searches row is the always-present fallback. **Sync sign-in on TV (M4+):** the grid keyboard + a code-entry option; no password fields on TV beyond what the system keyboard presents.

**TV home:** full-bleed hero (resume, Play + Episodes), then horizontal rails only: Continue Watching (8 max), Trending, This Season, Next Season, Genres, Recent searches. Adult content: permanently hidden, no toggle ([features.md § TV mode](features.md#tv-mode-android-tv--google-tv--fire-tv)).

## Desktop & keyboard parity

Window: default 1280×800, min 540×720, last size/position remembered; native chrome; title bar shows `Anime — E012 · title` while playing; macOS accent follows the theme primary. macOS quit (cmd-Q) / Windows close flushes position synchronously before exit.

**Key map (CLI lineage preserved):** `↑/↓ or J/K` move · `G/G` top/bottom · `PgUp/PgDn` page · `Enter` select · `Esc/Backspace` back · `/` focus search · `F` filler filter · `W` watched toggle · `D` download episode · `A` download rest · `[`/`]` prev/next episode · `Space` play/pause · `←/→` ±10s · `S` stats (when shipped) · `Ctrl+P` providers · `Ctrl+,` settings · `Shift+Tab` cycle tabs · `?` help overlay. Mouse: click rows, double-click episode = play, right-click = context menu, hover = focus outline. **Keyboard-only operation is an acceptance criterion** ([roadmap.md](roadmap.md)).

## First-run experience

No onboarding tour. **First run is a working Home:** Trending rail (AniList, no scraping, never blank) + an empty Continue-watching row with one honest line — "Nothing yet — search a series to start." All five providers default ON. Adult hidden by default. First search → play has zero dialogs: type → merged results → episodes → tap → auto quality → playing, with automatic fallback and a toast if a source dies. Sync is never prompted, never timed to N sessions — it waits in Settings ([decisions.md D-016](decisions.md#d-016--no-onboarding-tour-working-home-instead)). Desktop/TV first-run differs only in layout, not in content or steps.

## Error, empty, loading & offline states

The one product rule ([decisions.md D-015](decisions.md#d-015--the-one-state-rule)): **every screen renders its local truth first (SQLite: progress, history, library, toggles) and treats network data as optional enrichment — a failure may cost a badge or a cover, never the screen. The UI names the culprit and offers exactly one retry.** "Something went wrong" is banned. Loading = skeletons that telegraph structure. Empty = one honest sentence + one next action. Offline = a global banner, screens stay usable from local truth, network actions show "You're offline — retry."

| Screen | Loading | Empty | Failed | Offline |
|---|---|---|---|---|
| Home | Rail skeletons; hero appears when progress rows exist | "Nothing yet — search a series to start." + Search CTA | Trending/season rails show "Catalog unavailable — Retry"; hero and history render (local) | Banner; hero + history render; rails show Retry |
| Search | Per-provider latency line ("animekai still searching…") | "No results for 'q' — try another spelling or enable a provider" + Providers link | Dead provider named in a dismissible line; other providers' rows still render. All dead: "All providers failed — check Providers" | "You're offline — retry when connected"; recents render |
| Browse | Grid skeletons | Not applicable (AniList genres are static) | "Catalog unavailable — Retry"; genre chips render | Banner + Retry |
| Title detail | Header skeleton, then episode rows | "No episodes found — the provider may have moved; check Providers" | Culprit named ("gogoanime didn't respond") + Retry + Providers link; badges/covers simply absent if enrichment failed | Banner; nothing network renders |
| Episodes (list) | Row skeletons | Covered above | Filler absent silently if animefillerlist blocked ("only costs the badges") | Local rows render if the anime is downloaded |
| Player | Buffering spinner with the episode title | n/a | Mid-play: auto-fallback toast; all failed → exit, culprit named, one Retry | Playback continues from buffer; banner on exit |
| Library · Queue | n/a (local) | "No downloads yet — download from any episode" | Per-item error named (provider dead / TS-on-mobile / network) + Retry item | Queue paused, resumable |
| Library · Files | Scan indicator | "Downloads land here" (root missing = same) | n/a (filesystem) | Fully usable |
| Providers | Health load | n/a (5 rows always) | Diagnose reports per-provider error text | "You're offline — statuses may be stale" |
| Settings | n/a | n/a | Sync failures surface as "Sync paused — sign in again" only inside the Sync row ([sync.md § Failure modes](sync.md#failure-modes)) | Sync silently deferred |

**Ambient detection:** any adapter `last_error` lights the Home "Providers need attention" chip — MegaPlay rot announces itself instead of waiting for a failed search ([features.md § Providers](features.md#providers)); detection tooling is [operations.md](operations.md#detection-how-breakage-is-noticed).

## Accessibility

Text scaling honors OS settings (layouts must survive 150%); contrast meets the Carbon palette's built-in ratios; focus rings are 3dp and always visible (TV requirement doubles as keyboard-navigation aid); all state is conveyed by text/glyph, never color alone (Mono theme is the proof).
