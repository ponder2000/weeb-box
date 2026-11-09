package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponder2000/weeb-box/internal/deps"
	lib "github.com/ponder2000/weeb-box/internal/library"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/store"
)

func TestSafeName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"One Piece", "One Piece"},
		{"../etc/passwd", ".._etc_passwd"},
		{"a/../b", "a_.._b"},
		{"..", "unknown"},
		{"", "unknown"},
		{"trailing. ", "trailing"},
	} {
		got := safeName(tc.in)
		if got != tc.want {
			t.Errorf("safeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if filepath.Base(got) != got {
			t.Errorf("safeName(%q) = %q escapes its directory", tc.in, got)
		}
	}
}

func TestShowResultsRestoresAnimeList(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.animes = []provider.Anime{{Title: "A"}, {Title: "B"}}
	m.screen = screenEpisodes
	m.main.SetItems([]list.Item{item{title: "E001"}, item{title: "E002"}})
	m.main.Select(1) // stale cursor from the episode list

	m.showResults()

	if m.screen != screenResults {
		t.Fatalf("screen = %v", m.screen)
	}
	if got := m.main.Index(); got != 0 {
		t.Errorf("selection index = %d, want 0", got)
	}
	items := m.main.Items()
	if len(items) != len(m.animes) {
		t.Fatalf("got %d items, want %d", len(items), len(m.animes))
	}
	if it, ok := items[0].(item); !ok || it.title != "A" || it.anime.Title != "A" {
		t.Errorf("first item = %#v", items[0])
	}
}

func TestProgressHelpers(t *testing.T) {
	half := store.Progress{Position: 300, Duration: 600}
	if got := progressPct(half); got != "50%" {
		t.Errorf("progressPct = %q", got)
	}
	if got := progressRatio(half); got != 0.5 {
		t.Errorf("progressRatio = %v", got)
	}
	if got := progressPct(store.Progress{Position: 300}); got != "05:00 watched" {
		t.Errorf("progressPct without duration = %q", got)
	}
	if got := clock(3725); got != "1:02:05" {
		t.Errorf("clock = %q", got)
	}
	if got := clock(0); got != "00:00" {
		t.Errorf("clock(0) = %q", got)
	}
	if got := humanBytes(2048); got != "2.0 KB" {
		t.Errorf("humanBytes = %q", got)
	}
}

func (stubProvider) Name() string { return "gogoanime" }

func (stubProvider) Search(context.Context, string) ([]provider.Anime, error) { return nil, nil }

func (stubProvider) Episodes(context.Context, provider.Anime) ([]provider.Episode, error) {
	return nil, nil
}

func (stubProvider) Stream(context.Context, provider.Episode) (provider.Stream, error) {
	return provider.Stream{}, nil
}

type stubProvider struct{}

func TestStatusClearedWhenWorkFinishes(t *testing.T) {
	m := New(stubProvider{}, t.TempDir(), nil)
	m.screen, m.busy, m.status = screenBusy, busySearch, "Searching…"
	updated, _ := m.Update(searchMsg{animes: []provider.Anime{{Title: "A"}}, query: "a"})
	if got := updated.(Model); got.screen != screenResults || got.status != "" {
		t.Errorf("search finished: screen=%v status=%q", got.screen, got.status)
	}

	m.screen, m.busy, m.status = screenBusy, busyEpisodes, "Loading episodes…"
	updated, _ = m.Update(episodesMsg{episodes: []provider.Episode{{Number: 1}}})
	if got := updated.(Model); got.screen != screenEpisodes || got.status != "" {
		t.Errorf("episodes finished: screen=%v status=%q", got.screen, got.status)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		m := New(stubProvider{}, t.TempDir(), nil)
		m.width, m.height = size.w, size.h
		m.recent.SetItems([]list.Item{item{title: "psyren", desc: "2m ago"}})
		m.animes = []provider.Anime{{Title: strings.Repeat("long ", 20), Meta: "TV"}}
		m.episodes = []provider.Episode{{Number: 1, Title: strings.Repeat("long ", 20)}}
		m.anime = m.animes[0]

		views := map[string]func(){
			"home":    func() { m.screen = screenHome },
			"results": func() { m.screen = screenResults; m.main.SetItems(m.animeItems()) },
			"episodes": func() {
				m.screen = screenEpisodes
				m.main.SetItems([]list.Item{item{title: "E001", episode: m.episodes[0]}})
			},
			"downloads": func() {
				m.screen = screenDownloads
				m.queue = m.queue[:0]
				for i := 1; i <= 30; i++ {
					m.queue = append(m.queue, &queueItem{episode: provider.Episode{Number: i, Title: "Episode"}, state: qQueued})
				}
			},
			"help": func() { m.screen = screenHelp },
			"setup": func() {
				m.screen = screenSetup
				m.deps = deps.Report{Commands: []string{
					"brew install ffmpeg mpv",
					`/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/tomasklaen/uosc/HEAD/installers/unix.sh)"`,
					`mkdir -p "/Users/example/.config/mpv/script-opts"`,
					`cp "/Users/example/weeb-box/themes/mpv/uosc.conf" "/Users/example/.config/mpv/script-opts/uosc.conf"`,
				}}
			},
		}
		for name, setup := range views {
			setup()
			m.status = strings.Repeat("x", 300)
			m.layout()
			view := m.View()
			if got := lipgloss.Height(view); got > size.h {
				t.Errorf("%s at %dx%d: height %d", name, size.w, size.h, got)
			}
			for i, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > size.w {
					t.Errorf("%s at %dx%d: line %d width %d", name, size.w, size.h, i+1, got)
				}
			}
			lines := strings.Split(view, "\n")
			header := strings.Join(lines[:min(3, len(lines))], "\n")
			if !strings.Contains(header, "weeb-box") {
				t.Errorf("%s at %dx%d: header missing", name, size.w, size.h)
			}
		}
	}
}

