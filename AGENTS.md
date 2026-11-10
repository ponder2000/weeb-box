# Repository guidance

## Commands
- Build: `go build ./...` (or `go build -o bin/weeb-box .`).
- Vet: `go vet ./...`.
- Unit tests: `go test ./...`. They cover all provider adapters, the shared MegaPlay resolver/AES, deps, library, store round-trips, media arguments, a generated HLS download through real ffmpeg, and TUI state/multi-provider search/layout-fit tests. The HLS test skips if ffmpeg is absent.
- Live tests (hit the real sites, ~seconds): `WEEB_LIVE=1 go test ./internal/provider/... -run Live -v` and `WEEB_LIVE=1 go test ./internal/media -run Live -v`. Do not run casually.
- Run: `go run .` — alt-screen TUI, needs a real terminal; not a headless smoke test.
- Dependency check: `go run . -check` — prints ffmpeg/mpv/uosc/theme paths plus install commands; exits 1 if ffmpeg or mpv is missing.

## Architecture
- `main.go`: flags `-dir`, `-db`, `-base`, `-library`; default dir `~/Downloads/weeb-box`; library defaults to the download dir; builds the provider registry (`gogoanime`, `animekai`, `anizone`, `kissanime`, `hianime`), then `store.Open` and `tui.New(providers, dir, st).SetLibrary(...)`; `tea.WithAltScreen()` + `tea.WithMouseCellMotion()`; errors go to stderr via `fail`. `-base` overrides gogoanime only.
- `internal/provider/provider.go`: the boundary. `Anime{Title,URL,Meta}`, `Episode{Number,Title,URL}`, `Subtitle{URL,Label,Default}`, `Stream{URL,Referer,Subtitles}`, `Provider{Name,Search,Episodes,Stream}`.
- `internal/provider/megaplay`: shared MegaPlay resolver: player page → `[data-id]` → `getSourcesNew` with Referer/XHR → AES-256-CBC → HLS + WebVTT tracks. Used by gogo, animekai, anizone, kissanime, hianime. The key/IV rotate; update them here.
- `internal/metadata`: AniList GraphQL for cover/genres/tags/isAdult/score; Kitsu age rating first, Jikan fallback (`G`, `PG-13`, `R`, `18+`, `Not rated`); `Genres` list and `Browse(genre,page)`. TUI caches lookups in `anime_meta` for 7 days.
- `internal/cover`: fetch/decode/resize covers; truecolor half-block `▀` output, grayscale ASCII fallback; in-memory render cache.
- `internal/provider/gogo`: gogoanime adapter (WordPress animestream + MegaPlay). 30s HTTP client, 20 MiB body cap, browser UA.
  - Search: `.postbody .listupd article.bs a[itemprop="url"]`; title from `title` attr, else text; meta from `.typez`/`.sb`/`.epx`; dedupe by href.
  - Episodes: `.eplister ul li a`; number from `.epl-num`, else the `-episode-(\d+)` href regex; dedupe and sort ascending.
  - Stream: episode page → `iframe[src*="newplayer"]` → `iframe[src*="megaplay"]` (any iframe fallback) → `megaplay.Resolve`. MegaPlay returns a soft "Error 410" page without the Referer.
