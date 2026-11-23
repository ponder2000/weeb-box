package animekai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLive(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adapter := New("")
	animes, err := adapter.Search(ctx, "psyren")
	if err != nil || len(animes) == 0 {
		t.Fatalf("search: %v (%d)", err, len(animes))
	}
	episodes, err := adapter.Episodes(ctx, animes[0])
	if err != nil || len(episodes) == 0 {
		t.Fatalf("episodes: %v (%d)", err, len(episodes))
	}
	stream, err := adapter.Stream(ctx, episodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stream.URL, ".m3u8") {
		t.Fatalf("stream = %s", stream.URL)
	}
	if len(stream.Subtitles) == 0 {
		t.Fatal("expected captions")
	}
}