func TestListIndexMapsRows(t *testing.T) {
	l := list.New(nil, list.NewDefaultDelegate(), 40, 8)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	l.SetItems([]list.Item{item{title: "a"}, item{title: "b"}, item{title: "c"}})
	l.SetSize(40, 8)
	if got := listIndex(l, 0); got != 0 {
		t.Errorf("row 0 = %d", got)
	}
	if got := listIndex(l, 1); got != 0 {
		t.Errorf("description row = %d", got)
	}
	if got := listIndex(l, 2); got != 1 {
		t.Errorf("row 2 = %d", got)
	}
	if got := listIndex(l, 999); got != -1 {
		t.Errorf("out of range = %d", got)
	}
}

func TestLibraryShortcutAndMissingLocalFile(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 80, 24
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlL})
	got := updated.(Model)
	if got.screen != screenLibrary || !got.libraryLoading {
		t.Fatalf("screen=%v loading=%v", got.screen, got.libraryLoading)
	}

	missing := lib.File{Path: filepath.Join(t.TempDir(), "missing.mp4"), Name: "missing"}
	updated, cmd := m.startLocalPlayback(missing)
	got = updated.(Model)
	if got.err == nil || cmd != nil {
		t.Fatalf("err=%v cmd=%v", got.err, cmd != nil)
	}
}

func TestMouseClickFocusesSearch(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 80, 24
	m.layout()
	tagline := 0
	if m.height >= 28 {
		tagline = 1
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.leftMargin() + 4, Y: m.bodyOffset() + 4 + tagline,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if got := updated.(Model); got.focus != focusSearch {
		t.Fatalf("focus = %v", got.focus)
	}
}

func TestMouseSelectsResumeAndWheelMovesList(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 120, 40
	m.resume.SetItems([]list.Item{item{title: "Frieren"}})
	m.layout()
	tagline := 1
	panelTop := m.bodyOffset() + 8 + tagline
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.leftMargin() + 2, Y: panelTop + 2,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	got := updated.(Model)
	if got.focus != focusResume || got.resume.Index() != 0 {
		t.Fatalf("focus=%v index=%d", got.focus, got.resume.Index())
	}

	got.screen = screenResults
	got.main.SetItems([]list.Item{item{title: "A"}, item{title: "B"}})
	got.main.Select(0)
	got.layout()
	updated, _ = got.handleMouse(tea.MouseMsg{
		X: got.leftMargin() + 2, Y: got.bodyOffset() + 10,
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	if idx := updated.(Model).main.Index(); idx != 1 {
		t.Fatalf("wheel index = %d", idx)
	}
}

func TestFailedDownloadMarksQueueItem(t *testing.T) {
	m := New(stubProvider{}, t.TempDir(), nil)
	m.screen = screenDownloads
	m.queue = []*queueItem{{episode: provider.Episode{Number: 1}, state: qActive}}
	m.downloading = true
	m.dlCh = make(chan dlEvent)
	m.cancelDownload = func() {}

	updated, _ := m.Update(dlEvent{index: 0, state: qFailed, err: errors.New("boom"), done: true})
	got := updated.(Model)
	if got.queue[0].state != qFailed || got.queue[0].err == nil {
		t.Errorf("queue item = %#v", got.queue[0])
	}
	if got.err == nil {
		t.Error("download error was not surfaced")
	}
	if got.status == "Download complete" {
		t.Error("failed download reported as complete")
	}
}
