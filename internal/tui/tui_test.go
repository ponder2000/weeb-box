package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/ponder2000/weeb-box/internal/cover"
	"github.com/ponder2000/weeb-box/internal/deps"
	lib "github.com/ponder2000/weeb-box/internal/library"
	"github.com/ponder2000/weeb-box/internal/metadata"
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
	m.results = []searchResult{{provider: stubProvider{}, anime: provider.Anime{Title: "A"}}, {provider: stubProvider{}, anime: provider.Anime{Title: "B"}}}
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
	if len(items) != len(m.results) {
		t.Fatalf("got %d items, want %d", len(items), len(m.results))
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

func (stubProvider) Name() string    { return "gogoanime" }
func (stubProvider) SiteURL() string { return "https://gogoanime.example" }

func (stubProvider) Search(context.Context, string) ([]provider.Anime, error) { return nil, nil }

func (stubProvider) Episodes(context.Context, provider.Anime) ([]provider.Episode, error) {
	return nil, nil
}

func (stubProvider) Stream(context.Context, provider.Episode) (provider.Stream, error) {
	return provider.Stream{}, nil
}

type stubProvider struct{}

// fakeMeta stubs AniList without network access.
type fakeMeta struct {
	info    metadata.Info
	items   []metadata.Info
	err     error
	lookups int
	browses int
	genre   string
	page    int
}

func (f *fakeMeta) Lookup(context.Context, string) (metadata.Info, error) {
	f.lookups++
	return f.info, f.err
}

func (f *fakeMeta) Browse(_ context.Context, genre string, page int) ([]metadata.Info, error) {
	f.browses++
	f.genre, f.page = genre, page
	return f.items, f.err
}

// fakeCover stubs terminal artwork.
type fakeCover struct {
	art   string
	calls int
}

func (f *fakeCover) Render(context.Context, string, int, int) (string, error) {
	f.calls++
	return f.art, nil
}

// fakeNativeCover also advertises a native image protocol.
type fakeNativeCover struct {
	fakeCover
	kind        cover.NativeKind
	nativeCalls int
}

func (f *fakeNativeCover) Native() cover.NativeKind { return f.kind }

func (f *fakeNativeCover) RenderNative(context.Context, string, int, int) (string, error) {
	f.nativeCalls++
	return "\x1b]1337;File=inline=1:AAAA\x07", nil
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func fakeArt() string {
	rows := make([]string, coverRows)
	for i := range rows {
		rows[i] = strings.Repeat("ART", coverCols/3)
	}
	return strings.Join(rows, "\n")
}

// runCmd feeds metadata/browse/search messages into the model, expanding
// batches. Timers and spinner ticks are dropped so tests cannot hang.
func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 16 && cmd != nil; i++ {
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				m = runCmd(t, m, c)
			}
			return m
		case metaMsg, coverMsg, browseMsg, searchMsg, episodesMsg, coverViewMsg:
			updated, next := m.Update(msg)
			m, cmd = updated.(Model), next
		default:
			return m
		}
	}
	return m
}

func resultsModel(t *testing.T, fm *fakeMeta, fc coverRenderer) Model {
	t.Helper()
	m := New(nil, t.TempDir(), nil)
	m.meta, m.covers = fm, fc
	m.width, m.height = 120, 40
	m.results = []searchResult{{provider: stubProvider{}, anime: provider.Anime{Title: "Frieren"}}}
	m.main.SetItems(m.animeItems())
	m.main.Select(0)
	m.screen = screenResults
	m.layout()
	return m
}

type fixedProvider struct {
	name   string
	animes []provider.Anime
	err    error
}

