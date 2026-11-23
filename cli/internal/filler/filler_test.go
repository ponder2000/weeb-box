package filler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const searchHTML = `<div class="item-list"><ul>
	<li><a href="/shows/naruto-shippuden" title="Naruto Shippuden"><img src="a.jpg" alt="Naruto Shippuden"></a></li>
	<li><a href="/shows/naruto" title="Naruto"><img src="n.jpg" alt="Naruto"></a></li>
</ul></div>`

const showHTML = `<table class="views-table"><tbody>
	<tr class="filler odd"><td class="Number">1</td><td class="Title">Enter</td></tr>
	<tr class="mixed_canon/filler even"><td class="Number">2</td><td class="Title">Mix</td></tr>
	<tr class="manga_canon odd"><td class="Number">3</td><td class="Title">Manga</td></tr>
	<tr class="anime_canon even"><td class="Number">4</td><td class="Title">Anime</td></tr>
</tbody></table>`

func serve(t *testing.T, search, show string) (*Client, *[]string) {
	t.Helper()
	var hits []string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		switch {
		case r.URL.Path == "/search/node":
			w.Write([]byte(search))
		case strings.HasPrefix(r.URL.Path, "/shows/"):
			w.Write([]byte(show))
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.URL), &hits
}

func TestLookupPrefersExactTitle(t *testing.T) {
	c, hits := serve(t, searchHTML, showHTML)
	list, err := c.Lookup(context.Background(), "naruto")
	if err != nil {
		t.Fatal(err)
	}
	if list.Title != "Naruto" {
		t.Fatalf("title: %q", list.Title)
	}
	if !strings.HasSuffix(list.URL, "/shows/naruto") {
		t.Fatalf("url: %q", list.URL)
	}
	if len(*hits) != 2 || (*hits)[1] != "/shows/naruto" {
		t.Fatalf("hits: %v", *hits)
	}
}

func TestParseShowClassification(t *testing.T) {
	list, err := parseShow(strings.NewReader(showHTML))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{1, Filler}, {2, Mixed}, {3, Canon}, {4, Canon}}
	if len(list) != len(want) {
		t.Fatalf("got %+v", list)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("row %d: got %+v want %+v", i, list[i], want[i])
		}
	}
	l := List{Entries: list}
	if kind, ok := l.KindFor(2); !ok || kind != Mixed {
		t.Fatalf("KindFor(2): %q %v", kind, ok)
	}
	if _, ok := l.KindFor(99); ok {
		t.Fatal("KindFor(99) should be missing")
	}
	if l.Max() != 4 {
		t.Fatalf("Max: %d", l.Max())
	}
}

func TestParseShowSkipsDuplicatesAndJunk(t *testing.T) {
	html := `<table><tbody>
		<tr class="filler odd"><td class="Number">1</td></tr>
		<tr class="filler even"><td class="Number">1</td></tr>
		<tr class="filler odd"><td class="Number">x</td></tr>
		<tr class="filler even"><td>no number cell</td></tr>
		<tr class="opening odd"><td class="Number">9</td></tr>
		<tr class="anime_canon even"><td class="Number">5</td></tr>
	</tbody></table>`
	list, err := parseShow(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Number != 1 || list[1].Number != 5 {
		t.Fatalf("got %+v", list)
	}
	if kind, _ := (List{Entries: list}).KindFor(1); kind != Filler {
		t.Fatalf("first duplicate should win: %q", kind)
	}
}

func TestLookupNotFound(t *testing.T) {
	search := `<div class="item-list"><ul>
		<li><a href="/shows/naruto-shippuden" title="Naruto Shippuden">Naruto Shippuden</a></li>
	</ul></div>`
	c, _ := serve(t, search, showHTML)
	_, err := c.Lookup(context.Background(), "Naruto")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestLookupNonOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cloudflare", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if _, err := New(srv.URL).Lookup(context.Background(), "naruto"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestNewBaseURL(t *testing.T) {
	if got := New("").BaseURL; got != DefaultBaseURL {
		t.Fatalf("empty: %q", got)
	}
	if got := New("  https://mirror.example/  ").BaseURL; got != "https://mirror.example" {
		t.Fatalf("trimmed: %q", got)
	}
	if New("").Client.Timeout == 0 {
		t.Fatal("client needs a timeout")
	}
}

func TestNormalizeStripsAudioSuffix(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Bleach (Dub)", "bleach"},
		{"One Piece (Sub)", "onepiece"},
		{"Naruto: Shippuden", "narutoshippuden"},
		{"Bleach English Dubbed", "bleachenglish"},
		{"", ""},
	} {
		if got := normalize(tc.in); got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
