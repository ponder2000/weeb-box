package metadata

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveLookup(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1 to hit AniList and Jikan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := New()

	info, err := client.Lookup(ctx, "Psyren")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("lookup: %+v", info)
	if info.Title == "" || info.CoverURL == "" {
		t.Fatalf("incomplete info = %+v", info)
	}

	items, err := client.Browse(ctx, "Adventure", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("browse: %d items, first = %s", len(items), items[0].Title)
	if len(items) == 0 {
		t.Fatal("browse returned nothing")
	}
}
