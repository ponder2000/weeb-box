package anizone

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider"
)

func TestSearch(t *testing.T) {
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-json/wp/v2/az_series", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search") != "alpha & beta" || r.URL.Query().Get("per_page") != "20" {
			t.Errorf("bad query %q", r.URL.RawQuery)
		}
		w.Write([]byte(`[
			{"title":{"rendered":"Alpha &amp; Beta"},"link":"` + baseURL + `/anime/alpha/"},
			{"title":{"rendered":"Gamma"},"link":"` + baseURL + `/anime/gamma/"},
			{"title":{"rendered":"Alpha &amp; Beta"},"link":"` + baseURL + `/anime/alpha/"},
			{"title":{"rendered":""},"link":"` + baseURL + `/anime/no-title/"}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	baseURL = srv.URL

	animes, err := New(srv.URL+"/").Search(context.Background(), "alpha & beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 2 || animes[0].Title != "Alpha & Beta" || animes[1].Title != "Gamma" {
		t.Fatalf("got %+v", animes)
	}
	if animes[0].URL != srv.URL+"/anime/alpha/" {
		t.Fatalf("got %q", animes[0].URL)
	}
}

func TestSearchEmpty(t *testing.T) {
	animes, err := parseSearch(strings.NewReader(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 0 {
		t.Fatalf("got %+v", animes)
	}
}

func TestEpisodesSortedDedupedAndResolved(t *testing.T) {
	html := `<div class="episodes">
		<a href="/watch/show-episode-2/">Episode 2</a>
		<a href="/watch/show-episode-1/">Episode 1</a>
		<a href="/watch/show-episode-2/">Episode 2</a>
		<a href="/anime/show/">series</a>
		<a href="/watch/show-movie/">special</a>
	</div>`

	episodes, err := parseEpisodes(strings.NewReader(html), "https://x.example/anime/show/")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 {
		t.Fatalf("got %+v", episodes)
	}
	if episodes[0].Number != 1 || episodes[0].URL != "https://x.example/watch/show-episode-1/" {
		t.Fatalf("got %+v", episodes[0])
	}
	if episodes[0].Title != "Episode 1" || episodes[1].Number != 2 {
		t.Fatalf("got %+v", episodes)
	}
}

func TestEpisodesIgnoreRecommendedLinks(t *testing.T) {
	html := `<section class="az-episode-browser"><div class="az-ep-grid">
		<a href="/watch/show-episode-1/" aria-label="Watch Episode 1">1</a>
	</div></section>
	<aside><a href="/watch/other-show-episode-1/">other show</a></aside>`

	episodes, err := parseEpisodes(strings.NewReader(html), "https://x.example/anime/show/")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 || episodes[0].Title != "" || episodes[0].Number != 1 {
		t.Fatalf("got %+v", episodes)
	}
}

func TestStreamMissingIframe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>no player here</body></html>`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Stream(context.Background(), provider.Episode{Number: 1, URL: srv.URL + "/watch/x-episode-1/"})
	if err == nil || !strings.Contains(err.Error(), "episode player") {
		t.Fatalf("got %v", err)
	}
}

func TestNewTrimsAndDefaultsBaseURL(t *testing.T) {
	if got := New("").BaseURL; got != DefaultBaseURL {
		t.Fatalf("empty: %q", got)
	}
	if got := New("  https://mirror.example/  ").BaseURL; got != "https://mirror.example" {
		t.Fatalf("trimmed: %q", got)
	}
}

func TestLiveStream(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1 to hit anizone.io")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adapter := New("")
	animes, err := adapter.Search(ctx, "immortal legend")
	if err != nil || len(animes) == 0 {
		t.Fatalf("search: %v (%d results)", err, len(animes))
	}
	episodes, err := adapter.Episodes(ctx, animes[0])
	if err != nil || len(episodes) == 0 {
		t.Fatalf("episodes: %v (%d episodes)", err, len(episodes))
	}
	stream, err := adapter.Stream(ctx, episodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stream.URL, ".m3u8") {
		t.Fatalf("expected HLS url, got %s", stream.URL)
	}
}
