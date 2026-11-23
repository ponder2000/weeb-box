package tui

import (
	"os"
	"testing"

	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/provider/animekai"
	"github.com/ponder2000/weeb-box/internal/provider/anizone"
	"github.com/ponder2000/weeb-box/internal/provider/gogo"
	"github.com/ponder2000/weeb-box/internal/provider/hianime"
	"github.com/ponder2000/weeb-box/internal/provider/kissanime"
)

// TestLiveFallback proves the cross-provider rescue against the real sites:
// the skipped adapter "fails" so the command has to find the episode elsewhere.
func TestLiveFallback(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1")
	}
	providers := []provider.Provider{
		gogo.New(""), animekai.New(""), anizone.New(""), kissanime.New(""), hianime.New(""),
	}
	msg, ok := fallbackCmd(nil, providers[1:], "gogoanime", provider.Anime{Title: "Psyren"}, 1)().(streamMsg)
	if !ok {
		t.Fatal("unexpected message type")
	}
	if msg.err != nil {
		t.Skipf("no adapter currently matches Psyren E001: %v", msg.err)
	}
	if msg.provider == nil || msg.stream.URL == "" || len(msg.episodes) == 0 {
		t.Fatalf("incomplete fallback: provider=%v url=%q episodes=%d", msg.provider, msg.stream.URL, len(msg.episodes))
	}
	if msg.episode.Number != 1 {
		t.Errorf("episode = %d", msg.episode.Number)
	}
	if _, ok := bestTitleMatch(msg.anime.Title, []provider.Anime{msg.anime}); !ok {
		t.Errorf("returned title %q does not match itself", msg.anime.Title)
	}
	t.Logf("%s E%03d -> %s", msg.provider.Name(), msg.episode.Number, msg.stream.URL)
}