func (f fixedProvider) Name() string    { return f.name }
func (f fixedProvider) SiteURL() string { return "https://" + f.name + ".example" }
func (f fixedProvider) Search(context.Context, string) ([]provider.Anime, error) {
	return f.animes, f.err
}
func (f fixedProvider) Episodes(context.Context, provider.Anime) ([]provider.Episode, error) {
	return nil, nil
}
func (f fixedProvider) Stream(context.Context, provider.Episode) (provider.Stream, error) {
	return provider.Stream{}, nil
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if got := expandPath("~/Videos/anime"); got != filepath.Join(home, "Videos/anime") {
		t.Fatalf("expandPath = %q", got)
	}
	if got := expandPath("/tmp/plain"); got != "/tmp/plain" {
		t.Fatalf("plain = %q", got)
	}
}

func TestScrollText(t *testing.T) {
	if got := scrollText("short", 5, 10); got != "short" {
		t.Fatalf("short = %q", got)
	}
	got := scrollText("abcdefghij", 1, 5)
	if got != "bcdef" {
		t.Fatalf("scrolled = %q", got)
	}
	if runewidth.StringWidth(got) > 5 {
		t.Fatalf("width = %d", runewidth.StringWidth(got))
	}
}

func TestThemeCyclePersistAndApply(t *testing.T) {
	t.Cleanup(func() { applyTheme("carbon-dark") })
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New([]provider.Provider{stubProvider{}}, t.TempDir(), st)
	if m.theme != "carbon-dark" {
		t.Fatalf("default theme = %q", m.theme)
	}
	m.cycleTheme(1)
	if m.theme != "carbon-light" {
		t.Fatalf("cycled theme = %q", m.theme)
	}
	got, err := st.GetSetting(context.Background(), "theme", "")
	if err != nil || got != "carbon-light" {
		t.Fatalf("saved theme = %q err=%v", got, err)
	}
	updated, _ := m.Update(settingsMsg{theme: "tokyo-night"})
	m = updated.(Model)
	if m.theme != "tokyo-night" {
		t.Fatalf("settingsMsg theme = %q", m.theme)
	}
	m.screen = screenSettings
	m.settingsRow = settingsTheme
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	if got := updated.(Model); got.theme != "mono" {
		t.Fatalf("space theme = %q", got.theme)
	}
}

func TestProviderToggleFiltersSearch(t *testing.T) {
	m := New([]provider.Provider{
		fixedProvider{name: "alpha"},
		fixedProvider{name: "beta"},
	}, t.TempDir(), nil)
	if got := m.activeProviders(); len(got) != 2 {
		t.Fatalf("active = %d", len(got))
	}
	m.providerState["alpha"] = store.ProviderState{Name: "alpha", Enabled: false}
	got := m.activeProviders()
	if len(got) != 1 || got[0].Name() != "beta" {
		t.Fatalf("active = %v", got)
	}
}

func TestSearchAggregatesProviders(t *testing.T) {
	providers := []provider.Provider{
		fixedProvider{name: "alpha", animes: []provider.Anime{{Title: "A"}}},
		fixedProvider{name: "beta", animes: []provider.Anime{{Title: "B"}}},
		fixedProvider{name: "broken", err: errors.New("boom")},
	}
	msg := searchCmd(nil, providers, "x")().(searchMsg)
	if len(msg.results) != 2 || msg.failed != 1 {
		t.Fatalf("results=%d failed=%d err=%v", len(msg.results), msg.failed, msg.err)
	}
	if msg.results[0].provider.Name() != "alpha" || msg.results[1].provider.Name() != "beta" {
		t.Fatalf("provider order = %s, %s", msg.results[0].provider.Name(), msg.results[1].provider.Name())
	}
}

