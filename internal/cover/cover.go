// Package cover renders remote images as terminal art.
package cover

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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

// Renderer fetches and renders covers. Set TrueColor to override terminal
// detection after New.
type Renderer struct {
	HTTP      *http.Client
	TrueColor bool
	cache     map[string]string
	mu        sync.Mutex
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
// renders two pixel rows per line with half blocks; otherwise grayscale ASCII
// is used. Results are cached per url, size and mode.
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
		out = renderBlocks(resize(img, width, height*2), width, height)
	} else {
		out = renderASCII(resize(img, width, height), width, height)
	}

	r.mu.Lock()
	r.cache[key] = out
	r.mu.Unlock()
	return out, nil
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

func renderBlocks(img *image.RGBA, width, height int) string {
	var b strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			top := img.RGBAAt(x, y*2)
			bottom := img.RGBAAt(x, y*2+1)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bottom.R, bottom.G, bottom.B)
		}
		b.WriteString("\x1b[0m")
		if y < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
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
