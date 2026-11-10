package animekai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ponder2000/weeb-box/internal/provider"
)

func TestParseSearch(t *testing.T) {
	body := `[
		{"title":"Cocoon: One Summer of Girlhood &#8211; Dub","url":"https://x/anime/cocoon-dub/","subtype":"anime"},
		{"title":"Not An Anime","url":"https://x/2026/11/post/","subtype":"post"},
		{"title":"Anime By URL &amp; Title","url":"https://x/anime/by-url/","subtype":"post"},
		{"title":"Cocoon: One Summer of Girlhood &#8211; Dub","url":"https://x/anime/cocoon-dub/","subtype":"anime"}
	]`

	animes, err := parseSearch(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 2 {
		t.Fatalf("got %+v", animes)
	}
	if animes[0].Title != "Cocoon: One Summer of Girlhood – Dub" || animes[0].URL != "https://x/anime/cocoon-dub/" {
		t.Fatalf("first: %+v", animes[0])
	}
	if animes[1].Title != "Anime By URL & Title" {
		t.Fatalf("second: %+v", animes[1])
	}
}

func TestParseEpisodesSortedAndDeduped(t *testing.T) {
	html := `<section id="ak-episodes"><ul class="ak-ep-grid">
		<li><a href="https://x/show-episode-2/">02</a></li>
		<li><a href="https://x/show-episode-10/">10</a></li>
		<li><a href="https://x/show-episode-1/"> 01 </a></li>
		<li><a href="https://x/show-episode-2/">02</a></li>
	</ul></section>
	<aside class="sidebar"><a href="https://x/other-show-episode-7/">07</a></aside>`

	episodes, err := parseEpisodes(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 3 {
		t.Fatalf("got %+v", episodes)
	}
	if episodes[0].Number != 1 || episodes[0].Title != "01" {
		t.Fatalf("first: %+v", episodes[0])
	}
	if episodes[1].Number != 2 || episodes[2].Number != 10 {
		t.Fatalf("order: %+v", episodes)
	}
}

func TestParseEpisodesWithoutGrid(t *testing.T) {
	html := `<a href="https://x/show-episode-1/">01</a>`
	episodes, err := parseEpisodes(strings.NewReader(html))
	if err != nil || len(episodes) != 1 || episodes[0].Number != 1 {
		t.Fatalf("%v %+v", err, episodes)
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

// TestStreamChain covers episode page -> newplayer -> megaplay -> AES -> m3u8
// against a fake server, so normal tests never hit the network.
func TestStreamChain(t *testing.T) {
	var baseURL string
	fail := func(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusForbidden) }
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wp-json/wp/v2/search":
			if r.URL.Query().Get("search") != "cocoon" || r.URL.Query().Get("per_page") != "20" {
				t.Errorf("bad query %q", r.URL.RawQuery)
			}
			w.Write([]byte(`[{"title":"Cocoon","url":"` + baseURL + `/anime/cocoon/","subtype":"anime"}]`))
		case "/anime/cocoon/":
			w.Write([]byte(`<section id="ak-episodes"><ul>
				<li><a href="` + baseURL + `/cocoon-episode-1/">01</a></li>
			</ul></section>`))
		case "/cocoon-episode-1/":
			w.Write([]byte(`<iframe src="/newplayer.php?ep=1"></iframe>`))
		case "/newplayer.php":
			w.Write([]byte(`<iframe src="stream/mal/1/1/sub"></iframe>`))
		case "/stream/mal/1/1/sub":
			w.Write([]byte(`<div id="megaplay-player" data-id="abc123"></div>`))
		case "/stream/getSourcesNew":
			if r.URL.Query().Get("id") != "abc123" {
				t.Errorf("bad id %q", r.URL.RawQuery)
			}
			w.Write([]byte(`{"enc":"wdeBruh3qqn_i5wUNnyaPWiRLLdfX1H1YKBxoBvLhJuin6WMxjzk9aZMRezhoHTo"}`))
		default:
			fail(w, "unexpected path "+r.URL.Path)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	baseURL = srv.URL

	a := New(srv.URL + "/")
	animes, err := a.Search(context.Background(), "cocoon")
	if err != nil || len(animes) != 1 {
		t.Fatalf("search: %v %+v", err, animes)
	}
	episodes, err := a.Episodes(context.Background(), animes[0])
	if err != nil || len(episodes) != 1 || episodes[0].Number != 1 {
		t.Fatalf("episodes: %v %+v", err, episodes)
	}
	stream, err := a.Stream(context.Background(), episodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://cdn.example/x/master.m3u8" || stream.Referer != srv.URL+"/" {
		t.Fatalf("got %+v", stream)
	}
}

func TestStreamMissingIframe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>no player here</body></html>`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Stream(context.Background(), provider.Episode{Number: 1, URL: srv.URL + "/x-episode-1/"})
	if err == nil || !strings.Contains(err.Error(), "episode player") {
		t.Fatalf("got %v", err)
	}
}

func TestSearchNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).Search(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v", err)
	}
}
