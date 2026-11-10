package cover

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 60), G: uint8(y * 60), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func imageServer(t *testing.T, requests *int32) *httptest.Server {
	t.Helper()
	data := testPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requests, 1)
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("missing browser UA for %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server
}

func lines(out string) []string { return strings.Split(out, "\n") }

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderTrueColor(t *testing.T) {
	var requests int32
	server := imageServer(t, &requests)
	r := New()
	r.TrueColor = true

	out, err := r.Render(context.Background(), server.URL, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	rows := lines(out)
	if len(rows) != 3 {
		t.Fatalf("lines = %d, want 3", len(rows))
	}
	for _, row := range rows {
		if !strings.HasSuffix(row, "\x1b[0m") {
			t.Errorf("row without reset: %q", row)
		}
		if n := utf8.RuneCountInString(ansi.ReplaceAllString(row, "")); n != 4 {
			t.Errorf("row width = %d, want 4", n)
		}
	}
	if !strings.Contains(out, "\x1b[38;2;") || !strings.Contains(out, "\x1b[48;2;") {
		t.Errorf("missing 24-bit color: %q", out)
	}
}

func TestRenderASCII(t *testing.T) {
	var requests int32
	server := imageServer(t, &requests)
	r := New()
	r.TrueColor = false

	out, err := r.Render(context.Background(), server.URL, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("ASCII output has ANSI: %q", out)
	}
	rows := lines(out)
	if len(rows) != 3 {
		t.Fatalf("lines = %d, want 3", len(rows))
	}
	for _, row := range rows {
		if n := utf8.RuneCountInString(row); n != 4 {
			t.Errorf("row width = %d, want 4", n)
		}
		for _, c := range row {
			if !strings.ContainsRune(asciiRamp, c) {
				t.Errorf("row %q has ramp-external %q", row, c)
			}
		}
	}
}

func TestRenderCache(t *testing.T) {
	var requests int32
	server := imageServer(t, &requests)
	r := New()
	r.TrueColor = true

	first, err := r.Render(context.Background(), server.URL, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Render(context.Background(), server.URL, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("cached output differs")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("server requests = %d, want 1", got)
	}
}

func TestRenderNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	if _, err := New().Render(context.Background(), server.URL, 2, 2); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestRenderInvalidSize(t *testing.T) {
	if _, err := New().Render(context.Background(), "http://example.com/x.png", 0, 2); err == nil {
		t.Fatal("expected error on zero width")
	}
}
