# mpv playback

`weeb-box` launches mpv with a Carbon-styled OSD, IBM Plex Mono subtitles, and
a short streaming buffer. It does not download the whole episode for playback.

## Built-in controls

The app passes these automatically:

- OSC + keyboard controls, hardware decoding
- `--cache-secs=30`, `--demuxer-readahead-secs=15`, `--demuxer-max-bytes=48MiB`
  — roughly 15–30 seconds ahead, not the full file
- `--sub-file` for the default caption track (English when available)
- IBM Plex Mono styling for OSD and subtitles

## Optional: uosc

For a more modern control bar, install [uosc](https://github.com/tomasklaen/uosc).
The official installer is the least error-prone:

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/tomasklaen/uosc/HEAD/installers/unix.sh)"
```

Manual install (note the `src/uosc` directory, not the repo root):

```sh
git clone --depth 1 https://github.com/tomasklaen/uosc /tmp/uosc
mkdir -p ~/.config/mpv/scripts
cp -R /tmp/uosc/src/uosc ~/.config/mpv/scripts/uosc
```

Then install the Carbon theme (back up an existing `uosc.conf` first, or merge
the keys):

```sh
mkdir -p ~/.config/mpv/script-opts
cp themes/mpv/uosc.conf ~/.config/mpv/script-opts/uosc.conf
```

`weeb-box` detects `~/.config/mpv/scripts/uosc` and launches mpv with
`--osc=no`, so uosc is the only controller. If it is not installed, the built-in
OSC is used.

The theme sets the top bar title from `media-title`; the app passes
`--force-media-title=<anime> — E001 · Episode title`, so uosc shows that instead
of `master.m3u8`.

## Subtitle controls

While mpv is playing:

- `v` — show/hide the current subtitle track
- `j` — cycle subtitle tracks
- the **CC** button in the uosc control bar toggles subtitles on/off

The default English track is loaded automatically when the episode has one.

If playback stutters, raise `--cache-secs`/`--demuxer-max-bytes` in
`internal/media/media.go`. If you want less prefetch, lower them; 15 seconds of
read-ahead and a 48 MiB forward buffer are the current sweet spot.
