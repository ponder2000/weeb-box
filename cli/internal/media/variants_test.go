package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ponder2000/weeb-box/internal/provider"
)

const variantMasterPlaylist = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080
high/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720
mid/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720
mid-alt/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=854x480
low/index.m3u8
`

func TestVariantsMaster(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(variantMasterPlaylist))
	}))
	defer server.Close()

	got, err := Variants(context.Background(), server.Client(),
		provider.Stream{URL: server.URL + "/master.m3u8"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1080p", "720p", "480p"}; !reflect.DeepEqual(got, want) {
		t.Errorf("variants = %v, want %v", got, want)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1", requests)
	}
}

func TestVariantsMediaPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\nseg0.ts\n"))
	}))
	defer server.Close()

	got, err := Variants(context.Background(), server.Client(),
		provider.Stream{URL: server.URL + "/index.m3u8"})
	if err != nil || got != nil {
		t.Errorf("variants = %v err=%v, want nil nil", got, err)
	}
}

func TestVariantsNonHLS(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()

	for _, raw := range []string{"", "https://cdn.example/ep.mp4", server.URL + "/playlist"} {
		got, err := Variants(context.Background(), server.Client(), provider.Stream{URL: raw})
		if err != nil || got != nil {
			t.Errorf("%q: variants = %v err=%v", raw, got, err)
		}
	}
	if requests != 0 {
		t.Errorf("made %d requests for non-HLS urls", requests)
	}
}

func TestVariantsFetchError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	if _, err := Variants(context.Background(), server.Client(),
		provider.Stream{URL: server.URL + "/master.m3u8"}); err == nil {
		t.Fatal("expected an error for a 404 playlist")
	}
}

// The Referer is forwarded when set, and a stream without one still resolves.
func TestVariantsReferer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "https://megaplay.buzz/"; r.Header.Get("Referer") != want {
			t.Errorf("referer = %q, want %q", r.Header.Get("Referer"), want)
		}
		_, _ = w.Write([]byte(variantMasterPlaylist))
	}))
	defer server.Close()

	got, err := Variants(context.Background(), server.Client(), provider.Stream{
		URL:     server.URL + "/master.m3u8",
		Referer: "https://megaplay.buzz/",
	})
	if err != nil || len(got) != 3 {
		t.Fatalf("variants = %v err=%v", got, err)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "" {
			t.Errorf("unexpected referer %q", r.Header.Get("Referer"))
		}
		_, _ = w.Write([]byte(variantMasterPlaylist))
	}))
	defer plain.Close()

	got, err = Variants(context.Background(), plain.Client(),
		provider.Stream{URL: plain.URL + "/master.m3u8"})
	if err != nil || len(got) != 3 {
		t.Fatalf("no-referer variants = %v err=%v", got, err)
	}
}
