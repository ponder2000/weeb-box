// Package cover renders remote images as terminal art.
package cover

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
)

const (
	userAgent     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0 Safari/537.36"
	maxImageBytes = 8 << 20
	asciiRamp     = " .:-=+*#%@"
)

// NativeKind is a terminal inline-image protocol.
type NativeKind int

const (
	NativeNone   NativeKind = iota // no native protocol; use Render
	NativeITerm2                   // iTerm2 OSC 1337 File
	NativeKitty                    // Kitty graphics protocol
)

// Renderer fetches and renders covers. Set TrueColor to override terminal
// detection after New.
type Renderer struct {
	HTTP      *http.Client
	TrueColor bool
	native    NativeKind
	nativeSet bool
	cache     map[string]string
	mu        sync.Mutex
}

// SetNative forces a native protocol, including NativeNone, instead of
// detecting it from the environment.
func (r *Renderer) SetNative(k NativeKind) {
	r.native, r.nativeSet = k, true
}

// Native reports the protocol used by RenderNative. Precedence: an explicit
// SetNative value or WEEB_COVER, then the kitty protocol (kitty, Ghostty,
// WezTerm, Konsole), then iTerm2, then NativeNone.
func (r *Renderer) Native() NativeKind {
	if r.nativeSet {
		return r.native
	}
	return detectNative()
}

// detectNative reads the environment once per call; it is cheap and keeps
// tests honest, unlike caching at process start.
func detectNative() NativeKind {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WEEB_COVER"))) {
	case "kitty":
		return NativeKitty
	case "iterm2", "iterm":
		return NativeITerm2
	case "none", "blocks", "off":
		return NativeNone
	}
	term := strings.ToLower(os.Getenv("TERM"))
	if os.Getenv("KITTY_WINDOW_ID") != "" ||
		strings.Contains(term, "xterm-kitty") ||
		strings.Contains(term, "xterm-ghostty") ||
		strings.EqualFold(os.Getenv("TERM_PROGRAM"), "ghostty") ||
		strings.EqualFold(os.Getenv("TERM_PROGRAM"), "WezTerm") ||
		os.Getenv("WEZTERM_EXECUTABLE") != "" ||
		os.Getenv("KONSOLE_VERSION") != "" {
		return NativeKitty
	}
	if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "iTerm.app") ||
		strings.EqualFold(os.Getenv("LC_TERMINAL"), "iTerm2") {
		return NativeITerm2
	}
	return NativeNone
}

// inTmux reports a multiplexer that hides graphics escapes unless they are
// wrapped in a passthrough DCS.
func inTmux() bool { return os.Getenv("TMUX") != "" }

// wrapTmux escapes a control sequence for tmux's passthrough channel, which
// expects every ESC inside the payload to be doubled.
func wrapTmux(seq string) string {
	if !inTmux() {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// ClearNative returns the escape that deletes inline kitty images, so the cover
// does not linger over the TUI after the viewer closes. Empty for other
// terminals: iTerm2's inline images belong to the cell flow and disappear with
// the redraw.
func ClearNative() string {
	if detectNative() != NativeKitty {
		return ""
	}
	return wrapTmux("\x1b_Ga=d,d=A\x1b\\")
}

// New returns a Renderer with a 15s client and truecolor detected from
// COLORTERM/TERM.
func New() *Renderer {
	return &Renderer{
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		TrueColor: detectTrueColor(),
		cache:     make(map[string]string),
	}
}

func detectTrueColor() bool {
	if strings.Contains(strings.ToLower(os.Getenv("COLORTERM")), "truecolor") ||
		strings.Contains(strings.ToLower(os.Getenv("COLORTERM")), "24bit") {
		return true
	}
	term := strings.ToLower(os.Getenv("TERM"))
	return strings.Contains(term, "truecolor") || strings.Contains(term, "24bit") || strings.Contains(term, "direct")
}

// Render returns width x height terminal lines for the image at url. TrueColor
// renders a 2x2 pixel block per cell with quadrant glyphs (four pixels per
// cell); otherwise grayscale ASCII is used. Results are cached per url, size
// and mode.
func (r *Renderer) Render(ctx context.Context, url string, width, height int) (string, error) {
	if width <= 0 || height <= 0 {
		return "", fmt.Errorf("cover: invalid size %dx%d", width, height)
	}
	key := fmt.Sprintf("%s|%d|%d|%t", url, width, height, r.TrueColor)

	r.mu.Lock()
	cached, ok := r.cache[key]
	r.mu.Unlock()
	if ok {
		return cached, nil
	}

	img, err := r.fetch(ctx, url)
	if err != nil {
		return "", err
	}

	var out string
	if r.TrueColor {
		out = renderQuadrants(resize(img, width*2, height*2), width, height)
	} else {
		out = renderASCII(resize(img, width, height), width, height)
	}

	r.mu.Lock()
	r.cache[key] = out
	r.mu.Unlock()
	return out, nil
}

// RenderNative returns an escape sequence that draws the image at url inline
// through the terminal's own image protocol. width/height are terminal cells;
// the terminal scales the image itself, so no half-block art is produced.
// NativeNone is an error. Results are not cached.
func (r *Renderer) RenderNative(ctx context.Context, url string, width, height int) (string, error) {
	if width <= 0 || height <= 0 {
		return "", fmt.Errorf("cover: invalid size %dx%d", width, height)
	}
	kind := r.Native()
	if kind == NativeNone {
		return "", fmt.Errorf("cover: terminal has no native image protocol")
	}

	img, err := r.fetch(ctx, url)
	if err != nil {
		return "", err
	}
	// Shrink before base64: a 2000px cover becomes a 1 MB escape that the
	// terminal then throws away when it scales to cells.
	img = shrink(img, width*16, height*32)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("cover: encode: %w", err)
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())

	if kind == NativeITerm2 {
		return wrapTmux(fmt.Sprintf("\x1b]1337;File=inline=1;width=%dpx;height=%dpx;preserveAspectRatio=1:%s\x07",
			width*8, height*16, data)), nil
	}
	return wrapTmux(kittyEscape(width, height, data)), nil
}

// shrink downsamples only when the source is larger than the target; covers
// that are already small are decoded at native size.
func shrink(src image.Image, width, height int) image.Image {
	if width < 1 || height < 1 || src.Bounds().Dx() <= width && src.Bounds().Dy() <= height {
		return src
	}
	return resize(src, width, height)
}

// kittyEscape builds the graphics protocol command; a payload over 4096 bytes
// must be split across chunks linked by m=1/m=0.
func kittyEscape(width, height int, data string) string {
	const max = 4096
	var b strings.Builder
	for i := 0; i < len(data); i += max {
		end := i + max
		if end > len(data) {
			end = len(data)
		}
		last := end == len(data)
		b.WriteString("\x1b_G")
		if i == 0 {
			fmt.Fprintf(&b, "a=T,f=100,c=%d,r=%d,q=2", width, height)
			if !last {
				b.WriteString(",m=1")
			}
			b.WriteByte(';')
		} else if last {
			b.WriteString("m=0;")
		} else {
			b.WriteString("m=1;")
		}
		b.WriteString(data[i:end])
		b.WriteString("\x1b\\")
	}
	return b.String()
}

func (r *Renderer) fetch(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cover: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	client := r.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover: %s: %s", url, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cover: %w", err)
	}
	if len(body) > maxImageBytes {
		return nil, fmt.Errorf("cover: %s: image exceeds %d bytes", url, maxImageBytes)
	}

	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cover: decode %s: %w", url, err)
	}
	return img, nil
}