func TestStatusClearedWhenWorkFinishes(t *testing.T) {
	m := New([]provider.Provider{stubProvider{}}, t.TempDir(), nil)
	m.screen, m.busy, m.status = screenBusy, busySearch, "Searching…"
	updated, _ := m.Update(searchMsg{results: []searchResult{{provider: stubProvider{}, anime: provider.Anime{Title: "A"}}}, query: "a"})
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
		m := New([]provider.Provider{stubProvider{}}, t.TempDir(), nil)
		m.width, m.height = size.w, size.h
		m.recent.SetItems([]list.Item{item{title: "psyren", desc: "2m ago"}})
		m.results = []searchResult{{provider: stubProvider{}, anime: provider.Anime{Title: strings.Repeat("long ", 20), Meta: "TV"}}}
		m.episodes = []provider.Episode{{Number: 1, Title: strings.Repeat("long ", 20)}}
		m.anime = m.results[0].anime
		info := metadata.Info{
			Title: "Frieren", Rating: "R", Genres: []string{"Adventure", "Drama", "Fantasy"},
			Tags: []string{"Magic", "Journey", "Elves"}, CoverURL: "https://img/x.jpg",
		}
		key := metadata.Key(m.results[0].anime.Title)
		m.metaCache[key] = info
		m.coverArt[metadata.Key(info.Title)] = fakeArt()

		views := map[string]func(){
			"home":    func() { m.screen = screenHome },
			"results": func() { m.screen = screenResults; m.main.SetItems(m.animeItems()) },
			"genres": func() {
				m.screen = screenGenres
				items := make([]list.Item, 5)
				for i, genre := range metadata.Genres[:5] {
					items[i] = item{title: genre, desc: "popular · non-adult"}
				}
				m.main.SetItems(items)
			},
			"browse": func() {
				m.screen = screenBrowse
				m.browseLoad = false
				m.browseGenre = "Drama"
				m.main.SetItems([]list.Item{item{title: "Frieren", desc: browseDesc(info), info: info}})
			},
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
			"providers": func() {
				m.screen = screenProviders
				m.main.SetItems(m.providerItems())
				m.providerLoading = false
			},
			"settings": func() {
				m.screen = screenSettings
				m.dirEditing = false
				m.settingsRow = settingsDir
			},
			"setup": func() {
				m.screen = screenSetup
				m.deps = deps.Report{Commands: []string{
					"brew install ffmpeg mpv",
					`/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/tomasklaen/uosc/HEAD/installers/unix.sh)"`,
					`mkdir -p "/Users/example/.config/mpv/script-opts"`,
					`cp "/Users/example/weeb-box/themes/mpv/uosc.conf" "/Users/example/.config/mpv/script-opts/uosc.conf"`,
				}}
			},
			"cover": func() {
				m.screen = screenCover
				m.coverInfo = info
				m.coverViewArt = fakeArt()
				m.coverViewLoad = false
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

func TestQualitySettingLoadCyclePersist(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	if m.quality != "auto" {
		t.Fatalf("default quality = %q", m.quality)
	}
	m.cycleQuality(1)
	m.cycleQuality(1)
	if m.quality != "720p" {
		t.Fatalf("cycled quality = %q", m.quality)
	}
	m.cycleQuality(-1)
	if m.quality != "1080p" {
		t.Fatalf("cycled back = %q", m.quality)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	msg := loadSettingsCmd(st)().(settingsMsg)
	if msg.quality != "auto" {
		t.Fatalf("loaded default = %q", msg.quality)
	}

	persisted := New(nil, t.TempDir(), st)
	persisted.quality = "720p"
	persisted.cycleQuality(1)
	if persisted.quality != "480p" {
		t.Fatalf("persisted quality = %q", persisted.quality)
	}
	if got, _ := st.GetSetting(context.Background(), "quality", ""); got != "480p" {
		t.Fatalf("stored quality = %q", got)
	}
	if got := loadSettingsCmd(st)().(settingsMsg).quality; got != "480p" {
		t.Fatalf("reloaded quality = %q", got)
	}

	updated, _ := New(nil, t.TempDir(), nil).Update(settingsMsg{quality: "1080p"})
	if got := updated.(Model).quality; got != "1080p" {
		t.Fatalf("settings msg quality = %q", got)
	}
}

func TestSettingsDirSaveUpdatesLibraryAndStore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	old := t.TempDir()
	m := New(nil, old, st)
	m.width, m.height = 80, 24
	m.screen = screenSettings
	m.settingsRow = settingsDir
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if !got.dirEditing {
		t.Fatal("enter did not start editing the directory")
	}

	next := filepath.Join(t.TempDir(), "elsewhere")
	got.dirInput.SetValue(next)
	updated, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(Model)
	if got.dirEditing || got.outDir != next || got.libraryDir != next {
		t.Fatalf("editing=%v outDir=%q libraryDir=%q", got.dirEditing, got.outDir, got.libraryDir)
	}
	if saved, _ := st.GetSetting(context.Background(), "download_dir", ""); saved != next {
		t.Fatalf("stored download_dir = %q", saved)
	}
}

func TestSettingsShortcut(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 80, 24
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	if got := updated.(Model); got.screen != screenSettings {
		t.Fatalf("screen = %v", got.screen)
	}
}

func TestFailedDownloadMarksQueueItem(t *testing.T) {
	m := New([]provider.Provider{stubProvider{}}, t.TempDir(), nil)
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

func TestCreatorHeaderContents(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 120, 40
	view := m.View()
	if !strings.Contains(view, "(developed by ") || !strings.Contains(view, "@chotathanos") {
		t.Fatal("view is missing the developer credit")
	}
	if !strings.Contains(view, "\x1b]8;;"+creatorURL+"\x1b\\") {
		t.Fatalf("view is missing the OSC 8 link to %s", creatorURL)
	}
	if !strings.Contains(view, "\x1b]8;;\x1b\\") {
		t.Fatal("hyperlink is not closed")
	}
	header := strings.Split(view, "\n")[0]
	if !strings.Contains(header, "@chotathanos") {
		t.Fatalf("credit is not in the header: %q", header)
	}
}

func TestCreatorHeaderFit(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		m := New(nil, t.TempDir(), nil)
		m.width, m.height = size.w, size.h
		m.layout()
		view := m.View()
		if got := lipgloss.Height(view); got != size.h {
			t.Errorf("%dx%d: height = %d", size.w, size.h, got)
		}
		header := strings.Split(view, "\n")[0]
		if !strings.Contains(header, "(developed by ") || !strings.Contains(header, "@chotathanos") {
			t.Errorf("%dx%d: credit missing from header", size.w, size.h)
		}
		for i, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > size.w {
				t.Errorf("%dx%d: line %d width %d", size.w, size.h, i+1, got)
			}
		}
	}
}

func TestCreatorHitTarget(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 120, 40
	x := m.leftMargin() + lipgloss.Width("weeb-box  ")
	if !m.creatorHit(x, 0) {
		t.Fatal("header credit is not clickable")
	}
	if m.creatorHit(x, 1) {
		t.Fatal("rule row should not open the credit link")
	}

	m.width, m.height = 80, 24
	if !m.creatorHit(m.leftMargin()+lipgloss.Width("weeb-box  "), 0) {
		t.Fatal("header credit should be visible at 80x24")
	}
}

func TestResultsKeyboardNavigation(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 120, 40
	m.screen = screenResults
	items := make([]list.Item, 5)
	for i := range items {
		items[i] = item{title: string(rune('A' + i))}
	}
	m.main.SetItems(items)
	m.main.Select(0)
	m.layout()

	for _, tc := range []struct {
		msg  tea.KeyMsg
		want int
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 1},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 2},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, 1},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}, 4},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}, 0},
		{tea.KeyMsg{Type: tea.KeyEnd}, 4},
		{tea.KeyMsg{Type: tea.KeyHome}, 0},
	} {
		updated, _ := m.handleKey(tc.msg)
		m = updated.(Model)
		if got := m.main.Index(); got != tc.want {
			t.Fatalf("%s: index = %d, want %d", tc.msg.String(), got, tc.want)
		}
		if m.screen != screenResults {
			t.Fatalf("%s: left the results screen", tc.msg.String())
		}
	}

	if updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft}); updated.(Model).screen != screenHome {
		t.Fatal("left did not go back")
	}
}

