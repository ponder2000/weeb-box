package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/store"
)

func TestWatchTimeFormat(t *testing.T) {
	for _, tc := range []struct {
		seconds float64
		want    string
	}{
		{0, "0m"},
		{45, "0m"},
		{90 * 60, "1h 30m"},
		{60 * 60, "1h 0m"},
		{30 * 3600, "1d 6h"},
		{50 * 3600, "2d 2h"},
	} {
		if got := watchTime(tc.seconds); got != tc.want {
			t.Errorf("watchTime(%v) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestGenreSharesSplitsTitleTime(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.statsRows = []store.WatchStat{
		{AnimeTitle: "Action Show", Seconds: 3600},
		{AnimeTitle: "Two Tags", Seconds: 7200},
	}
	m.metaCache[metadata.Key("Action Show")] = metadata.Info{Genres: []string{"Action"}}
	m.metaCache[metadata.Key("Two Tags")] = metadata.Info{Genres: []string{"Action", "Comedy"}}

	shares := genreShares(m)
	if len(shares) != 2 {
		t.Fatalf("shares = %+v", shares)
	}
	if shares[0].genre != "Action" || shares[0].seconds != 3600+3600 {
		t.Errorf("top share = %+v", shares[0])
	}
	if shares[1].genre != "Comedy" || shares[1].seconds != 3600 {
		t.Errorf("second share = %+v", shares[1])
	}
}

func TestStatsViewTotalsAndGenres(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 120, 40
	m.statsRows = []store.WatchStat{
		{AnimeTitle: "Action Show", Episodes: 2, Seconds: 3600, LastWatched: time.Now().Unix() - 60},
		{AnimeTitle: "Two Tags", Episodes: 3, Seconds: 7200, LastWatched: time.Now().Unix() - 3600},
	}
	m.metaCache[metadata.Key("Action Show")] = metadata.Info{Genres: []string{"Action"}}
	m.metaCache[metadata.Key("Two Tags")] = metadata.Info{Genres: []string{"Action", "Comedy"}}
	m.stats = store.Stats{Downloads: 4, Bytes: 2048, Watched: 5}

	for _, size := range []struct{ w, h int }{{120, 40}, {80, 24}} {
		m.width, m.height = size.w, size.h
		view := m.statsView()
		for _, want := range []string{"Watch time", "Genres", "3h 0m", "5 episodes", "2 titles", "0.1 days", "Action", "Comedy", "4 downloads"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d view missing %q", size.w, size.h, want)
			}
		}
		if size.h >= 30 && !strings.Contains(view, "Top titles") {
			t.Errorf("%dx%d view missing the top titles panel", size.w, size.h)
		}
	}
}

func TestStatsEmptyState(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 120, 40
	if view := m.statsView(); !strings.Contains(view, "Nothing watched yet") {
		t.Errorf("empty view = %q", view)
	}
}

func TestStatsKeyOpensScreen(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.focus = focusResume

	updated, cmd := m.Update(runeKey('s'))
	m = updated.(Model)
	if m.screen != screenStats || !m.statsLoading {
		t.Fatalf("screen = %v loading = %v", m.screen, m.statsLoading)
	}
	if cmd == nil {
		t.Fatal("no load command")
	}

	// s and esc both go back home.
	m.focus = focusResume
	updated, _ = m.Update(runeKey('s'))
	if got := updated.(Model).screen; got != screenHome {
		t.Fatalf("s from stats = %v", got)
	}

	// Typing s in the search box stays text.
	m.screen, m.focus = screenHome, focusSearch
	updated, _ = m.Update(runeKey('s'))
	if got := updated.(Model).screen; got != screenHome {
		t.Fatalf("s in the search box left home: %v", got)
	}
}

func TestStatsLoadsGenres(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.statsRows = []store.WatchStat{{AnimeTitle: "Uncached", Seconds: 60}}
	if cmd := m.ensureStatsGenres(); cmd == nil {
		t.Fatal("no metadata lookup for an uncached title")
	}
	if !m.metaLoading[metadata.Key("Uncached")] {
		t.Error("lookup not marked in flight")
	}

	m.metaCache[metadata.Key("Uncached")] = metadata.Info{}
	if cmd := m.ensureStatsGenres(); cmd != nil {
		t.Error("cached title looked up again")
	}
}

func TestStatsMessagePopulatesRows(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	rows := []store.WatchStat{{AnimeTitle: "A", Seconds: 120, Episodes: 1}}
	updated, _ := m.Update(watchStatsMsg{rows: rows, stats: store.Stats{Downloads: 1}})
	m = updated.(Model)
	if len(m.statsRows) != 1 || m.stats.Downloads != 1 || m.statsLoading {
		t.Fatalf("rows = %+v stats = %+v loading = %v", m.statsRows, m.stats, m.statsLoading)
	}

	updated, _ = m.Update(watchStatsMsg{err: errors.New("stats failed")})
	if got := updated.(Model); got.err == nil {
		t.Error("stats error not surfaced")
	}
}