func resize(src image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// quadRunes maps a 2x2 pixel mask (TL=1, TR=2, BL=4, BR=8) to a block glyph.
var quadRunes = [16]rune{' ', '▘', '▝', '▀', '▖', '▌', '▞', '▛', '▗', '▚', '▐', '▜', '▄', '▙', '▟', '█'}

// renderQuadrants draws four pixels per cell with two cluster colors, so the
// same terminal area carries twice the horizontal detail of half blocks.
func renderQuadrants(img *image.RGBA, width, height int) string {
	var b strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fg, bg, mask := quadColors([4]color.RGBA{
				img.RGBAAt(x*2, y*2),
				img.RGBAAt(x*2+1, y*2),
				img.RGBAAt(x*2, y*2+1),
				img.RGBAAt(x*2+1, y*2+1),
			})
			if mask == 0 {
				fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm ", bg.R, bg.G, bg.B)
				continue
			}
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%c",
				fg.R, fg.G, fg.B, bg.R, bg.G, bg.B, quadRunes[mask])
		}
		b.WriteString("\x1b[0m")
		if y < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// quadColors splits four pixels into two color clusters and reports the mask of
// pixels closest to fg. Near-uniform blocks collapse to one background color.
func quadColors(p [4]color.RGBA) (fg, bg color.RGBA, mask int) {
	best, ai, bi := -1, 0, 0
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			if d := rgbDistance(p[i], p[j]); d > best {
				best, ai, bi = d, i, j
			}
		}
	}
	if best < 24 {
		m := meanColor(p)
		return m, m, 0
	}
	a, c := p[ai], p[bi]
	na, nc := 0, 0
	for pass := 0; pass < 2; pass++ {
		var sumA, sumC [3]int
		mask, na, nc = 0, 0, 0
		for i := 0; i < 4; i++ {
			if rgbDistance(p[i], a) <= rgbDistance(p[i], c) {
				mask |= 1 << i
				sumA[0] += int(p[i].R)
				sumA[1] += int(p[i].G)
				sumA[2] += int(p[i].B)
				na++
			} else {
				sumC[0] += int(p[i].R)
				sumC[1] += int(p[i].G)
				sumC[2] += int(p[i].B)
				nc++
			}
		}
		if na == 0 || nc == 0 {
			m := meanColor(p)
			return m, m, 0
		}
		a = color.RGBA{uint8(sumA[0] / na), uint8(sumA[1] / na), uint8(sumA[2] / na), 255}
		c = color.RGBA{uint8(sumC[0] / nc), uint8(sumC[1] / nc), uint8(sumC[2] / nc), 255}
	}
	return a, c, mask
}

func rgbDistance(a, b color.RGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

func meanColor(p [4]color.RGBA) color.RGBA {
	var r, g, b int
	for _, c := range p {
		r += int(c.R)
		g += int(c.G)
		b += int(c.B)
	}
	return color.RGBA{uint8(r / 4), uint8(g / 4), uint8(b / 4), 255}
}

func renderASCII(img *image.RGBA, width, height int) string {
	var b strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := img.RGBAAt(x, y)
			gray := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
			b.WriteByte(asciiRamp[gray*(len(asciiRamp)-1)/255])
		}
		if y < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
