package hianime

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

func TestParseSearch(t *testing.T) {
	html := `<section class="hk-grid hk-results">
		<article class="hk-tile">
			<h3 class="hk-tile__name"><a href="https://x/details/alpha-1a877" title="Alpha (TV, 1999)"><span class="hk-dual" data-en="Alpha" data-jp="Arufa">Alpha</span></a></h3>
			<p class="hk-tile__meta"><span>TV</span><span class="hk-dot"></span><span>1999</span></p>
		</article>
		<article class="hk-tile">
			<h3 class="hk-tile__name"><a href="https://x/details/beta-c1586" title="Beta (Movie, 2024)">Beta</a></h3>
			<p class="hk-tile__meta"><span>Movie</span><span class="hk-dot"></span><span>2024</span></p>
		</article>
		<article class="hk-tile">
			<h3 class="hk-tile__name"><a href="https://x/details/alpha-1a877" title="Alpha (TV, 1999)">Alpha</a></h3>
		</article>
	</section>`

	animes, err := parseSearch(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 2 {
		t.Fatalf("got %+v", animes)
	}
	if animes[0].Title != "Alpha" || animes[0].URL != "https://x/details/alpha-1a877" || animes[0].Meta != "TV · 1999" {
		t.Fatalf("alpha: %+v", animes[0])
	}
	if animes[1].Title != "Beta" || animes[1].Meta != "Movie · 2024" {
		t.Fatalf("beta: %+v", animes[1])
	}
}

func TestParseEpisodesSortedAndDeduped(t *testing.T) {
	html := `<div class="hk-epgrid">
		<a class="hk-ep" href="https://x/watch-show-episode-2-1a877" data-ep="2" data-name="Second">2</a>
		<a class="hk-ep" href="https://x/watch-show-episode-1-1a877" data-ep="1" data-name="First">1</a>
		<a class="hk-ep" href="https://x/watch-show-episode-2-1a877" data-ep="2" data-name="Second">2</a>
		<a class="hk-ep" href="https://x/watch-show-episode-10-1a877" data-ep="10" data-name="Tenth">10</a>
	</div><a href="https://x/watch-show-episode-99-1a877">next</a>`

	episodes, err := parseEpisodes(strings.NewReader(html), "Show")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 3 {
		t.Fatalf("got %+v", episodes)
	}
	if episodes[0].Number != 1 || episodes[0].Title != "First" || episodes[1].Number != 2 || episodes[2].Number != 10 {
		t.Fatalf("got %+v", episodes)
	}
}

func TestParseEpisodesMovieFallback(t *testing.T) {
	html := `<div class="hk-title__cta">
		<a class="hk-btn" href="https://x/watch-alpha-movie-c1586">Watch the movie</a>
	</div>`

	episodes, err := parseEpisodes(strings.NewReader(html), "Alpha Movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 || episodes[0].Number != 1 || episodes[0].URL != "https://x/watch-alpha-movie-c1586" || episodes[0].Title != "Alpha Movie" {
		t.Fatalf("got %+v", episodes)
	}
}

func TestParseWatch(t *testing.T) {
	html := `<article class="hk-watch" data-hk-watch='{"movie":true,"ep":1,"sid":1029,"token":"OTYwNzE6YjQ4NmZhZDc","lang":"dub","gate":0}'></article>`

	cfg, err := parseWatch([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Movie || cfg.Ep != 1 || cfg.Token != "OTYwNzE6YjQ4NmZhZDc" || cfg.Lang != "dub" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestParseWatchMissing(t *testing.T) {
	_, err := parseWatch([]byte(`<html><body>no player</body></html>`))
	if err == nil || !strings.Contains(err.Error(), "data-hk-watch") {
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

// TestStreamChain covers the whole page -> token -> /stream redirect -> MegaPlay
// -> AES -> m3u8 path against two fake servers, without touching the live site.
func TestStreamChain(t *testing.T) {
	var site, player string
	fail := func(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusForbidden) }

	playerMux := http.NewServeMux()
	playerMux.HandleFunc("/stream/s-2/2143/sub", func(w http.ResponseWriter, r *http.Request) {
		// MegaPlay serves a 410 error page without a Referer.
		if r.Referer() == "" {
			fail(w, "megaplay page needs a referer")
			return
		}
		w.Write([]byte(`<div id="megaplay-player" data-id="36389"></div>`))
	})
	playerMux.HandleFunc("/stream/getSourcesNew", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "36389" {
			t.Errorf("bad id %q", r.URL.RawQuery)
		}
		if r.Referer() != player+"/stream/s-2/2143/sub" {
			fail(w, "sources need the player referer, got "+r.Referer())
			return
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			fail(w, "sources need XHR header")
			return
		}
		w.Write([]byte(`{"enc":"wdeBruh3qqn_i5wUNnyaPWiRLLdfX1H1YKBxoBvLhJuin6WMxjzk9aZMRezhoHTo",` +
			`"tracks":[{"file":"https://cdn.example/en.vtt","label":"English","kind":"captions","default":true}]}`))
	})
	playerSrv := httptest.NewServer(playerMux)
	defer playerSrv.Close()
	player = playerSrv.URL

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search":
			if r.URL.Query().Get("q") != "alpha" {
				t.Errorf("bad query %q", r.URL.RawQuery)
			}
			w.Write([]byte(`<article class="hk-tile"><h3 class="hk-tile__name"><a href="` + site + `/details/alpha-1a877">Alpha</a></h3></article>`))
		case r.URL.Path == "/details/alpha-1a877":
			w.Write([]byte(`<div class="hk-epgrid"><a class="hk-ep" href="` + site + `/watch-alpha-episode-2-1a877" data-ep="2" data-name="Two">2</a>` +
				`<a class="hk-ep" href="` + site + `/watch-alpha-episode-1-1a877" data-ep="1" data-name="One">1</a></div>`))
		case r.URL.Path == "/watch-alpha-episode-1-1a877":
			w.Write([]byte(`<article data-hk-watch='{"ep":1,"token":"tok-1","lang":"sub","gate":0}'></article>`))
		case r.URL.Path == "/stream/tok-1/sub":
			http.Redirect(w, r, player+"/stream/s-2/2143/sub", http.StatusFound)
		default:
			fail(w, "unexpected path "+r.URL.Path)
		}
	})
	siteSrv := httptest.NewServer(mux)
	defer siteSrv.Close()
	site = siteSrv.URL

	a := New(siteSrv.URL + "/")
	animes, err := a.Search(context.Background(), "alpha")
	if err != nil || len(animes) != 1 {
		t.Fatalf("search: %v %+v", err, animes)
	}
	episodes, err := a.Episodes(context.Background(), animes[0])
	if err != nil || len(episodes) != 2 || episodes[0].Number != 1 || episodes[1].Number != 2 {
		t.Fatalf("episodes: %v %+v", err, episodes)
	}
	stream, err := a.Stream(context.Background(), episodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://cdn.example/x/master.m3u8" || stream.Referer != player+"/" {
		t.Fatalf("got %+v", stream)
	}
	if len(stream.Subtitles) != 1 || !stream.Subtitles[0].Default {
		t.Fatalf("subtitles: %+v", stream.Subtitles)
	}
}

func TestStreamNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<article data-hk-watch='{"ep":1,"lang":"sub"}'></article>`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Stream(context.Background(), provider.Episode{Number: 1, URL: srv.URL + "/watch-x-episode-1-1a877"})
	if err == nil || !strings.Contains(err.Error(), "no stream token") {
		t.Fatalf("got %v", err)
	}
}

func TestStreamNoRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/stream/") {
			w.Write([]byte(`<html>proxied player page</html>`))
			return
		}
		w.Write([]byte(`<article data-hk-watch='{"ep":1,"token":"tok-1","lang":"sub"}'></article>`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Stream(context.Background(), provider.Episode{Number: 1, URL: srv.URL + "/watch-x-episode-1-1a877"})
	if err == nil || !strings.Contains(err.Error(), "did not redirect") {
		t.Fatalf("got %v", err)
	}
}

func TestLive(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1 to hit hianime.ms")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adapter := New("")
	animes, err := adapter.Search(ctx, "one piece")
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
	if len(stream.Subtitles) == 0 {
		t.Fatal("expected caption tracks")
	}
}