- `internal/provider/animekai`: `animekai.com.co`; WP REST search, `#ak-episodes`/anchor episodes, newplayer → `megaplay.Resolve`.
- `internal/provider/anizone`: `anizone.io`; WP REST `az_series` search, `.az-ep-grid` episodes, direct MegaPlay iframe. Recommended-series links outside the grid must be ignored.
- `internal/provider/kissanime`: `kissanime.com.cv`; WP REST search, episode anchors, `streaming.php` → MegaPlay.
- `internal/provider/hianime`: `hianime.ms`; HTML search/details, `data-hk-watch` token → `/stream/<token>/<lang>` 302 → `megaplay.Resolve`. Details pages omit unavailable episodes.
- `internal/tui/tui.go`: screens `home → results → genres → browse → episodes → downloads → library → providers → settings`, plus `setup`, `busy`, and `help`, with a home focus cycle `search → resume → recent`. Results/Browse show a metadata preview at >=100 columns; `ctrl+b` enters genre browse. Metadata/cover are injected via small interfaces for offline tests. `Init` also runs `deps.Check`; missing tools auto-open Setup once per session, and `r` rechecks. Search fans out to every **enabled** provider concurrently, keeps registry order, marks `last_ok`/`last_error` in `provider_state`, and each result row carries its adapter. Selecting a result sets the current provider; `m.byName` resolves provider names from saved progress. Provider work runs in `tea.Cmd`s: home load 5s, search/episodes 60s, stream 90s. Downloads run in a goroutine (`runDownloads`) feeding a buffered `dlEvent` channel read by `waitFor`. Playback uses `tea.ExecProcess`; `ctrl+c` cancels the download/play contexts and quits.
- The TUI is a max-110-column, horizontally centered column; the header/footer stay at the window edges and the body is centered vertically. Mouse hit-testing depends on `bodyOffset()`/`leftMargin()` mirroring `View()`. If layout changes, update those and `listIndex()` (2-line default delegate) together, or clicks land on the wrong row.
- Mouse: wheel scrolls the list under the pointer; click selects, a second click within 600ms activates; episode detail action rows are single-click. `main.go` enables `tea.WithMouseCellMotion()`.
- Long list titles use `marqueeDelegate`/`scrollText` with a shared `scrollState` advanced by `scrollTickCmd` every 160ms. Width is estimated as `list.Width()-8`; download rows compute their own remaining width. Keep `scrollText` width-safe with `runewidth`.
- Navigation: `j/k`, `g/G`, `pgup/pgdown`, `home/end` move lists; `esc/left/backspace` go back; `ctrl+o` Settings, `ctrl+p` Providers, `ctrl+l` Library, `ctrl+h` Help. The header credit `(developed by @chotathanos)` is an always-visible OSC 8 link to https://x.com/chotathanos and is clickable on row 0.
- `internal/media/media.go`: the only place `ffmpeg`/`ffprobe`/`mpv`/`ffplay` are invoked, and the only place the folder is opened.
- `internal/library/library.go`: filesystem scan for downloaded videos (`mp4/mkv/webm/avi/mov/m4v/ts`), parses `<anime> - E001.ext`, detects a sibling `.vtt`, and skips `.part`. A missing root is an empty library.
- `internal/deps/deps.go`: preflight for `ffmpeg`, `mpv`, `uosc`, and `script-opts/uosc.conf`; OS-specific install commands; shared by `-check` and the Setup screen. `media.MPVConfigDir()` honors `MPV_HOME`, `XDG_CONFIG_HOME`, Windows `%APPDATA%`, then `~/.config/mpv`.
- `internal/store/store.go`: the only SQL. Owns the schema, pragmas, and default path; TUI and the download goroutine call its methods.

