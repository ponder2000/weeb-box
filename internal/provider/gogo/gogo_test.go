package gogo

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider"
)

func TestParseSearch(t *testing.T) {
	html := `<div class="postbody"><div class="listupd">
		<article class="bs"><a itemprop="url" href="https://x/anime/alpha/" title="Alpha">Alpha</a></article>
		<article class="bs"><a itemprop="url" href="https://x/anime/beta/" title="Beta">Beta</a></article>
		<article class="bs"><a itemprop="url" href="https://x/anime/alpha/" title="Alpha">Alpha</a></article>
	</div></div>`

	animes, err := parseSearch(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 2 || animes[0].Title != "Alpha" || animes[1].Title != "Beta" {
		t.Fatalf("got %+v", animes)
	}
}

func TestParseEpisodesSortedAndDeduped(t *testing.T) {
	html := `<div class="eplister"><ul>
		<li><a href="https://x/show-episode-2/"><div class="epl-num">2</div></a></li>
		<li><a href="https://x/show-episode-1/"><div class="epl-num">1</div></a></li>
		<li><a href="https://x/show-episode-2/"><div class="epl-num">2</div></a></li>
	</ul></div><a href="https://x/show-episode-99/">next</a>`

	episodes, err := parseEpisodes(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 || episodes[0].Number != 1 || episodes[1].Number != 2 {
		t.Fatalf("got %+v", episodes)
	}
}

func TestDecryptSources(t *testing.T) {
	enc := "wdeBruh3qqn_i5wUNnyaPcXqidp1UWP84FfPHzGyKXAnifiddIPaR0t7-4y_3oD_d4IhuoQz-H_gSEcS" +
		"meaWJq5Iwi_8J2Wpdg8CnQVCIrj7g8KEk7eibAcgOpboVqt9BMi8KuRY8AUaaSDX5KONWxS1fG00cVB-" +
		"q7lbtnAN1eA"
	got, err := decryptSources(enc)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"file":"https://fetch.nexabloom.top/anime/c55a26472f8223cd7717aa736f09dffc/6db6dd99f070ec52b283886b65c9adf8/master.m3u8"}`
	if got != want {
		t.Fatalf("got %s", got)
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

func TestParseSearchEmptyResults(t *testing.T) {
	html := `<div class="postbody"><div class="listupd"><center><h3>Not Found</h3></center></div></div>`
	animes, err := parseSearch(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(animes) != 0 {
		t.Fatalf("got %+v", animes)
	}
}

// TestStreamChain runs the whole iframe -> megaplay -> AES -> m3u8 path against a
// fake server, which is the only way to cover it without hitting the live site.
func TestStreamChain(t *testing.T) {
	var baseURL string
	fail := func(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusForbidden) }
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if r.URL.Query().Get("s") != "alpha" {
				t.Errorf("bad query %q", r.URL.RawQuery)
			}
			w.Write([]byte(`<div class="postbody"><div class="listupd">
				<article class="bs"><a itemprop="url" href="` + baseURL + `/anime/alpha/" title="Alpha">Alpha</a></article>
			</div></div>`))
		case "/anime/alpha/":
			w.Write([]byte(`<div class="eplister"><ul>
				<li><a href="` + baseURL + `/alpha-episode-1/"><div class="epl-num">1</div></a></li>
			</ul></div>`))
		case "/alpha-episode-1/":
			w.Write([]byte(`<iframe src="/newplayer.php?ep=1"></iframe>`))
		case "/newplayer.php":
			w.Write([]byte(`<iframe src="stream/mal/1/1/sub"></iframe>`))
		case "/stream/mal/1/1/sub":
			if r.Header.Get("Referer") != baseURL+"/newplayer.php?ep=1" {
				fail(w, "megaplay page needs player referer")
				return
			}
			w.Write([]byte(`<div id="megaplay-player" data-id="abc123"></div>`))
		case "/stream/getSourcesNew":
			if r.URL.Query().Get("id") != "abc123" {
				t.Errorf("bad id %q", r.URL.RawQuery)
			}
			if r.Header.Get("Referer") != baseURL+"/stream/mal/1/1/sub" {
				fail(w, "sources need stream referer")
				return
			}
			if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				fail(w, "sources need XHR header")
				return
			}
			w.Write([]byte(`{"enc":"` + encryptForTest(`{"file":"https://cdn.example/x/master.m3u8"}`) + `"}`))
		default:
			fail(w, "unexpected path "+r.URL.Path)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	baseURL = srv.URL

	a := New(srv.URL + "/")
	animes, err := a.Search(context.Background(), "alpha")
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

func encryptForTest(plain string) string {
	n := aes.BlockSize - len(plain)%aes.BlockSize
	data := append([]byte(plain), bytes.Repeat([]byte{byte(n)}, n)...)
	block, err := aes.NewCipher(padKey([]byte(sourcesKey), 32))
	if err != nil {
		panic(err)
	}
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, padKey([]byte(sourcesIV), 16)).CryptBlocks(out, data)
	return base64.RawURLEncoding.EncodeToString(out)
}

func TestLive(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1 to hit gogoanime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adapter := New("")
	animes, err := adapter.Search(ctx, "psyren")
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