func TestBackspaceGoesBack(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 80, 24
	m.screen = screenLibrary
	if updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace}); updated.(Model).screen != screenHome {
		t.Fatal("backspace did not go back from library")
	}
}

func TestSettingsLeftRightCyclesQuality(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 80, 24
	m.screen = screenSettings
	m.settingsRow = settingsQuality

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	got := updated.(Model)
	if got.quality != "480p" {
		t.Fatalf("left from auto = %q", got.quality)
	}
	if got.screen != screenSettings {
		t.Fatal("left left the settings screen")
	}

	updated, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	got = updated.(Model)
	if got.quality != "auto" || got.screen != screenSettings {
		t.Fatalf("right = %q on %v", got.quality, got.screen)
	}

	updated, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if got = updated.(Model); got.settingsRow != settingsRowCount-1 {
		t.Fatalf("G selected row %d", got.settingsRow)
	}
	updated, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if got = updated.(Model); got.settingsRow != 0 {
		t.Fatalf("g selected row %d", got.settingsRow)
	}
}

func TestBreadcrumbClickGoesHome(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.width, m.height = 120, 40
	m.screen = screenResults
	m.main.SetItems([]list.Item{item{title: "A"}})
	m.layout()

	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.leftMargin() + 1, Y: m.bodyOffset(),
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if got := updated.(Model); got.screen != screenHome {
		t.Fatalf("breadcrumb click left screen at %v", got.screen)
	}
}