## State and persistence
- `store.Open("")` resolves `DefaultPath()`: macOS `~/Library/Application Support/weeb-box/weeb-box.db`; Windows `%LOCALAPPDATA%\weeb-box` (fallback `~/AppData/Local/weeb-box`); otherwise `$XDG_DATA_HOME/weeb-box` or `~/.local/share/weeb-box`. `-db` overrides it. Opens WAL, `busy_timeout=5000`, `synchronous=NORMAL`, `MaxOpenConns(1)`, and creates the schema idempotently.
- Schema: `search_history(query PK, last_used, uses)`; `watch_progress(provider, episode_url, anime_url, anime_title, episode_number, position, duration, updated_at, PK(provider, episode_url))`; `downloads(provider, episode_url, anime_url, anime_title, episode_number, path, bytes, completed_at, PK(provider, episode_url))`; `provider_state(name PK, enabled, last_ok, last_error)`; `settings(key PK, value)`; `anime_meta(key PK, payload JSON, fetched_at)`. `GetSetting` returns a fallback when unset; metadata cache TTL is 7 days.
- Stats = DB counts/sums from `downloads`, plus `COUNT(*)` of `watch_progress WHERE position > 0`. `loadHomeCmd` overrides the Downloads/Bytes numbers with a fresh `library.Scan` of the library root, so Home reflects files on disk rather than stale DB rows.
- A search is recorded before the provider call. A download (including a skipped existing file) is recorded on completion. Progress is saved with the resume position before playback and every ~5s while mpv runs; positions within 5s of the end are stored as 0 so the episode is not offered for resume.
- Local library playback stores progress with `provider="local"` and `episode_url=<absolute path>`, so Continue watching can resume downloaded files. `episode_url` is the file path, not a URL; keep it stable if moving files.

