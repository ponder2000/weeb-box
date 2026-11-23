package filler

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveNaruto(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1 to hit animefillerlist.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	list, err := New("").Lookup(ctx, "naruto")
	if err != nil {
		t.Skipf("site unavailable: %v", err)
	}
	filler := 0
	for _, e := range list.Entries {
		if e.Kind == Filler {
			filler++
		}
	}
	if filler < 20 {
		t.Fatalf("%s: only %d filler entries of %d", list.Title, filler, len(list.Entries))
	}
	if kind, ok := list.KindFor(26); !ok || kind != Filler {
		t.Fatalf("episode 26: %q %v", kind, ok)
	}
}
