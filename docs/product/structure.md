# Product structure

The shape of the product: how the two clients are organized, what each module owns, the information architecture per form factor, and every on-disk format contract. Code-level truth (SQLite schema, function names) stays in AGENTS.md.

## Two clients, one product

`cli/` (Go, shipped) and `app/` (Flutter, planned) **share no code**. They share three contracts:

1. **The provider adapters** — the same five scrapers, the same MegaPlay resolver, re-implemented per language. Breakage fixes are dual-mandatory ([overview.md § CLI's long-term role](overview.md#the-clis-long-term-role)).
2. **The on-disk formats** — filename schema, sidecar rule, library extensions (see [§ On-disk contracts](#on-disk-format-contracts) below).
3. **Firestore (opt-in)** — the only cross-device/cross-client data bridge ([sync.md](sync.md)).

Each client owns its local SQLite store; there is no shared local database.

## Module map

The Flutter app mirrors `cli/internal/` so the Go code is the reference implementation for every adapter.

| cli/ package | Responsibility | app/ counterpart (planned) |
|---|---|---|
| `provider/provider.go` | The boundary: `Anime`, `Episode`, `Stream{URL, Referer, Subtitles}` | `lib/providers/` (same shapes) |
| `provider/{gogo,animekai,anizone,kissanime,hianime}` | 5 site adapters | `lib/providers/` — one file per site |
| `provider/megaplay` | Shared HLS resolver: player page → `getSourcesNew` → AES-256-CBC → playlist + VTT tracks | `lib/megaplay/` (same key/IV constants — see [operations.md](operations.md#megaplay-key-rotation-runbook)) |
| `metadata` | AniList GraphQL, Kitsu/Jikan ratings, browse/seasons, 7-day cache | `lib/metadata/` |
| `filler` | animefillerlist lookup, Max() trust rule, 30-day cache | `lib/filler/` |
| `cover` | Terminal cover rendering (blocks/native escapes) | **Dropped in the app** — GUI shows art directly (see [features.md § Discovery](features.md#discovery)) |
| `store` | The only SQL; schema, pragmas, progress/history/settings | `lib/store/` (drift, same table semantics) + sync outbox |
| `media` | mpv/ffplay invocation, quality select, ffmpeg downloads, IPC progress | `lib/player/` (media_kit) + `lib/downloads/` |
| `library` | Filesystem scan, `<anime> - E001.ext` parse, sibling `.vtt` | `lib/library/` (same schema) |
| `deps` | ffmpeg/mpv/uosc preflight | **Dropped as a boot screen** — desktop surfaces missing ffmpeg inline in Downloads settings |
| `tui` | Screens, keys, theming, playback/download orchestration | `lib/screens/` + `lib/theme/` + `lib/tv/` |

Stack decided for the app: **Riverpod + go_router + drift + cached_network_image + media_kit** — see [decisions.md D-003](decisions.md#d-003--players-mpv-on-cli-media_kit-libmpv-on-app) and [ui-design.md](ui-design.md).

## On-disk format contracts

Owned here; both clients must honor them exactly. "The filesystem is truth; DB rows are hints" applies to every rule below.

1. **Directory layout:** `<download root>/<safeName(provider)>/<safeName(anime)>/`
2. **File name:** `<safeName(anime)> - E%03d.mp4` (episode zero-padded to 3)
3. **`safeName`:** `[^A-Za-z0-9._ -]+` → `_`, trailing dots/spaces trimmed, empty → `unknown`. Non-ASCII titles collapse to `_` — on Windows two anime can collide into one directory (known limit, inherited from the CLI).
4. **Sidecar captions:** `<video>.vtt` next to the video, auto-loaded by players.
5. **Partial downloads:** destination is `<final>.part` during download; removed on failure/cancel; never scanned by the library.
6. **Library extensions:** `mp4/mkv/webm/avi/mov/m4v/ts`.
7. **Library rescan never deletes DB rows**; a missing file is simply not listed. Skip-existing is decided by file presence + size, not DB rows.
8. **Local resume key:** `provider="local"`, `episode_url=<absolute path>` — moving a file breaks its resume; accepted.
9. **Shared root rule:** if both clients point at one download root, each scanner indexes what the other wrote (the schema above is the contract); duplicate downloads are prevented by rule 7, per client.

## Information architecture — terminal

The shipped TUI's screen graph (code truth in AGENTS.md): `home → results → genres → browse → episodes → playing → downloads → library → providers → stats → settings`, plus `cover`, `setup`, `busy`, `help`. Keyboard-driven; home focus cycle `search → resume → recent`.

## Information architecture — phone/tablet

Five tabs: **Home · Browse · Search · Library · Settings**. Push navigation with the platform back stack. Providers and Stats are Settings sub-pages. Detail screens are pushed routes; the player is a fullscreen opaque route. Sheets (subtitle/quality/episode actions) are modal bottom sheets. See [ui-design.md](ui-design.md#navigation) for the full spec.

## Information architecture — desktop

Same five destinations on a left navigation rail (extends ≥1100px). Title detail is two-pane ≥900px (episode list left, inspector right). Full keyboard map incl. CLI-lineage shortcuts; native window chrome; default 1280×800, minimum 540×720, last size remembered. See [ui-design.md § Desktop & keyboard parity](ui-design.md#desktop--keyboard-parity).

## Information architecture — TV

Single leanback entry point → TV home (hero + horizontal rails; **tabs live on the top edge**, no bottom bar). Focus rings on every selectable; D-pad traversal rules and remote key map in [ui-design.md § 10-foot TV mode](ui-design.md#10-foot-tv-mode-android-tv--google-tv--fire-tv).

## Data flows

**Search → resolve → play:** query fans out to all enabled adapters concurrently (60s budget) → merged/tagged results ([features.md § Search semantics](features.md#search-semantics)) → select pins the provider → episode list → `Stream` resolve (90s budget) → pre-play fallback on failure → player. Metadata and filler load in parallel, best-effort.

**Progress write:** the player polls position every ~5s into SQLite only; near-end positions store as 0 (finished). Cloud writes never ride this poll — see [sync.md § Write path](sync.md#write-path).

**Download:** queue is serial; each item resolves its stream, downloads (ffmpeg on desktop / Dart fMP4 elsewhere), writes the sidecar `.vtt`, records the row on completion. Failures stop the queue and keep the batch for retry. See [features.md § Downloads](features.md#downloads).

**Sync (opt-in):** every store mutation also writes the local outbox; flushes are debounced (episode exit / app exit / 60s-if-nonempty); pulls are cursor-incremental. Full contract: [sync.md](sync.md).

**Filler/variants enrichment:** loaded per episode-list and per selected episode respectively; both cached per device, failures cached so revisits stay quiet.