## Media behavior
- `Download` runs ffmpeg with `-c copy` into `dest.part`, reports `Progress{Percent,Bytes,Rate,Speed,ETA}` from `out_time_us`/`total_size`/`speed`, then renames. `Rate` is measured from byte deltas; `Speed` is ffmpeg's realtime multiplier. The ffprobe duration runs concurrently so a slow probe does not delay ffmpeg; percent stays 0 until it arrives, while bytes/rate show immediately. `defer os.Remove(part)` cleans up failures/cancels.
- The `.part` suffix has no recognizable extension, so the output muxer must be explicit (`-f mp4`). Do not remove that flag; ffmpeg otherwise fails with "Unable to choose an output format".
- Shared input options must precede the input URL: `-allowed_segment_extensions ALL -extension_picky 0` plus `-headers Referer: ...` when set (ffmpeg 9 otherwise rejects the site's decoy HLS segment extensions).
- `Play(ctx, src, title, start, quality)` and `Download(ctx, src, dest, quality, ...)` call `SelectQuality` first. Quality is `auto` (default) or `1080p`/`720p`/`480p`; auto probes a segment and chooses a variant with ~30% headroom (`shortcut:`; rough single-sample estimate). Selection failure is non-fatal and falls back to the master playlist. `PlayLocal` ignores quality.
- `Play` prefers mpv, falling back to ffplay, and errors if neither exists. Resume seek is applied only when `start > 1` (`--start=`, `-ss`). It fetches the default WebVTT track (with the stream Referer) into the player's temp dir and passes `--sub-file`; on fetch failure it passes the URL and lets mpv fetch it.
- mpv gets Carbon OSD/subtitle styling, `--hwdec=auto-safe`, and a deliberately short HLS buffer: `--cache-secs=30`, `--demuxer-readahead-secs=15`, `--demuxer-max-bytes=48MiB`, `--demuxer-max-back-bytes=8MiB`. Do not raise these to "download ahead"; playback should stay near the playhead.
- If `~/.config/mpv/scripts/uosc` (or `$XDG_CONFIG_HOME/mpv/...`) exists, mpv is launched with `--osc=no` so uosc is the only controller; otherwise the built-in OSC is used. `themes/mpv/uosc.conf` is the Carbon theme (copy to `~/.config/mpv/script-opts/uosc.conf`); `themes/mpv/README.md` documents install/merge.
- `--force-media-title=<anime> — E001 · title` is passed so uosc's top bar shows the episode title instead of `master.m3u8`. Do not remove it when editing mpv args.
- mpv gets `--input-ipc-server` on a temp unix socket (not on Windows) and `WatchProgress` polls `duration`/`time-pos` every 5s with `request_id` matching. ffplay and Windows mpv have no IPC, so no live progress. This ffmpeg/ffplay build has no libass/subtitles filter, so ffplay is a controls-only fallback; mpv is required for subtitles.
- `DownloadSidecar` writes the default caption as `<video>.vtt` next to downloaded episodes (auto-loaded by mpv/VLC).
- `PlayLocal(path,title,start)` is the offline path: no Referer/HLS input flags, attaches a sibling `<video>.vtt` if present, and uses the same mpv/uosc/cache/progress options. `mpvArgs` is shared with streaming; keep both paths in sync.
- `Player.Cleanup` removes the temp socket/subtitle directory after `tea.ExecProcess` returns.

## Gotchas
- Imports are module paths (`github.com/ponder2000/weeb-box/internal/...`). Add nothing outside `internal/` unless it is `main`.
- The TUI deliberately does not treat `q`/`?`/`l` as commands while home search is focused, so users can type them; `ctrl+h` is the always-available Help toggle, `ctrl+l` the Library, and `ctrl+p` the Providers screen. `ctrl+r` only clears history when the recent list has focus; `v` opens the queue only when it is non-empty.
- Library truth is the filesystem, not the `downloads` table. A row can be stale after manual deletes; rescanning (`r`) does not delete DB rows. Moving a file breaks its `provider="local"` resume key.
- Setup auto-opens once per session when `deps.Check` finds missing pieces; setup commands are shortened to the panel width. Use `go run . -check` for the full copy-paste commands. `-check` exits 1 only when ffmpeg or mpv is missing; uosc/theme are optional.
- Providers screen (`ctrl+p`) toggles rows and persists `provider_state.enabled`. `updated` means the last **successful Search**, not adapter build time. Disabling a provider only filters future searches; existing progress/downloads remain. A successful search clears `last_error`; all providers disabled makes search return "all providers are disabled".
- Settings (`ctrl+o`) persists `quality`, `theme`, and `download_dir`. Themes are Carbon Dark, Carbon Light, Tokyo Night, Mono; `applyTheme` mutates global styles and `refreshTheme` rebuilds widget delegates. The font is terminal-level, not app-controlled. `main.go` uses saved `download_dir` only when `-dir` is absent; `-dir` wins. The database path is display-only (`-db` requires restart).
- `media.Play`/`Download` receive the quality setting; `auto` does a bandwidth probe. `PlayLocal` does not use quality. `media.OpenURL` is used by the creator link.
- Metadata is best-effort: title matching is heuristic, Jikan is unreachable on some networks, and Kitsu may lack a rating (shown as "Not rated"). Browse uses AniList `isAdult:false`, but provider search results are not filtered by rating. Cover width assumes one cell per `▀`; truecolor detection is startup-only.
- `x` cancels a queue by cancelling its context and marking active/queued items failed, but does not set `lastFailed`; `r` therefore cannot retry a cancelled queue. Re-start it from the episodes screen.
- Downloads are serial; `runDownloads` stops on the first stream/download error. Failures also set the model's sticky `err`.
- Skips are based on `os.Stat(dest).Size() > 0`, not the database; a deleted DB row does not force a re-download, and an existing file is re-indexed when its episode is queued.
- Filenames: `safeName` replaces `[^A-Za-z0-9._ -]+` with `_`, trims trailing dots/spaces, and maps empty to `unknown`. Directory is `<dir>/<safeName(provider)>/<safeName(anime)>`; file is `<safeName(anime)> - E%03d.mp4`.
- The `-dir` tree and the database are independent; changing `-dir` does not migrate rows or files.
- MegaPlay AES key/IV are copied from player JS and will rotate. When streams start failing, re-read `https://megaplay.buzz/lib/app.main.js` and update `sourcesKey`/`sourcesIV` before chasing parsers.
- No browser automation or JS execution; everything is plain HTTP + goquery. XHR-only state belongs inside `gogo.Stream`.
- The live site is fragile: selectors, hosts, and the player change without notice. Use `-base` for mirrors; keep live tests gated.
- The TUI cannot set the terminal font. IBM Plex Mono is configured by the user's terminal; keep TUI colors aligned with the Carbon palette in `themes/` rather than pretending the app controls fonts.
