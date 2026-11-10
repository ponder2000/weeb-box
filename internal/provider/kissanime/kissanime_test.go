package kissanime

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

func TestSearchFromWPJSON(t *testing.T) {
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-json/wp/v2/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search") != "alpha" || r.URL.Query().Get("per_page") != "20" {
			t.Errorf("bad query %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"title":"Alpha &#8211; Sub","url":"` + baseURL + `/anime/alpha/","subtype":"anime"},
			{"title":"Beta","url":"/anime/beta/","subtype":"page"},
			{"title":"Blog Post","url":"https://x/blog/post/","subtype":"post"},
			{"title":"Alpha &#8211; Sub","url":"` + baseURL + `/anime/alpha/","subtype":"anime"}
		]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	baseURL = srv.URL

	animes, err := New(srv.URL).Search(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 2 {
		t.Fatalf("got %+v", animes)
	}
	if animes[0].Title != "Alpha – Sub" || animes[0].URL != srv.URL+"/anime/alpha/" {
		t.Fatalf("first = %+v", animes[0])
	}
	if animes[1].Title != "Beta" || animes[1].URL != srv.URL+"/anime/beta/" {
		t.Fatalf("second = %+v", animes[1])
	}
}

func TestParseEpisodesSortedAndDeduped(t *testing.T) {
	html := `<div class="listing">
		<a href="https://x/show-episode-2/">Show Episode 2</a>
		<a href="https://x/show-episode-1/">Show Episode 1</a>
		<a href="https://x/show-episode-2/">Show Episode 2</a>
		<a href="https://x/show-movie/">Movie</a>
	</div>`

	episodes, err := parseEpisodes(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 {
		t.Fatalf("got %+v", episodes)
	}
	if episodes[0].Number != 1 || episodes[0].Title != "Show Episode 1" || episodes[1].Number != 2 {
		t.Fatalf("got %+v", episodes)
	}
}

// TestStreamChain covers episode page -> streaming.php -> megaplay -> AES -> m3u8
// against a fake server, so the whole path runs without the live site.
func TestStreamChain(t *testing.T) {
	var baseURL string
	fail := func(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusForbidden) }
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/anime/alpha/":
			w.Write([]byte(`<a href="` + baseURL + `/anime/alpha/alpha-episode-1/">Alpha Episode 1</a>`))
		case "/anime/alpha/alpha-episode-1/":
			w.Write([]byte(`<iframe src="/player/streaming.php?id=1"></iframe>`))
		case "/player/streaming.php":
			w.Write([]byte(`<iframe src="/megaplay/stream/1/1/sub"></iframe>`))
		case "/megaplay/stream/1/1/sub":
			if r.Header.Get("Referer") != baseURL+"/player/streaming.php?id=1" {
				fail(w, "megaplay page needs player referer")
				return
			}
			w.Write([]byte(`<div id="megaplay-player" data-id="abc123"></div>`))
		case "/stream/getSourcesNew":
			if r.URL.Query().Get("id") != "abc123" {
				t.Errorf("bad id %q", r.URL.RawQuery)
			}
			if r.Header.Get("Referer") != baseURL+"/megaplay/stream/1/1/sub" || r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				fail(w, "bad sources headers")
				return
			}
			w.Write([]byte(`{"enc":"wdeBruh3qqn_i5wUNnyaPWiRLLdfX1H1YKBxoBvLhJuin6WMxjzk9aZMRezhoHTo"}`))
		default:
			fail(w, "unexpected path "+r.URL.Path)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	baseURL = srv.URL

	a := New(srv.URL)
	anime := provider.Anime{Title: "Alpha", URL: "/anime/alpha/"}
	episodes, err := a.Episodes(context.Background(), anime)
	if err != nil || len(episodes) != 1 || episodes[0].Number != 1 {
		t.Fatalf("episodes: %v %+v", err, episodes)
	}
	if episodes[0].URL != srv.URL+"/anime/alpha/alpha-episode-1/" {
		t.Fatalf("episode url = %q", episodes[0].URL)
	}

	stream, err := a.Stream(context.Background(), episodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://cdn.example/x/master.m3u8" || stream.Referer != srv.URL+"/" {
		t.Fatalf("got %+v", stream)
	}
}

func TestStreamMissingPlayer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>no player here</body></html>`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Stream(context.Background(), provider.Episode{Number: 1, URL: srv.URL + "/x-episode-1/"})
	if err == nil || !strings.Contains(err.Error(), "episode player") {
		t.Fatalf("got %v", err)
	}
}

func TestLive(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1 to hit kissanime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adapter := New("")
	animes, err := adapter.Search(ctx, "naruto")
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