func TestResultsPreviewWideAndNarrow(t *testing.T) {
	info := metadata.Info{
		Title: "Frieren", Rating: "PG-13", Genres: []string{"Adventure", "Drama"},
		Tags: []string{"Magic", "Journey"}, CoverURL: "https://img/x.jpg",
	}
	fm := &fakeMeta{info: info}
	fc := &fakeCover{art: fakeArt()}
	m := resultsModel(t, fm, fc)

	cmd := m.ensurePreview()
	if cmd == nil {
		t.Fatal("selection did not start a lookup")
	}
	if again := m.ensurePreview(); again != nil {
		t.Error("duplicate in-flight lookup was started")
	}
	if !strings.Contains(m.View(), "loading metadata") {
		t.Error("wide results view has no loading placeholder")
	}

	m = runCmd(t, m, cmd)
	if fm.lookups != 1 || fc.calls != 1 {
		t.Fatalf("lookups=%d coverCalls=%d", fm.lookups, fc.calls)
	}
	wide := m.View()
	if !strings.Contains(wide, "Preview") || !strings.Contains(wide, "PG-13") {
		t.Error("wide results view is missing the preview panel")
	}
	if !strings.Contains(wide, strings.Repeat("ART", coverCols/3)) {
		t.Error("wide results view is missing the cover art")
	}

	m.width, m.height = 80, 24
	m.layout()
	narrow := m.View()
	if strings.Contains(narrow, strings.Repeat("ART", coverCols/3)) {
		t.Error("narrow results view should not render the cover")
	}
	if strings.Contains(narrow, "Preview") {
		t.Error("narrow results view should not render the preview panel")
	}
	if !strings.Contains(narrow, "PG-13") || !strings.Contains(narrow, "Adventure") {
		t.Error("narrow results view is missing the compact metadata line")
	}
}

func TestMetadataLookupErrorIsNonFatal(t *testing.T) {
	fm := &fakeMeta{err: errors.New("anilist down")}
	m := resultsModel(t, fm, &fakeCover{})

	m = runCmd(t, m, m.ensurePreview())
	if m.err != nil {
		t.Fatalf("preview failure surfaced as an error: %v", m.err)
	}
	if m.screen != screenResults {
		t.Fatalf("screen = %v", m.screen)
	}
	if !m.metaFailed[metadata.Key("Frieren")] {
		t.Error("failed lookup was not recorded")
	}
	if cmd := m.ensurePreview(); cmd != nil {
		t.Error("failed lookup is retried on every selection")
	}
}

