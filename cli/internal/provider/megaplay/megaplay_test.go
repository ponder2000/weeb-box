package megaplay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestResolveFixture(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		if r.Referer() != "https://site.example/watch" {
			t.Errorf("player referer = %q", r.Referer())
		}
		_, _ = w.Write([]byte(`<div id="megaplay-player" data-id="abc"></div>`))
	})
	mux.HandleFunc("/stream/getSourcesNew", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "abc" || r.Referer() != serverURL+"/watch" || r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Errorf("bad sources request: %s referer=%q xhr=%q", r.URL, r.Referer(), r.Header.Get("X-Requested-With"))
		}
		_, _ = w.Write([]byte(`{"enc":"wdeBruh3qqn_i5wUNnyaPWiRLLdfX1H1YKBxoBvLhJuin6WMxjzk9aZMRezhoHTo",` +
			`"tracks":[{"file":"https://cdn.example/en.vtt","label":"English","kind":"captions","default":true}]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	stream, err := Resolve(context.Background(), server.Client(), server.URL+"/watch", "https://site.example/watch")
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != "https://cdn.example/x/master.m3u8" || stream.Referer != server.URL+"/" {
		t.Fatalf("stream = %+v", stream)
	}
	if len(stream.Subtitles) != 1 || stream.Subtitles[0].Label != "English" || !stream.Subtitles[0].Default {
		t.Fatalf("subtitles = %+v", stream.Subtitles)
	}
}
