package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider"
)

const masterPlaylist = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360,NAME="360p"
low/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,NAME="720p"
mid/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,NAME="1080p"
https://cdn.example/high/index.m3u8
`

func playlistServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://megaplay.buzz/" {
			t.Errorf("missing referer on %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("missing UA on %s", r.URL.Path)
		}
		if r.URL.Path != "/hls/master.m3u8" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(masterPlaylist))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchVariants(t *testing.T) {
	server := playlistServer(t)
	src := provider.Stream{URL: server.URL + "/hls/master.m3u8", Referer: "https://megaplay.buzz/"}

	variants, err := fetchVariants(context.Background(), server.Client(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 3 {
		t.Fatalf("variants = %+v", variants)
	}
	got := variants[1]
	if got.bandwidth != 2500000 || got.height != 720 || got.name != "720p" {
		t.Errorf("variant = %+v", got)
	}
	if want := server.URL + "/hls/mid/index.m3u8"; got.url != want {
		t.Errorf("relative uri = %q, want %q", got.url, want)
	}
	if want := "https://cdn.example/high/index.m3u8"; variants[2].url != want {
		t.Errorf("absolute uri = %q, want %q", variants[2].url, want)
	}
}

func TestSelectQualityExplicit(t *testing.T) {
	server := playlistServer(t)
	src := provider.Stream{
		URL:       server.URL + "/hls/master.m3u8",
		Referer:   "https://megaplay.buzz/",
		Subtitles: []provider.Subtitle{{URL: "https://cdn.example/sub.vtt", Label: "English"}},
	}
	tests := []struct {
		preference string
		want       string
	}{
		{"1080p", "https://cdn.example/high/index.m3u8"},
		{"720p", server.URL + "/hls/mid/index.m3u8"},
		{"1080", "https://cdn.example/high/index.m3u8"},
		{"2160p", "https://cdn.example/high/index.m3u8"}, // closest lower
		{"900p", server.URL + "/hls/mid/index.m3u8"},     // closest lower
		{"240p", server.URL + "/hls/low/index.m3u8"},     // lowest
	}
	for _, tt := range tests {
		selected, err := SelectQuality(context.Background(), server.Client(), src, tt.preference)
		if err != nil {
			t.Fatalf("%s: %v", tt.preference, err)
		}
		if selected.URL != tt.want {
			t.Errorf("%s: url = %q, want %q", tt.preference, selected.URL, tt.want)
		}
		if selected.Referer != src.Referer || len(selected.Subtitles) != 1 {
			t.Errorf("%s: lost stream metadata: %+v", tt.preference, selected)
		}
	}
}

// A master whose variant playlists are unreachable must not fail; auto falls
// back to 720p.
func TestSelectQualityAutoFallback(t *testing.T) {
	server := playlistServer(t)
	src := provider.Stream{URL: server.URL + "/hls/master.m3u8", Referer: "https://megaplay.buzz/"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	selected, err := SelectQuality(ctx, server.Client(), src, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if want := server.URL + "/hls/mid/index.m3u8"; selected.URL != want {
		t.Errorf("url = %q, want %q", selected.URL, want)
	}
}

func TestSelectQualityAutoPicksFastVariant(t *testing.T) {
	segment := make([]byte, probeBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=100000,RESOLUTION=640x360
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=1280x720
high.m3u8
`))
		case "/low.m3u8", "/high.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\nseg.ts\n"))
		case "/seg.ts":
			if r.Header.Get("Range") != "bytes=0-262143" {
				t.Errorf("range = %q", r.Header.Get("Range"))
			}
			_, _ = w.Write(segment)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	selected, err := SelectQuality(context.Background(), server.Client(),
		provider.Stream{URL: server.URL + "/master.m3u8"}, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if want := server.URL + "/high.m3u8"; selected.URL != want {
		t.Errorf("url = %q, want %q", selected.URL, want)
	}
}

func TestSelectQualityNonHLS(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, context.DeadlineExceeded
	})}
	for _, raw := range []string{"", "https://cdn.example/ep.mp4", "https://cdn.example/playlist"} {
		src := provider.Stream{URL: raw, Referer: "https://megaplay.buzz/"}
		selected, err := SelectQuality(context.Background(), client, src, "720p")
		if err != nil || selected.URL != src.URL || selected.Referer != src.Referer {
			t.Errorf("%q: stream = %+v err=%v", raw, selected, err)
		}
	}
	if requests != 0 {
		t.Errorf("made %d requests for non-HLS urls", requests)
	}
}

// A media playlist has no #EXT-X-STREAM-INF lines, so it is passed through
// even though the URL ends in .m3u8.
func TestSelectQualityMediaPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\nseg0.ts\n"))
	}))
	defer server.Close()

	src := provider.Stream{URL: server.URL + "/index.m3u8"}
	selected, err := SelectQuality(context.Background(), server.Client(), src, "1080p")
	if err != nil || selected.URL != src.URL {
		t.Fatalf("stream = %+v err=%v", selected, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestParseAttrs(t *testing.T) {
	attrs := parseAttrs(`BANDWIDTH=2500000,NAME="English, 720p",RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2"`)
	if attrs["BANDWIDTH"] != "2500000" || attrs["NAME"] != "English, 720p" || attrs["RESOLUTION"] != "1280x720" {
		t.Fatalf("attrs = %v", attrs)
	}
	if !strings.Contains(attrs["CODECS"], "avc1.4d401f,mp4a.40.2") {
		t.Errorf("codecs = %q", attrs["CODECS"])
	}
}