func TestGenreNavigation(t *testing.T) {
	fm := &fakeMeta{items: []metadata.Info{{
		Title: "Frieren", Rating: "PG-13", Genres: []string{"Adventure"}, CoverURL: "https://img/x.jpg",
	}}}
	fc := &fakeCover{art: fakeArt()}
	m := New(nil, t.TempDir(), nil)
	m.meta, m.covers = fm, fc
	m.width, m.height = 120, 40

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = updated.(Model)
	if m.screen != screenGenres || len(m.main.Items()) != len(metadata.Genres) {
		t.Fatalf("screen=%v items=%d", m.screen, len(m.main.Items()))
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, updated.(Model), cmd)
	if fm.browses != 1 || fm.genre != metadata.Genres[0] || fm.page != 1 {
		t.Fatalf("browse calls=%d genre=%q page=%d", fm.browses, fm.genre, fm.page)
	}
	if m.screen != screenBrowse || len(m.main.Items()) != 1 {
		t.Fatalf("screen=%v items=%d", m.screen, len(m.main.Items()))
	}
	if !strings.Contains(m.View(), "PG-13") {
		t.Error("browse view is missing the rating")
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.screen != screenGenres {
		t.Fatalf("esc from browse = %v", m.screen)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := updated.(Model); got.screen != screenHome {
		t.Fatalf("esc from genres = %v", got.screen)
	}
}

func TestBrowseTitleRunsSearch(t *testing.T) {
	providers := []provider.Provider{fixedProvider{name: "alpha", animes: []provider.Anime{{Title: "Frieren"}}}}
	fm := &fakeMeta{items: []metadata.Info{{Title: "Frieren", Rating: "R"}}}
	m := New(providers, t.TempDir(), nil)
	m.meta, m.covers = fm, &fakeCover{}
	m.width, m.height = 80, 24

	m.screen = screenBrowse
	m.browseGenre = "Drama"
	m.main.SetItems([]list.Item{item{title: "Frieren", info: fm.items[0]}})
	m.main.Select(0)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.screen != screenBusy || m.busy != busySearch {
		t.Fatalf("screen=%v busy=%v", m.screen, m.busy)
	}
	if m.search.Value() != "Frieren" {
		t.Fatalf("search value = %q", m.search.Value())
	}

	m = runCmd(t, m, cmd)
	if m.screen != screenResults || len(m.results) != 1 || m.results[0].anime.Title != "Frieren" {
		t.Fatalf("screen=%v results=%#v", m.screen, m.results)
	}
}

func TestMetadataCacheHitSkipsLookup(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	payload, err := json.Marshal(metadata.Info{Title: "Cached Frieren", Rating: "R", Genres: []string{"Drama"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutAnimeMeta(ctx, metadata.Key("Frieren"), string(payload)); err != nil {
		t.Fatal(err)
	}

	fm := &fakeMeta{info: metadata.Info{Title: "Fresh"}}
	msg := metaCmd(st, fm, "Frieren")().(metaMsg)
	if msg.err != nil || msg.info.Title != "Cached Frieren" {
		t.Fatalf("cached msg = %#v", msg)
	}
	if fm.lookups != 0 {
		t.Fatalf("cache hit still called Lookup %d time(s)", fm.lookups)
	}

	msg = metaCmd(st, fm, "Uncached Frieren")().(metaMsg)
	if msg.err != nil || fm.lookups != 1 || msg.info.Title != "Fresh" {
		t.Fatalf("uncached msg = %#v lookups=%d", msg, fm.lookups)
	}
	if _, fetchedAt, ok, err := st.GetAnimeMeta(ctx, metadata.Key("Uncached Frieren")); err != nil || !ok || time.Since(fetchedAt) > time.Minute {
		t.Fatalf("lookup was not cached: ok=%v err=%v", ok, err)
	}
}

func TestBrowseErrorIsFatal(t *testing.T) {
	fm := &fakeMeta{err: errors.New("anilist down")}
	m := New(nil, t.TempDir(), nil)
	m.meta = fm
	m.width, m.height = 80, 24

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	updated, cmd := updated.(Model).handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, updated.(Model), cmd)
	if m.err == nil {
		t.Fatal("browse failure was not surfaced")
	}
	if m.screen != screenGenres {
		t.Fatalf("screen = %v", m.screen)
	}
}

func TestBrowsePagination(t *testing.T) {
	items := []metadata.Info{{Title: "Frieren", Rating: "PG-13"}}
	fm := &fakeMeta{items: items}
	m := New(nil, t.TempDir(), nil)
	m.meta = fm
	m.width, m.height = 80, 24
	m.screen, m.browseGenre, m.browsePage = screenBrowse, "Drama", 1
	m.main.SetItems([]list.Item{item{title: "Frieren", info: items[0]}})
	m.main.Select(0)

	updated, cmd := m.handleKey(runeKey('n'))
	m = updated.(Model)
	if m.browsePage != 2 || !m.browseLoad {
		t.Fatalf("page=%d loading=%v", m.browsePage, m.browseLoad)
	}
	m = runCmd(t, m, cmd)
	if fm.page != 2 || fm.genre != "Drama" {
		t.Fatalf("browse page=%d genre=%q", fm.page, fm.genre)
	}
	if !strings.Contains(m.View(), "Popular · page 2") {
		t.Error("browse view is missing the page in its title")
	}

	updated, cmd = m.handleKey(runeKey('['))
	m = runCmd(t, updated.(Model), cmd)
	if fm.page != 1 {
		t.Fatalf("previous page = %d", fm.page)
	}

	// An empty page keeps the browse screen and reports it.
	fm.items = nil
	updated, cmd = m.handleKey(runeKey(']'))
	m = runCmd(t, updated.(Model), cmd)
	if m.screen != screenBrowse || !strings.Contains(m.View(), "no more titles") {
		t.Fatalf("empty page: screen=%v", m.screen)
	}
	if m.browsePage != 2 {
		t.Fatalf("empty page = %d", m.browsePage)
	}
}

func TestHideAdultSetting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "adult.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := New(nil, t.TempDir(), st)
	if m.hideAdult {
		t.Fatal("adult titles should be visible by default")
	}
	if loadSettingsCmd(st)().(settingsMsg).hideAdult {
		t.Fatal("hide_adult defaulted to on")
	}

	m.screen, m.settingsRow = screenSettings, settingsHideAdult
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	got := updated.(Model)
	if !got.hideAdult {
		t.Fatal("space did not enable the filter")
	}
	if saved, _ := st.GetSetting(context.Background(), "hide_adult", ""); saved != "yes" {
		t.Fatalf("stored hide_adult = %q", saved)
	}
	if !loadSettingsCmd(st)().(settingsMsg).hideAdult {
		t.Fatal("hide_adult was not reloaded")
	}

	updated, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.(Model).hideAdult {
		t.Fatal("enter did not disable the filter")
	}
	if saved, _ := st.GetSetting(context.Background(), "hide_adult", ""); saved != "no" {
		t.Fatalf("stored hide_adult = %q", saved)
	}

	updated, _ = m.Update(settingsMsg{hideAdult: true})
	if !updated.(Model).hideAdult {
		t.Fatal("settingsMsg did not apply hide_adult")
	}
}

func TestAdultFilterBlocksEnter(t *testing.T) {
	info := metadata.Info{Title: "Frieren", Adult: true, CoverURL: "https://img/x.jpg"}
	m := resultsModel(t, &fakeMeta{info: info}, &fakeCover{})
	m.metaCache[metadata.Key("Frieren")] = info

	if !strings.Contains(m.View(), "18+") {
		t.Fatal("adult badge is hidden by default")
	}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := updated.(Model); got.screen != screenBusy || got.busy != busyEpisodes {
		t.Fatalf("default enter: screen=%v busy=%v", got.screen, got.busy)
	}

	m = resultsModel(t, &fakeMeta{info: info}, &fakeCover{})
	m.metaCache[metadata.Key("Frieren")] = info
	m.hideAdult = true
	if !strings.Contains(m.View(), "18+ hidden") {
		t.Fatal("preview does not hide the adult title")
	}
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.screen != screenResults || cmd != nil {
		t.Fatalf("blocked enter: screen=%v cmd=%v", got.screen, cmd != nil)
	}
	if !strings.Contains(got.status, "adult filter") {
		t.Fatalf("status = %q", got.status)
	}

	updated, cmd = got.handleKey(runeKey('i'))
	got = updated.(Model)
	if got.screen != screenResults || cmd != nil || !strings.Contains(got.status, "adult filter") {
		t.Fatalf("adult cover: screen=%v status=%q", got.screen, got.status)
	}
}

func TestCoverViewerHalfBlock(t *testing.T) {
	info := metadata.Info{
		Title: "Frieren", Rating: "PG-13", Genres: []string{"Adventure", "Drama"},
		Tags: []string{"Magic", "Journey"}, CoverURL: "https://img/x.jpg",
	}
	fc := &fakeCover{art: fakeArt()}
	m := resultsModel(t, &fakeMeta{info: info}, fc)
	m.metaCache[metadata.Key("Frieren")] = info

	updated, cmd := m.handleKey(runeKey('i'))
	m = updated.(Model)
	if m.screen != screenCover || !m.coverViewLoad || m.coverReturn != screenResults {
		t.Fatalf("screen=%v loading=%v return=%v", m.screen, m.coverViewLoad, m.coverReturn)
	}
	m = runCmd(t, m, cmd)
	if m.coverViewArt == "" || m.coverViewLoad {
		t.Fatal("cover art was not loaded")
	}
	if !strings.Contains(m.View(), "PG-13") || !strings.Contains(m.View(), "Adventure") {
		t.Error("cover view is missing its metadata")
	}

	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		m.width, m.height = size.w, size.h
		m.layout()
		view := m.View()
		if got := lipgloss.Height(view); got > size.h {
			t.Errorf("%dx%d: height %d", size.w, size.h, got)
		}
		for i, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > size.w {
				t.Errorf("%dx%d: line %d width %d", size.w, size.h, i+1, got)
			}
		}
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := updated.(Model); got.screen != screenResults {
		t.Fatalf("esc left the viewer on %v", got.screen)
	}
}

func TestNativeCoverDecision(t *testing.T) {
	if _, ok := nativeCover(&fakeCover{}); ok {
		t.Fatal("half-block renderer chose the native viewer")
	}
	if _, ok := nativeCover(&fakeNativeCover{kind: cover.NativeNone}); ok {
		t.Fatal("NativeNone chose the native viewer")
	}
	native := &fakeNativeCover{kind: cover.NativeITerm2}
	if runtime.GOOS == "windows" {
		if _, ok := nativeCover(native); ok {
			t.Fatal("windows chose the native viewer")
		}
		return
	}
	if _, ok := nativeCover(native); !ok {
		t.Fatal("native renderer fell back to half blocks")
	}

	info := metadata.Info{Title: "Frieren", CoverURL: "https://img/x.jpg"}
	m := resultsModel(t, &fakeMeta{info: info}, native)
	m.metaCache[metadata.Key("Frieren")] = info
	updated, cmd := m.openCover()
	if got := updated.(Model); got.screen != screenResults || cmd == nil {
		t.Fatalf("native viewer: screen=%v cmd=%v", got.screen, cmd != nil)
	}

	msg := cmd().(nativeCoverMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	defer os.Remove(msg.path)
	staged, err := os.ReadFile(msg.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(staged), "\x1b]1337;") {
		t.Fatalf("staged escape = %q", staged)
	}
}
