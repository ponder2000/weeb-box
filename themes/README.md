# IBM Plex Mono / Carbon theme

`weeb-box` is a terminal app, so it cannot change the font itself. The font and
window colors come from your terminal emulator. This folder gives you both.

The look: **IBM Plex Mono** on an **IBM Carbon** dark palette
(`#161616` background, Blue 40 `#78a9ff` accent).

## 1. Install the font

macOS (Homebrew):

```sh
brew install --cask font-ibm-plex-mono
```

Or download IBM Plex Mono from <https://github.com/IBM/plex/releases> and
install the `.ttf` files.

The family name to select is **IBM Plex Mono**. Use Regular 12–14 pt, Medium
for headings, and enable ligatures if your terminal supports them.

## 2. Set the terminal font

- **iTerm2:** Settings → Profiles → Text → Font → *IBM Plex Mono*.
- **Terminal.app:** Settings → Profiles → Text → Font → *IBM Plex Mono*.
- **Kitty:** `font_family IBM Plex Mono` in `kitty.conf`.
- **Alacritty:** `[font.normal] family = "IBM Plex Mono"` in `alacritty.toml`.
- **WezTerm:** `config.font = wezterm.font("IBM Plex Mono")`.

## 3. Apply the Carbon colors

Ready-made configs:

- Alacritty: copy the contents of [`ibm-carbon.toml`](ibm-carbon.toml) into
  your `alacritty.toml`.
- Kitty: include [`ibm-carbon.conf`](ibm-carbon.conf) from `kitty.conf`:
  `include /path/to/weeb-box/themes/ibm-carbon.conf`.

iTerm2: Settings → Profiles → Colors → Color Presets → Import, choose
[`ibm-carbon.itermcolors`](ibm-carbon.itermcolors), then select the imported
preset. The same values are listed below for manual setup:

| slot | hex | slot | hex |
| --- | --- | --- | --- |
| background | `#161616` | foreground | `#f4f4f4` |
| cursor | `#78a9ff` | selection | `#393939` |
| black | `#161616` | bright black | `#525252` |
| red | `#fa4d56` | bright red | `#ff8389` |
| green | `#42be65` | bright green | `#6fdc8c` |
| yellow | `#f1c21b` | bright yellow | `#f1c21b` |
| blue | `#4589ff` | bright blue | `#78a9ff` |
| magenta | `#be95ff` | bright magenta | `#d4bbff` |
| cyan | `#33b1ff` | bright cyan | `#82cfff` |
| white | `#f4f4f4` | bright white | `#ffffff` |

The TUI itself uses the same Carbon palette, so the panels and badges match the
terminal scheme.

## 4. Optional: modern mpv controls

The app already styles mpv and caps its HLS prefetch. For a more modern control
bar, install uosc as described in [`mpv/README.md`](mpv/README.md).

## 5. Run

```sh
go run .
```

If the colors look wrong, make sure the terminal is using a true-color profile
and that `COLORTERM=truecolor` is set.
