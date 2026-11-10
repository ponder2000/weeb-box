package cover

import (
	"bytes"
	"context"
	"encoding/base64"
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

func TestNativeDetect(t *testing.T) {
	clear := func() {
		for _, k := range []string{"KITTY_WINDOW_ID", "TERM", "TERM_PROGRAM", "LC_TERMINAL"} {
			t.Setenv(k, "")
		}
	}
	cases := []struct {
		name string
		env  map[string]string
		want NativeKind
	}{
		{"none", nil, NativeNone},
		{"kitty window", map[string]string{"KITTY_WINDOW_ID": "1"}, NativeKitty},
		{"kitty term", map[string]string{"TERM": "xterm-kitty"}, NativeKitty},
		{"iterm program", map[string]string{"TERM_PROGRAM": "iTerm.app"}, NativeITerm2},
		{"iterm lc", map[string]string{"LC_TERMINAL": "iTerm2"}, NativeITerm2},
		{"kitty beats iterm", map[string]string{"KITTY_WINDOW_ID": "1", "TERM_PROGRAM": "iTerm.app"}, NativeKitty},
	}
	for _, tc := range cases {
		clear()
		for k, v := range tc.env {
			t.Setenv(k, v)
		}
		if got := New().Native(); got != tc.want {
			t.Errorf("%s: Native() = %d, want %d", tc.name, got, tc.want)
		}
	}

	r := New()
	r.SetNative(NativeITerm2)
	t.Setenv("KITTY_WINDOW_ID", "1")
	if got := r.Native(); got != NativeITerm2 {
		t.Errorf("override: Native() = %d, want %d", got, NativeITerm2)
	}
}

func TestRenderNativeITerm2(t *testing.T) {
	var requests int32
	server := imageServer(t, &requests)
	r := New()
	r.SetNative(NativeITerm2)

	out, err := r.RenderNative(context.Background(), server.URL, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "\x1b]1337;File=inline=1;width=32px;height=48px;preserveAspectRatio=1:"
	if !strings.HasPrefix(out, prefix) {
		t.Fatalf("prefix = %q, want %q", out[:min(len(out), 80)], prefix)
	}
	if !strings.HasSuffix(out, "\x07") {
		t.Fatalf("missing BEL suffix: %q", out)
	}
	if atomic.LoadInt32(&requests) != 1 {
		t.Errorf("server requests = %d, want 1", requests)
	}
	decodePNGPayload(t, strings.TrimSuffix(strings.TrimPrefix(out, prefix), "\x07"))
}

func TestRenderNativeKitty(t *testing.T) {
	var requests int32
	server := imageServer(t, &requests)
	r := New()
	r.SetNative(NativeKitty)

	out, err := r.RenderNative(context.Background(), server.URL, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "\x1b_Ga=T,f=100,c=4,r=3,q=2;"
	if !strings.HasPrefix(out, prefix) {
		t.Fatalf("prefix = %q, want %q", out[:min(len(out), 80)], prefix)
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("missing ST suffix: %q", out)
	}
	if strings.Contains(out, "m=1") {
		t.Errorf("small image should be a single chunk: %q", out)
	}
	decodePNGPayload(t, strings.TrimSuffix(strings.TrimPrefix(out, prefix), "\x1b\\"))
}

func TestRenderNativeNone(t *testing.T) {
	var requests int32
	server := imageServer(t, &requests)
	r := New()
	r.SetNative(NativeNone)

	if _, err := r.RenderNative(context.Background(), server.URL, 2, 2); err == nil {
		t.Fatal("expected error without a native protocol")
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Errorf("server requests = %d, want 0", requests)
	}
}

func TestRenderNativeInvalidSize(t *testing.T) {
	r := New()
	r.SetNative(NativeKitty)
	if _, err := r.RenderNative(context.Background(), "http://example.com/x.png", 0, 2); err == nil {
		t.Fatal("expected error on zero width")
	}
}

func TestKittyChunks(t *testing.T) {
	data := strings.Repeat("A", 9000)
	out := kittyEscape(2, 3, data)
	if n := strings.Count(out, "\x1b_G"); n != 3 {
		t.Fatalf("chunks = %d, want 3", n)
	}
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100,c=2,r=3,q=2,m=1;") {
		t.Errorf("first chunk header wrong: %q", out[:50])
	}
	if !strings.HasSuffix(out, "m=0;"+data[8192:]+"\x1b\\") {
		t.Errorf("last chunk wrong: %q", out[len(out)-60:])
	}

	var got strings.Builder
	for _, seq := range strings.Split(out, "\x1b\\")[:3] {
		got.WriteString(seq[strings.IndexByte(seq, ';')+1:])
	}
	if got.String() != data {
		t.Errorf("reassembled payload = %d bytes, want %d", got.Len(), len(data))
	}
}

func decodePNGPayload(t *testing.T, payload string) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("payload is not PNG: %v", err)
	}
}
