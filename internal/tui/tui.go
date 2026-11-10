// Package tui is the Bubble Tea interface for browsing, playing, and downloading.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/ponder2000/weeb-box/internal/cover"
	"github.com/ponder2000/weeb-box/internal/deps"
	lib "github.com/ponder2000/weeb-box/internal/library"
	"github.com/ponder2000/weeb-box/internal/media"
	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/store"
)

type screen int

const (
	screenHome screen = iota
	screenResults
	screenGenres
	screenBrowse
	screenEpisodes
	screenDownloads
	screenLibrary
	screenProviders
	screenSetup
	screenBusy
	screenHelp
	screenSettings
	screenCover
)

const (
	settingsQuality = iota
	settingsDir
	settingsDatabase
	settingsProviders
	settingsHideAdult
	settingsTheme
	settingsFont
	settingsRowCount
)

var qualityOptions = []string{"auto", "1080p", "720p", "480p"}

const (
	creatorURL     = "https://x.com/chotathanos"
	creatorText    = "developed by @chotathanos"
	metaTTL        = 7 * 24 * time.Hour
	minPreviewCols = 8
)

// metaProvider is the metadata client surface the TUI needs; tests stub it.
type metaProvider interface {
	Lookup(ctx context.Context, title string) (metadata.Info, error)
	Browse(ctx context.Context, genre string, page int) ([]metadata.Info, error)
}

// coverRenderer turns a cover URL into terminal art; tests stub it.
type coverRenderer interface {
	Render(ctx context.Context, url string, width, height int) (string, error)
}

// nativeCoverRenderer is the optional surface used for full-size inline
// images. Renderers that do not implement it keep the half-block viewer.
type nativeCoverRenderer interface {
	Native() cover.NativeKind
	RenderNative(ctx context.Context, url string, width, height int) (string, error)
}

type focus int

const (
	focusSearch focus = iota
	focusResume
	focusRecent
)

type busyKind int

const (
	busySearch busyKind = iota
	busyEpisodes
	busyStream
)

type item struct {
	title    string
	desc     string
	anime    provider.Anime
	episode  provider.Episode
	progress store.Progress
	file     lib.File
	query    string
	prov     provider.Provider
	info     metadata.Info
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type providerItem struct {
	name      string
	url       string
	enabled   bool
	lastOK    int64
	lastError string
}

func (p providerItem) Title() string {
	mark := "●"
	if !p.enabled {
		mark = "○"
	}
	return mark + " " + p.name
}

func (p providerItem) Description() string {
	state := "enabled"
	if !p.enabled {
		state = "disabled"
	}
	updated := "never"
	if p.lastOK > 0 {
		updated = ago(p.lastOK)
	}
	desc := p.url + " · " + state + " · updated " + updated
	if p.lastError != "" {
		desc += " · error: " + p.lastError
	}
	return desc
}

func (p providerItem) FilterValue() string { return p.name }

type scrollState struct{ offset int }

type scrollTickMsg struct{}

func scrollTickCmd() tea.Cmd {
	return tea.Tick(160*time.Millisecond, func(time.Time) tea.Msg { return scrollTickMsg{} })
}

type marqueeDelegate struct {
	list.DefaultDelegate
	state *scrollState
}

func (d marqueeDelegate) Render(w io.Writer, m list.Model, index int, data list.Item) {
	item, ok := data.(list.DefaultItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, data)
		return
	}
	width := max(8, m.Width()-8)
	d.DefaultDelegate.Render(w, m, index, marqueeItem{DefaultItem: item, state: d.state, width: width})
}

type marqueeItem struct {
	list.DefaultItem
	state *scrollState
	width int
}

func (i marqueeItem) Title() string {
	offset := 0
	if i.state != nil {
		offset = i.state.offset
	}
	return scrollText(i.DefaultItem.Title(), offset, i.width)
}

func (m Model) scrollOffset() int {
	if m.scroll == nil {
		return 0
	}
	return m.scroll.offset
}

func scrollText(text string, offset, width int) string {
	if width < 4 || runewidth.StringWidth(text) <= width {
		return text
	}
	const gap = "   "
	runes := []rune(text + gap)
	if len(runes) == 0 {
		return text
	}
	at := offset % len(runes)
	rotated := append(append([]rune{}, runes[at:]...), runes[:at]...)
	return runewidth.Truncate(string(rotated), width, "")
}

type queueState int

const (
	qQueued queueState = iota
	qActive
	qDone
	qSkipped
	qFailed
)

type queueItem struct {
	episode provider.Episode
	state   queueState
	percent float64
	bytes   int64
	rate    float64
	speed   float64
	eta     time.Duration
	status  string
	err     error
}

type Model struct {
	prov       provider.Provider
	providers  []provider.Provider
	byName     map[string]provider.Provider
	st         *store.Store
	outDir     string
	libraryDir string

	screen     screen
	helpReturn screen
	busy       busyKind
	playReturn screen
	width      int
	height     int

	search textinput.Model
	focus  focus
	resume list.Model
	recent list.Model
	main   list.Model

	spinner spinner.Model

	stats store.Stats

	results  []searchResult
	anime    provider.Anime
	episodes []provider.Episode

	meta         metaProvider
	covers       coverRenderer
	metaCache    map[string]metadata.Info
	metaLoading  map[string]bool
	metaFailed   map[string]bool
	coverArt     map[string]coverArtEntry
	coverLoading map[string]bool
	browseGenre  string
	browsePage   int
	browseLoad   bool

	coverReturn   screen
	coverInfo     metadata.Info
	coverViewArt  string
	coverViewLoad bool
	hideAdult     bool

	progressByURL  map[string]store.Progress
	downloadsByNum map[int]store.Download

	libraryFiles   []lib.File
	libraryLoading bool
	localProgress  map[string]store.Progress

	deps            deps.Report
	setupShown      bool
	providerState   map[string]store.ProviderState
	providerLoading bool
	scroll          *scrollState

	quality     string
	theme       string
	settingsRow int
	dirInput    textinput.Model
	dirEditing  bool

	player     *media.Player
	playCancel context.CancelFunc

	queue          []*queueItem
	downloading    bool
	cancelDownload context.CancelFunc
	dlCh           <-chan dlEvent
	lastFailed     *provider.Episode

	lastClickAt  time.Time
	lastClickKey string

	status string
	err    error
}

func New(providers []provider.Provider, outDir string, st *store.Store) Model {
	search := textinput.New()
	search.Placeholder = "search anime…"
	search.Prompt = "> "
	search.CharLimit = 100
	search.Width = 40
	search.PromptStyle = lipgloss.NewStyle().Foreground(colorAccent)
	search.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	search.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorFaint)
	search.Focus()

	scroll := &scrollState{}
	delegate := newThemedDelegate(scroll)
	l := func() list.Model {
		model := list.New(nil, delegate, 40, 8)
		model.SetShowTitle(false)
		model.SetShowStatusBar(false)
		model.SetShowHelp(false)
		model.SetFilteringEnabled(false)
		model.DisableQuitKeybindings()
		return model
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)

	dirInput := textinput.New()
	dirInput.Placeholder = "~/Downloads/weeb-box"
	dirInput.CharLimit = 500
	dirInput.Width = 40

	m := Model{
		providers:      providers,
		byName:         providerMap(providers),
		st:             st,
		outDir:         outDir,
		libraryDir:     outDir,
		screen:         screenHome,
		focus:          focusSearch,
		search:         search,
		resume:         l(),
		recent:         l(),
		main:           l(),
		spinner:        sp,
		progressByURL:  map[string]store.Progress{},
		downloadsByNum: map[int]store.Download{},
		providerState:  map[string]store.ProviderState{},
		scroll:         scroll,
		quality:        "auto",
		theme:          "carbon-dark",
		dirInput:       dirInput,
		width:          80,
		height:         24,
		meta:           metadata.New(),
		covers:         cover.New(),
		metaCache:      map[string]metadata.Info{},
		metaLoading:    map[string]bool{},
		metaFailed:     map[string]bool{},
		coverArt:       map[string]coverArtEntry{},
		coverLoading:   map[string]bool{},
	}
	if len(providers) > 0 {
		m.prov = providers[0]
	}
	m.layout()
	return m
}

func providerMap(providers []provider.Provider) map[string]provider.Provider {
	out := make(map[string]provider.Provider, len(providers))
	for _, p := range providers {
		out[p.Name()] = p
	}
	return out
}

// SetLibrary points the local library at a root other than the download dir.
func (m Model) SetLibrary(dir string) Model {
	if strings.TrimSpace(dir) != "" {
		m.libraryDir = dir
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.SetWindowTitle("weeb-box — "+m.providerLabel()),
		checkDepsCmd(),
		providerStateCmd(m.st),
		scrollTickCmd(),
		loadHomeCmd(m.st, m.libraryDir),
		loadSettingsCmd(m.st),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		// Cover art is rendered at the previous cell size; draw it again at
		// the new one. Metadata stays cached.
		if m.screen == screenResults || m.screen == screenBrowse {
			return m, m.ensurePreview()
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancelDownload != nil {
				m.cancelDownload()
			}
			if m.playCancel != nil {
				m.playCancel()
			}
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case scrollTickMsg:
		if m.scroll != nil {
			m.scroll.offset++
		}
		return m, scrollTickCmd()

	case depsMsg:
		m.deps = deps.Report(msg)
		if m.deps.NeedsSetup() && !m.setupShown {
			m.screen, m.setupShown = screenSetup, true
		}
		if m.screen == screenSetup && m.deps.Ready() {
			m.screen, m.status = screenHome, "Setup complete"
		}
		return m, nil

	case providerStateMsg:
		if msg.states != nil {
			m.providerState = msg.states
		}
		m.providerLoading = false
		if msg.err != nil {
			m.err = msg.err
		}
		if m.screen == screenProviders {
			index := m.main.Index()
			m.main.SetItems(m.providerItems())
			m.main.Select(index)
		}
		return m, nil

	case settingsMsg:
		if msg.quality != "" {
			m.quality = msg.quality
		}
		m.hideAdult = msg.hideAdult
		if msg.theme != "" {
			m.theme = msg.theme
			applyTheme(msg.theme)
			m.refreshTheme()
		}
		return m, nil

	case homeMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		m.stats = msg.stats
		resume := make([]list.Item, len(msg.progress))
		for i, p := range msg.progress {
			resume[i] = item{
				title:    p.AnimeTitle,
				desc:     fmt.Sprintf("E%03d · %s · %s / %s", p.EpisodeNumber, progressPct(p), clock(p.Position), clock(p.Duration)),
				progress: p,
			}
		}
		recent := make([]list.Item, len(msg.recents))
		for i, s := range msg.recents {
			recent[i] = item{title: s.Query, desc: ago(s.LastUsed), query: s.Query}
		}
		m.resume.SetItems(resume)
		m.resume.Select(0)
		m.recent.SetItems(recent)
		m.recent.Select(0)
		return m, nil

	case searchMsg:
		if msg.err != nil {
			m.screen, m.err, m.status = screenHome, msg.err, ""
			return m, nil
		}
		if len(msg.results) == 0 {
			m.screen, m.status = screenHome, fmt.Sprintf("No results for %q", msg.query)
			return m, nil
		}
		m.results = msg.results
		m.main.SetItems(m.animeItems())
		m.main.Select(0)
		m.screen, m.err, m.status = screenResults, nil, ""
		if msg.failed > 0 {
			m.status = fmt.Sprintf("%d source(s) unavailable", msg.failed)
		}
		m.layout()
		return m, m.ensurePreview()

	case metaMsg:
		delete(m.metaLoading, msg.key)
		if msg.err != nil {
			// A missing or unreachable metadata entry only costs the preview.
			m.metaFailed[msg.key] = true
			return m, nil
		}
		m.metaCache[msg.key] = msg.info
		// Only the results and browse screens draw a cover.
		if m.screen == screenResults || m.screen == screenBrowse {
			return m, m.ensureCover(msg.info)
		}
		return m, nil

	case coverMsg:
		delete(m.coverLoading, msg.key)
		cols, rows := m.previewCoverSize()
		if msg.cols != cols || msg.rows != rows {
			// The terminal changed size while this render was in flight;
			// redraw at the current size instead of showing squashed art.
			return m, m.ensurePreview()
		}
		if msg.err == nil && msg.art != "" {
			m.coverArt[msg.key] = coverArtEntry{art: msg.art, cols: cols, rows: rows}
		}
		return m, nil

	case coverViewMsg:
		m.coverViewLoad = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		cols, rows := m.coverViewerSize()
		if msg.cols != cols || msg.rows != rows {
			// Resized mid-render: retry once at the current size.
			if m.screen == screenCover && m.covers != nil && m.coverInfo.CoverURL != "" {
				m.coverViewLoad = true
				return m, coverViewCmd(m.covers, m.coverInfo.CoverURL, cols, rows)
			}
			return m, nil
		}
		m.coverViewArt = msg.art
		return m, nil

	case nativeCoverMsg:
		if msg.err != nil {
			m.err = msg.err
			m.screen = m.coverReturn
			return m, nil
		}
		path := msg.path
		cmd := exec.Command("bash", "-c", `cat "$0"; read -r -n 1 -s`, path)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return nativeCoverDoneMsg{path: path, err: err}
		})

	case nativeCoverDoneMsg:
		_ = os.Remove(msg.path)
		m.screen = m.coverReturn
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.status = "Cover closed"
		return m, nil

	case browseMsg:
		if !m.browseLoad || msg.genre != m.browseGenre {
			return m, nil
		}
		m.browseLoad = false
		m.browsePage = msg.page
		if msg.err != nil {
			m.screen, m.err = screenGenres, msg.err
			return m, nil
		}
		if len(msg.items) == 0 {
			m.main.SetItems(nil)
			m.screen, m.err, m.status = screenBrowse, nil, "no more titles"
			m.layout()
			return m, nil
		}
		items := make([]list.Item, len(msg.items))
		for i, info := range msg.items {
			m.metaCache[metadata.Key(info.Title)] = info
			items[i] = item{title: info.Title, desc: browseDesc(info), info: info}
		}
		m.main.SetItems(items)
		m.main.Select(0)
		m.screen, m.err, m.status = screenBrowse, nil, ""
		m.layout()
		return m, m.ensurePreview()

	case episodesMsg:
		if msg.err != nil {
			m.screen, m.err, m.status = screenResults, msg.err, ""
			return m, nil
		}
		if len(msg.episodes) == 0 {
			m.screen, m.status = screenResults, ""
			m.err = fmt.Errorf("no episodes found")
			return m, nil
		}
		m.episodes = msg.episodes
		m.progressByURL = msg.progress
		m.downloadsByNum = msg.downloads
		items := make([]list.Item, len(msg.episodes))
		for i, ep := range msg.episodes {
			var parts []string
			if ep.Title != "" {
				parts = append(parts, ep.Title)
			}
			if p, ok := msg.progress[ep.URL]; ok && p.Position > 0 {
				parts = append(parts, progressPct(p)+" watched")
			}
			if _, ok := msg.downloads[ep.Number]; ok {
				parts = append(parts, "downloaded")
			}
			items[i] = item{
				title:   fmt.Sprintf("E%03d", ep.Number),
				desc:    strings.Join(parts, " · "),
				episode: ep,
			}
		}
		m.main.SetItems(items)
		m.main.Select(0)
		m.screen, m.err, m.status = screenEpisodes, nil, ""
		m.layout()
		return m, m.ensureMeta(m.anime.Title)

	case libraryMsg:
		m.libraryLoading = false
		if msg.err != nil {
			m.err = msg.err
		}
		m.libraryFiles = msg.files
		m.localProgress = msg.progress
		items := make([]list.Item, len(msg.files))
		for i, file := range msg.files {
			parts := []string{humanBytes(file.Size), file.ModTime.Format("2006-01-02")}
			if file.HasSubs {
				parts = append(parts, "subs")
			}
			if p, ok := msg.progress[file.Path]; ok && p.Position > 0 {
				parts = append(parts, progressPct(p)+" watched")
			}
			items[i] = item{title: file.Title(), desc: strings.Join(parts, " · "), file: file}
		}
		m.main.SetItems(items)
		m.main.Select(0)
		m.screen, m.err, m.status = screenLibrary, nil, ""
		m.layout()
		return m, nil

	case streamMsg:
		if msg.err != nil {
			m.screen, m.err, m.status = m.playReturn, msg.err, ""
			return m, nil
		}
		title := fmt.Sprintf("%s — E%03d", msg.anime.Title, msg.episode.Number)
		if msg.episode.Title != "" {
			title += " · " + msg.episode.Title
		}
		player, err := media.Play(context.Background(), msg.stream, title, msg.start, m.quality)
		if err != nil {
			m.screen = m.playReturn
			m.err = err
			return m, nil
		}
		return m.playWith(player, playbackInfo{
			Provider:      m.providerName(),
			EpisodeURL:    msg.episode.URL,
			AnimeURL:      msg.anime.URL,
			AnimeTitle:    msg.anime.Title,
			EpisodeNumber: msg.episode.Number,
			Duration:      msg.duration,
		}, msg.start)

	case playbackDoneMsg:
		if m.playCancel != nil {
			m.playCancel()
			m.playCancel = nil
		}
		if m.player != nil {
			m.player.Cleanup()
			m.player = nil
		}
		m.screen = m.playReturn
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.status = "Playback ended"
		}
		if m.playReturn == screenHome {
			return m, loadHomeCmd(m.st, m.libraryDir)
		}
		if m.playReturn == screenLibrary {
			m.screen, m.libraryLoading = screenLibrary, true
			return m, tea.Batch(m.spinner.Tick, libraryCmd(m.libraryDir, m.st))
		}
		return m, nil

	case dlEvent:
		if m.dlCh == nil {
			return m, nil
		}
		if msg.index >= 0 && msg.index < len(m.queue) {
			q := m.queue[msg.index]
			q.state = msg.state
			if msg.percent > 0 {
				q.percent = msg.percent
			}
			if msg.bytes > 0 {
				q.bytes = msg.bytes
			}
			if msg.rate > 0 {
				q.rate = msg.rate
			}
			if msg.speed > 0 {
				q.speed = msg.speed
			}
			if msg.eta > 0 {
				q.eta = msg.eta
			}
			if msg.status != "" {
				q.status = msg.status
			}
			if msg.err != nil {
				q.err = msg.err
				m.err = msg.err
				ep := q.episode
				m.lastFailed = &ep
			}
		}
		if msg.done {
			m.downloading = false
			if m.cancelDownload != nil {
				m.cancelDownload()
				m.cancelDownload = nil
			}
			m.dlCh = nil
			if m.err == nil {
				m.status = "Download complete"
			}
			return m, nil
		}
		return m, waitFor(m.dlCh)

	case spinner.TickMsg:
		if m.screen != screenBusy && !(m.screen == screenLibrary && m.libraryLoading) && !(m.screen == screenProviders && m.providerLoading) && !m.browseLoad && !m.coverViewLoad && len(m.metaLoading) == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.screen == screenSettings && m.dirEditing {
		return m.handleSettingsEdit(msg)
	}

	if key == "ctrl+h" || (key == "?" && !(m.screen == screenHome && m.focus == focusSearch)) {
		if m.screen == screenHelp {
			m.screen = m.helpReturn
		} else {
			m.helpReturn = m.screen
			m.screen = screenHelp
		}
		return m, nil
	}
	if key == "q" && !(m.screen == screenHome && m.focus == focusSearch) {
		return m, tea.Quit
	}
	if key == "v" && m.screen != screenHome && m.screen != screenHelp && m.screen != screenBusy {
		if len(m.queue) > 0 {
			m.screen = screenDownloads
			m.layout()
			return m, nil
		}
	}
	if m.screen != screenBusy && (key == "ctrl+l" || (key == "l" && !(m.screen == screenHome && m.focus == focusSearch) && m.screen != screenHelp)) {
		return m.openLibrary()
	}
	if m.screen != screenBusy && key == "ctrl+p" {
		return m.openProviders()
	}
	if m.screen != screenBusy && key == "ctrl+b" {
		return m.openGenres()
	}
	if m.screen != screenBusy && key == "ctrl+o" {
		return m.openSettings()
	}
	if key == "i" && (m.screen == screenResults || m.screen == screenBrowse || m.screen == screenEpisodes) {
		return m.openCover()
	}

	switch m.screen {
	case screenHome:
		return m.handleHomeKey(msg)
	case screenGenres:
		switch key {
		case "esc", "left", "backspace":
			return m.goHome()
		case "enter":
			selected, ok := m.main.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			return m.startBrowse(selected.title)
		}
		var cmd tea.Cmd
		m.main, cmd = m.main.Update(msg)
		return m, cmd
	case screenBrowse:
		switch key {
		case "esc", "left", "backspace":
			return m.openGenres()
		case "n", "]":
			return m.browsePageBy(1)
		case "p", "[":
			return m.browsePageBy(-1)
		case "enter":
			selected, ok := m.main.SelectedItem().(item)
			if !ok || selected.title == "" {
				return m, nil
			}
			title := previewTitle(selected)
			m.search.SetValue(title)
			m.screen, m.busy, m.err = screenBusy, busySearch, nil
			m.status = fmt.Sprintf("Searching %q…", title)
			return m, tea.Batch(m.spinner.Tick, searchCmd(m.st, m.activeProviders(), title))
		}
		var cmd tea.Cmd
		m.main, cmd = m.main.Update(msg)
		return m, tea.Batch(cmd, m.ensurePreview())
	case screenResults:
		switch key {
		case "esc", "left", "backspace":
			m.screen, m.err, m.status = screenHome, nil, ""
			m.layout()
			return m, tea.Batch(loadHomeCmd(m.st, m.libraryDir))
		case "enter":
			selected, ok := m.main.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			if m.adultHidden(selected) {
				m.status = "18+ hidden by the adult filter"
				return m, nil
			}
			if selected.prov == nil {
				m.err = fmt.Errorf("result has no provider")
				return m, nil
			}
			m.prov = selected.prov
			m.anime = selected.anime
			m.screen, m.busy, m.err = screenBusy, busyEpisodes, nil
			m.status = "Loading episodes…"
			return m, tea.Batch(m.spinner.Tick, episodesCmd(m.st, m.prov, m.anime))
		}
		var cmd tea.Cmd
		m.main, cmd = m.main.Update(msg)
		return m, tea.Batch(cmd, m.ensurePreview())

	case screenEpisodes:
		switch key {
		case "esc", "left", "backspace":
			m.showResults()
			return m, m.ensurePreview()
		case "enter":
			selected, ok := m.main.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			m.playReturn = screenEpisodes
			m.screen, m.busy, m.err = screenBusy, busyStream, nil
			m.status = fmt.Sprintf("Resolving E%03d…", selected.episode.Number)
			return m, tea.Batch(m.spinner.Tick, streamCmd(m.st, m.prov, m.anime, selected.episode))
		case "d", "a":
			if m.downloading {
				m.status = "A download is already running"
				return m, nil
			}
			if _, ok := m.main.SelectedItem().(item); !ok {
				return m, nil
			}
			index := m.main.Index()
			if index < 0 || index >= len(m.episodes) {
				return m, nil
			}
			episodes := m.episodes[index : index+1]
			if key == "a" {
				episodes = m.episodes[index:]
			}
			return m.startDownload(episodes)
		}
		var cmd tea.Cmd
		m.main, cmd = m.main.Update(msg)
		return m, cmd

	case screenDownloads:
		switch key {
		case "esc", "left", "backspace":
			if m.anime.Title != "" {
				m.screen = screenEpisodes
			} else {
				m.screen = screenHome
			}
			m.layout()
			return m, nil
		case "x":
			if m.downloading {
				if m.cancelDownload != nil {
					m.cancelDownload()
				}
				for _, q := range m.queue {
					if q.state == qActive || q.state == qQueued {
						q.state = qFailed
						q.err = fmt.Errorf("cancelled")
					}
				}
				m.downloading = false
				m.dlCh = nil
				m.status = "Download cancelled"
			}
			return m, nil
		case "r":
			if !m.downloading && m.lastFailed != nil {
				return m.startDownload([]provider.Episode{*m.lastFailed})
			}
			return m, nil
		case "o":
			dir := m.outDir
			if m.anime.Title != "" {
				dir = filepath.Join(m.outDir, safeName(m.providerName()), safeName(m.anime.Title))
			}
			if err := media.OpenFolder(dir); err != nil {
				m.err = err
			}
			return m, nil
		}
		return m, nil

	case screenLibrary:
		switch key {
		case "esc", "left", "backspace":
			m.screen, m.err, m.status = screenHome, nil, ""
			m.layout()
			return m, tea.Batch(loadHomeCmd(m.st, m.libraryDir))
		case "enter":
			selected, ok := m.main.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			return m.startLocalPlayback(selected.file)
		case "o":
			dir := m.libraryDir
			if selected, ok := m.main.SelectedItem().(item); ok {
				dir = filepath.Dir(selected.file.Path)
			}
			if err := media.OpenFolder(dir); err != nil {
				m.err = err
			}
			return m, nil
		case "r":
			return m.openLibrary()
		}
		var cmd tea.Cmd
		m.main, cmd = m.main.Update(msg)
		return m, cmd

	case screenProviders:
		switch key {
		case "esc", "left", "backspace":
			m.screen, m.err, m.status = screenHome, nil, ""
			m.layout()
			return m, tea.Batch(loadHomeCmd(m.st, m.libraryDir))
		case "enter", " ":
			selected, ok := m.main.SelectedItem().(providerItem)
			if !ok {
				return m, nil
			}
			enabled := !selected.enabled
			if m.st != nil {
				if err := m.st.SetProviderEnabled(context.Background(), selected.name, enabled); err != nil {
					m.err = err
					return m, nil
				}
			}
			state := m.providerState[selected.name]
			state.Name, state.Enabled = selected.name, enabled
			m.providerState[selected.name] = state
			index := m.main.Index()
			m.main.SetItems(m.providerItems())
			m.main.Select(index)
			if enabled {
				m.status = selected.name + " enabled"
			} else {
				m.status = selected.name + " disabled"
			}
			return m, nil
		case "r":
			m.providerLoading = true
			return m, providerStateCmd(m.st)
		}
		var cmd tea.Cmd
		m.main, cmd = m.main.Update(msg)
		return m, cmd

	case screenSettings:
		switch key {
		case "esc", "backspace":
			m.screen, m.err, m.status = screenHome, nil, ""
			m.layout()
			return m, tea.Batch(loadHomeCmd(m.st, m.libraryDir))
		case "up", "k":
			if m.settingsRow > 0 {
				m.settingsRow--
			}
		case "down", "j":
			if m.settingsRow < settingsRowCount-1 {
				m.settingsRow++
			}
		case "home", "g", "pgup":
			m.settingsRow = 0
		case "end", "G", "pgdown":
			m.settingsRow = settingsRowCount - 1
		case " ", "left", "right":
			step := 1
			if key == "left" {
				step = -1
			}
			switch m.settingsRow {
			case settingsQuality:
				m.cycleQuality(step)
			case settingsHideAdult:
				if key == " " {
					m.toggleHideAdult()
				}
			case settingsTheme:
				m.cycleTheme(step)
			}
		case "enter":
			switch m.settingsRow {
			case settingsQuality:
				m.cycleQuality(1)
			case settingsHideAdult:
				m.toggleHideAdult()
			case settingsTheme:
				m.cycleTheme(1)
			case settingsDir:
				m.dirEditing = true
				m.dirInput.SetValue(m.outDir)
				m.dirInput.Width = max(10, m.viewWidth()-30)
				return m, m.dirInput.Focus()
			case settingsProviders:
				return m.openProviders()
			}
		}
		return m, nil

	case screenSetup:
		switch key {
		case "esc", "left", "backspace", "enter":
			m.screen, m.err = screenHome, nil
			m.layout()
			return m, nil
		case "r":
			m.status = ""
			return m, checkDepsCmd()
		}
		return m, nil

	case screenCover:
		switch key {
		case "esc", "enter", "left", "backspace":
			m.screen, m.err = m.coverReturn, nil
			m.layout()
		}
		return m, nil

	case screenHelp:
		if key == "esc" || key == "left" || key == "backspace" {
			m.screen = m.helpReturn
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleHomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.focus {
	case focusSearch:
		switch key {
		case "esc":
			return m, tea.Quit
		case "enter":
			query := strings.TrimSpace(m.search.Value())
			if query == "" {
				return m, nil
			}
			m.screen, m.busy, m.err = screenBusy, busySearch, nil
			m.status = fmt.Sprintf("Searching %q…", query)
			return m, tea.Batch(m.spinner.Tick, searchCmd(m.st, m.activeProviders(), query))
		case "tab", "down":
			switch {
			case len(m.resume.Items()) > 0:
				m.focus = focusResume
			case len(m.recent.Items()) > 0:
				m.focus = focusRecent
			default:
				return m, nil
			}
			m.search.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd

	case focusResume:
		switch key {
		case "esc", "/", "left", "backspace":
			m.focus = focusSearch
			return m, m.search.Focus()
		case "tab":
			if len(m.recent.Items()) > 0 {
				m.focus = focusRecent
				return m, nil
			}
			m.focus = focusSearch
			return m, m.search.Focus()
		case "enter":
			selected, ok := m.resume.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			p := selected.progress
			if p.Provider == "local" {
				return m.startLocalPlayback(lib.File{
					Path:    p.EpisodeURL,
					Name:    filepath.Base(p.EpisodeURL),
					Anime:   p.AnimeTitle,
					Episode: p.EpisodeNumber,
				})
			}
			prov := m.byName[p.Provider]
			if prov == nil {
				m.err = fmt.Errorf("provider %q is not configured", p.Provider)
				return m, nil
			}
			m.prov = prov
			m.playReturn = screenHome
			m.screen, m.busy, m.err = screenBusy, busyStream, nil
			m.status = fmt.Sprintf("Resuming %s E%03d…", p.AnimeTitle, p.EpisodeNumber)
			anime := provider.Anime{Title: p.AnimeTitle, URL: p.AnimeURL}
			episode := provider.Episode{Number: p.EpisodeNumber, URL: p.EpisodeURL}
			return m, tea.Batch(m.spinner.Tick, streamCmd(m.st, m.prov, anime, episode))
		}
		var cmd tea.Cmd
		m.resume, cmd = m.resume.Update(msg)
		return m, cmd

	case focusRecent:
		switch key {
		case "esc", "/", "left", "backspace":
			m.focus = focusSearch
			return m, m.search.Focus()
		case "tab":
			m.focus = focusSearch
			return m, m.search.Focus()
		case "enter":
			selected, ok := m.recent.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			m.screen, m.busy, m.err = screenBusy, busySearch, nil
			m.status = fmt.Sprintf("Searching %q…", selected.query)
			return m, tea.Batch(m.spinner.Tick, searchCmd(m.st, m.activeProviders(), selected.query))
		case "ctrl+r":
			if m.st != nil {
				if err := m.st.ClearSearches(context.Background()); err != nil {
					m.err = err
					return m, nil
				}
			}
			m.recent.SetItems(nil)
			m.status = "Search history cleared"
			return m, nil
		}
		var cmd tea.Cmd
		m.recent, cmd = m.recent.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleSettingsEdit processes text entry for the download directory row.
func (m Model) handleSettingsEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.dirEditing = false
		m.dirInput.Blur()
		m.status = "Download directory unchanged"
		return m, nil
	case "enter":
		dir := expandPath(strings.TrimSpace(m.dirInput.Value()))
		m.dirEditing = false
		m.dirInput.Blur()
		if dir == "" {
			m.err = fmt.Errorf("download directory cannot be empty")
			return m, nil
		}
		old := m.outDir
		m.outDir = dir
		if m.libraryDir == old {
			m.libraryDir = dir
		}
		if m.st != nil {
			if err := m.st.SetSetting(context.Background(), "download_dir", dir); err != nil {
				m.err = err
				return m, nil
			}
		}
		m.status = "Download directory saved"
		return m, nil
	}
	var cmd tea.Cmd
	m.dirInput, cmd = m.dirInput.Update(msg)
	return m, cmd
}

// cycleQuality moves through the quality options and persists the choice.
func (m *Model) cycleQuality(step int) {
	n := len(qualityOptions)
	index := 0
	for i, q := range qualityOptions {
		if q == m.quality {
			index = i
			break
		}
	}
	m.quality = qualityOptions[((index+step)%n+n)%n]
	if m.st != nil {
		_ = m.st.SetSetting(context.Background(), "quality", m.quality)
	}
}

func (m Model) openSettings() (tea.Model, tea.Cmd) {
	m.screen, m.dirEditing, m.err, m.status = screenSettings, false, nil, ""
	m.layout()
	return m, nil
}

// openGenres shows the AniList genre list that drives Browse.
func (m Model) openGenres() (tea.Model, tea.Cmd) {
	items := make([]list.Item, len(metadata.Genres))
	for i, genre := range metadata.Genres {
		items[i] = item{title: genre, desc: "popular · non-adult"}
	}
	m.screen, m.err, m.status = screenGenres, nil, ""
	m.browseLoad = false
	m.main.SetItems(items)
	m.main.Select(0)
	m.layout()
	return m, nil
}

func (m Model) startBrowse(genre string) (tea.Model, tea.Cmd) {
	if m.meta == nil {
		m.err = fmt.Errorf("metadata client unavailable")
		return m, nil
	}
	m.browseGenre, m.browsePage, m.browseLoad = genre, 1, true
	m.screen, m.err, m.status = screenBrowse, nil, ""
	m.main.SetItems(nil)
	m.layout()
	return m, tea.Batch(m.spinner.Tick, browseCmd(m.meta, genre, 1))
}

// browsePageBy moves the genre browse one page forward or back.
func (m Model) browsePageBy(step int) (tea.Model, tea.Cmd) {
	if m.meta == nil || m.browseLoad || m.browseGenre == "" {
		return m, nil
	}
	page := m.browsePage + step
	if page < 1 {
		return m, nil
	}
	m.browsePage, m.browseLoad = page, true
	m.err, m.status = nil, fmt.Sprintf("Loading page %d…", page)
	return m, tea.Batch(m.spinner.Tick, browseCmd(m.meta, m.browseGenre, page))
}

// toggleHideAdult flips the adult filter and persists it.
func (m *Model) toggleHideAdult() {
	m.hideAdult = !m.hideAdult
	if m.st == nil {
		return
	}
	value := "no"
	if m.hideAdult {
		value = "yes"
	}
	_ = m.st.SetSetting(context.Background(), "hide_adult", value)
}

// adultHidden reports whether the selected item is blocked by the adult filter.
func (m Model) adultHidden(selected item) bool {
	if !m.hideAdult {
		return false
	}
	info, ok := m.metaCache[metadata.Key(previewTitle(selected))]
	return ok && info.Adult
}

// ensureMeta starts a lookup for title unless it is cached, failing, or already
// in flight.
func (m *Model) ensureMeta(title string) tea.Cmd {
	title = strings.TrimSpace(title)
	if title == "" || m.meta == nil {
		return nil
	}
	key := metadata.Key(title)
	if _, ok := m.metaCache[key]; ok {
		return nil
	}
	if m.metaLoading[key] || m.metaFailed[key] {
		return nil
	}
	m.metaLoading[key] = true
	return metaCmd(m.st, m.meta, title)
}

// ensureCover renders the cover for info unless it is cached at the current
// terminal size or already in flight.
func (m *Model) ensureCover(info metadata.Info) tea.Cmd {
	if info.CoverURL == "" || m.covers == nil {
		return nil
	}
	key := metadata.Key(info.Title)
	cols, rows := m.previewCoverSize()
	if entry, ok := m.coverArt[key]; ok && entry.cols == cols && entry.rows == rows {
		return nil
	}
	if m.coverLoading[key] {
		return nil
	}
	m.coverLoading[key] = true
	return coverCmd(m.covers, key, info.CoverURL, cols, rows)
}

// coverSelection resolves the metadata for the item the cover key applies to.
func (m Model) coverSelection() (metadata.Info, bool) {
	if m.screen == screenEpisodes {
		info, ok := m.metaCache[metadata.Key(m.anime.Title)]
		return info, ok
	}
	selected, ok := m.main.SelectedItem().(item)
	if !ok {
		return metadata.Info{}, false
	}
	info, ok := m.metaCache[metadata.Key(previewTitle(selected))]
	return info, ok
}

// nativeCover reports the renderer to use for a full-size inline image and
// whether the native viewer is available.
func nativeCover(covers coverRenderer) (nativeCoverRenderer, bool) {
	native, ok := covers.(nativeCoverRenderer)
	if !ok || runtime.GOOS == "windows" || native.Native() == cover.NativeNone {
		return nil, false
	}
	return native, true
}

// openCover shows the selected title's cover: inline through the terminal's
// own protocol when available, otherwise as half-block art.
func (m Model) openCover() (tea.Model, tea.Cmd) {
	if m.covers == nil {
		m.status = "Cover renderer unavailable"
		return m, nil
	}
	info, ok := m.coverSelection()
	if !ok || info.CoverURL == "" {
		m.status = "No cover loaded yet"
		return m, nil
	}
	if m.hideAdult && info.Adult {
		m.status = "18+ hidden by the adult filter"
		return m, nil
	}
	m.coverReturn, m.coverInfo, m.coverViewArt = m.screen, info, ""
	cols, rows := m.coverViewerSize()
	if native, ok := nativeCover(m.covers); ok {
		m.status = "Loading cover…"
		return m, nativeCoverCmd(native, info.CoverURL, cols, rows)
	}
	m.screen, m.coverViewLoad, m.status = screenCover, true, ""
	m.layout()
	return m, tea.Batch(m.spinner.Tick, coverViewCmd(m.covers, info.CoverURL, cols, rows))
}

// ensurePreview loads metadata (or just the cover) for the selected list item.
func (m *Model) ensurePreview() tea.Cmd {
	selected, ok := m.main.SelectedItem().(item)
	if !ok {
		return nil
	}
	title := previewTitle(selected)
	if title == "" {
		return nil
	}
	if info, ok := m.metaCache[metadata.Key(title)]; ok {
		return m.ensureCover(info)
	}
	return m.ensureMeta(title)
}

func previewTitle(it item) string {
	if it.anime.Title != "" {
		return it.anime.Title
	}
	if it.info.Title != "" {
		return it.info.Title
	}
	return it.title
}

// ratingBadge renders the age rating, preferring the 18+ badge for adult titles.
func ratingBadge(info metadata.Info) string {
	if info.Adult {
		return "18+"
	}
	if info.Rating == "" {
		return "Not rated"
	}
	return info.Rating
}

func browseDesc(info metadata.Info) string {
	parts := []string{ratingBadge(info)}
	if len(info.Genres) > 0 {
		parts = append(parts, strings.Join(info.Genres[:min(2, len(info.Genres))], ", "))
	}
	if info.Score > 0 {
		parts = append(parts, fmt.Sprintf("★ %.0f", info.Score))
	}
	return strings.Join(parts, " · ")
}

// compactMeta is the one-line metadata fallback on narrow result lists.
func (m Model) compactMeta(selected item) string {
	title := previewTitle(selected)
	if title == "" {
		return ""
	}
	key := metadata.Key(title)
	if info, ok := m.metaCache[key]; ok {
		if m.hideAdult && info.Adult {
			return errStyle.Render("18+ hidden")
		}
		parts := []string{ratingBadge(info)}
		if len(info.Genres) > 0 {
			parts = append(parts, strings.Join(info.Genres[:min(3, len(info.Genres))], ", "))
		}
		return strings.Join(parts, " · ")
	}
	if m.metaLoading[key] {
		return m.spinner.View() + " loading metadata…"
	}
	return ""
}

// previewBody is the side panel: cover art, rating, genres and top tags.
func (m Model) previewBody(selected item, width int) string {
	title := previewTitle(selected)
	if title == "" {
		return dimStyle.Render("Select a title")
	}
	key := metadata.Key(title)
	info, ok := m.metaCache[key]
	if !ok {
		if m.metaLoading[key] {
			return m.spinner.View() + dimStyle.Render(" loading metadata…")
		}
		return dimStyle.Render("No metadata for " + title)
	}
	var lines []string
	if m.hideAdult && info.Adult {
		return errStyle.Render("18+ hidden")
	}
	// Covers are cached under the resolved title, which may differ from the
	// search title that produced the metadata entry, and only at the current
	// cell size.
	_, rows := m.previewCoverSize()
	if art := m.coverArt[metadata.Key(info.Title)]; art.art != "" && art.cols == width && art.rows == rows {
		lines = append(lines, strings.Split(art.art, "\n")...)
		lines = append(lines, "")
	}
	name := info.Title
	if name == "" {
		name = title
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(colorText).MaxWidth(width).Render(name))
	badge := ratingBadge(info)
	if info.Adult {
		lines = append(lines, errStyle.Render(badge))
	} else {
		lines = append(lines, dimStyle.Render(badge))
	}
	if len(info.Genres) > 0 {
		lines = append(lines, dimStyle.MaxWidth(width).Render(strings.Join(info.Genres, " · ")))
	}
	if len(info.Tags) > 0 {
		lines = append(lines, faintStyle.MaxWidth(width).Render(strings.Join(info.Tags[:min(3, len(info.Tags))], " · ")))
	}
	return strings.Join(lines, "\n")
}

// animeItems rebuilds the results list from the last search, so leaving the
// episode view returns to results instead of showing episodes there.
func (m Model) animeItems() []list.Item {
	items := make([]list.Item, len(m.results))
	for i, result := range m.results {
		name := "source"
		if result.provider != nil {
			name = result.provider.Name()
		}
		desc := name
		if result.anime.Meta != "" {
			desc += " · " + result.anime.Meta
		}
		items[i] = item{title: result.anime.Title, desc: desc, anime: result.anime, prov: result.provider}
	}
	return items
}

func (m *Model) showResults() {
	m.main.SetItems(m.animeItems())
	m.main.Select(0)
	m.screen, m.err, m.status = screenResults, nil, ""
	m.layout()
}

func (m Model) leftMargin() int {
	return max(0, (m.width-m.viewWidth())/2)
}

// bodyOffset mirrors View: header, one blank row, then vertical padding before
// the centered body. Mouse coordinates are absolute terminal cells.
func (m Model) bodyOffset() int {
	header := m.header()
	footer := m.footer()
	body := m.bodyView()
	inner := max(0, m.height-lipgloss.Height(header)-lipgloss.Height(footer)-3)
	pad := max(0, (inner-lipgloss.Height(body))/2)
	return lipgloss.Height(header) + 2 + pad
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.scrollMouse(msg.X, msg.Y, -1), nil
	case tea.MouseButtonWheelDown:
		return m.scrollMouse(msg.X, msg.Y, 1), nil
	case tea.MouseButtonLeft:
		return m.clickMouse(msg.X, msg.Y)
	}
	return m, nil
}

func (m Model) scrollMouse(x, y, delta int) Model {
	left := m.leftMargin()
	if x < left || x >= left+m.viewWidth() {
		return m
	}
	bx := x - left
	switch m.screen {
	case screenHome:
		tagline := 0
		if m.height >= 28 {
			tagline = 1
		}
		if y < m.bodyOffset()+8+tagline {
			return m
		}
		half := (m.viewWidth() - 2) / 2
		switch {
		case bx < half:
			if delta < 0 {
				m.resume.CursorUp()
			} else {
				m.resume.CursorDown()
			}
		case bx >= half+2:
			if delta < 0 {
				m.recent.CursorUp()
			} else {
				m.recent.CursorDown()
			}
		}
	case screenResults, screenBrowse:
		if y >= m.bodyOffset() && (!m.previewSplit() || bx < m.resultsLeftOuter()) {
			if delta < 0 {
				m.main.CursorUp()
			} else {
				m.main.CursorDown()
			}
		}
	case screenGenres, screenLibrary, screenProviders:
		if y >= m.bodyOffset() {
			if delta < 0 {
				m.main.CursorUp()
			} else {
				m.main.CursorDown()
			}
		}
	case screenEpisodes:
		if y >= m.bodyOffset() && bx < m.viewWidth()*56/100 {
			if delta < 0 {
				m.main.CursorUp()
			} else {
				m.main.CursorDown()
			}
		}
	case screenSettings:
		if y >= m.bodyOffset() {
			if delta < 0 && m.settingsRow > 0 {
				m.settingsRow--
			}
			if delta > 0 && m.settingsRow < settingsRowCount-1 {
				m.settingsRow++
			}
		}
	}
	return m
}

func (m Model) clickMouse(x, y int) (tea.Model, tea.Cmd) {
	if m.creatorHit(x, y) {
		if err := media.OpenURL(creatorURL); err != nil {
			m.err = err
		}
		return m, nil
	}
	left := m.leftMargin()
	if x < left || x >= left+m.viewWidth() {
		return m, nil
	}
	bx, by := x-left, y-m.bodyOffset()
	if by < 0 {
		return m, nil
	}
	if m.crumbHome(bx, by) {
		return m.goHome()
	}
	switch m.screen {
	case screenHome:
		return m.clickHome(bx, by)
	case screenResults:
		if m.previewSplit() && bx >= m.resultsLeftOuter() {
			return m, nil
		}
		updated, cmd := m.clickList(&m.main, by, 4, screenResults)
		mm := updated.(Model)
		return mm, tea.Batch(cmd, mm.ensurePreview())
	case screenGenres:
		return m.clickList(&m.main, by, 4, screenGenres)
	case screenBrowse:
		if m.previewSplit() && bx >= m.resultsLeftOuter() {
			return m, nil
		}
		updated, cmd := m.clickList(&m.main, by, 4, screenBrowse)
		mm := updated.(Model)
		return mm, tea.Batch(cmd, mm.ensurePreview())
	case screenEpisodes:
		return m.clickEpisodes(bx, by)
	case screenLibrary:
		return m.clickList(&m.main, by, 4, screenLibrary)
	case screenProviders:
		return m.clickList(&m.main, by, 4, screenProviders)
	case screenSettings:
		if row := by - 4; row >= 0 && row < settingsRowCount {
			m.settingsRow = row
		}
		return m, nil
	}
	return m, nil
}

// crumbHome reports whether a click inside the body lands on the "Home" crumb.
// Every sub-screen except busy starts its body with it.
func (m Model) crumbHome(bx, by int) bool {
	if by != 0 || m.screen == screenHome || m.screen == screenBusy {
		return false
	}
	return bx >= 0 && bx < lipgloss.Width("Home")
}

func (m Model) goHome() (tea.Model, tea.Cmd) {
	m.screen, m.err, m.status = screenHome, nil, ""
	m.layout()
	return m, tea.Batch(loadHomeCmd(m.st, m.libraryDir))
}

func (m Model) clickHome(bx, by int) (tea.Model, tea.Cmd) {
	tagline := 0
	if m.height >= 28 {
		tagline = 1
	}
	searchTop := 4 + tagline
	if by >= searchTop && by < searchTop+3 {
		m.focus = focusSearch
		return m, m.search.Focus()
	}
	panelsTop := 8 + tagline
	if by < panelsTop {
		return m, nil
	}
	half := (m.viewWidth() - 2) / 2
	var target *list.Model
	var targetFocus focus
	switch {
	case bx < half:
		target, targetFocus = &m.resume, focusResume
	case bx >= half+2:
		target, targetFocus = &m.recent, focusRecent
	default:
		return m, nil
	}
	m.search.Blur()
	m.focus = targetFocus
	if by < panelsTop+2 {
		return m, nil
	}
	return m.clickList(target, by, panelsTop+2, screenHome)
}

func (m Model) clickEpisodes(bx, by int) (tea.Model, tea.Cmd) {
	content := m.viewWidth()
	leftOuter := content * 56 / 100
	if bx < leftOuter {
		return m.clickList(&m.main, by, 4, screenEpisodes)
	}
	if bx < leftOuter+2 {
		return m, nil
	}
	selected, ok := m.main.SelectedItem().(item)
	if !ok || selected.episode.Number == 0 {
		return m, nil
	}
	width := max(20, content-leftOuter-2-4)
	for i, line := range strings.Split(m.detailView(selected.episode, width), "\n") {
		if by != 4+i {
			continue
		}
		switch {
		case strings.Contains(line, "Play online"):
			return m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		case strings.Contains(line, "Download episode"):
			return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
		case strings.Contains(line, "Download from here"):
			return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		}
	}
	return m, nil
}

func (m Model) clickList(target *list.Model, by, listStart int, on screen) (tea.Model, tea.Cmd) {
	index := listIndex(*target, by-listStart)
	if index < 0 {
		return m, nil
	}
	key := fmt.Sprintf("%d:%d", on, index)
	if m.lastClickKey == key && time.Since(m.lastClickAt) < 600*time.Millisecond {
		m.lastClickKey = ""
		return m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	m.lastClickKey, m.lastClickAt = key, time.Now()
	target.Select(index)
	return m, nil
}

// listIndex maps a terminal row inside a list to an absolute item index. The
// default list delegate renders each item on two lines.
func listIndex(l list.Model, row int) int {
	if row < 0 {
		return -1
	}
	item := row / 2
	if item < 0 || item >= l.Paginator.PerPage {
		return -1
	}
	index := l.Paginator.Page*l.Paginator.PerPage + item
	if index < 0 || index >= len(l.Items()) {
		return -1
	}
	return index
}

type playbackInfo struct {
	Provider      string
	EpisodeURL    string
	AnimeURL      string
	AnimeTitle    string
	EpisodeNumber int
	Duration      float64
}

func (m Model) playWith(player *media.Player, info playbackInfo, start float64) (tea.Model, tea.Cmd) {
	m.player = player
	ctx, cancel := context.WithCancel(context.Background())
	m.playCancel = cancel
	if m.st != nil {
		_ = m.st.SaveProgress(context.Background(), store.Progress{
			Provider: info.Provider, EpisodeURL: info.EpisodeURL, AnimeURL: info.AnimeURL,
			AnimeTitle: info.AnimeTitle, EpisodeNumber: info.EpisodeNumber,
			Position: start, Duration: info.Duration,
		})
	}
	go player.WatchProgress(ctx, func(position, duration float64) {
		if m.st == nil {
			return
		}
		if duration > 0 && position >= duration-5 {
			position = 0
		}
		_ = m.st.SaveProgress(context.Background(), store.Progress{
			Provider: info.Provider, EpisodeURL: info.EpisodeURL, AnimeURL: info.AnimeURL,
			AnimeTitle: info.AnimeTitle, EpisodeNumber: info.EpisodeNumber,
			Position: position, Duration: duration,
		})
	})
	m.status = "Playing — close the player to return"
	return m, tea.ExecProcess(player.Cmd, func(err error) tea.Msg {
		return playbackDoneMsg{err: err}
	})
}

func (m Model) startLocalPlayback(file lib.File) (tea.Model, tea.Cmd) {
	if file.Path == "" {
		return m, nil
	}
	var saved store.Progress
	if m.st != nil {
		if p, ok, _ := m.st.Progress(context.Background(), "local", file.Path); ok {
			saved = p
		}
	}
	start := saved.Position
	if saved.Duration > 0 && start >= saved.Duration-5 {
		start = 0
	}
	title := file.Title()
	if title == "" {
		title = filepath.Base(file.Path)
	}
	player, err := media.PlayLocal(file.Path, title, start)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.playReturn = m.screen
	return m.playWith(player, playbackInfo{
		Provider: "local", EpisodeURL: file.Path, AnimeURL: file.Path,
		AnimeTitle: file.Anime, EpisodeNumber: file.Episode, Duration: saved.Duration,
	}, start)
}

func (m Model) openLibrary() (tea.Model, tea.Cmd) {
	m.screen, m.libraryLoading, m.err, m.status = screenLibrary, true, nil, ""
	return m, tea.Batch(m.spinner.Tick, libraryCmd(m.libraryDir, m.st))
}

type libraryMsg struct {
	files    []lib.File
	progress map[string]store.Progress
	err      error
}

func libraryCmd(root string, st *store.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		files, err := lib.Scan(root)
		var progress map[string]store.Progress
		if st != nil {
			progress, _ = st.ProgressForProvider(ctx, "local")
		}
		return libraryMsg{files: files, progress: progress, err: err}
	}
}

func (m *Model) startDownload(episodes []provider.Episode) (tea.Model, tea.Cmd) {
	if m.prov == nil {
		m.err = fmt.Errorf("no provider selected")
		return m, nil
	}
	m.queue = make([]*queueItem, len(episodes))
	for i, ep := range episodes {
		m.queue[i] = &queueItem{episode: ep, state: qQueued}
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan dlEvent, 64)
	m.cancelDownload = cancel
	m.dlCh = ch
	m.downloading = true
	m.lastFailed = nil
	m.err = nil
	m.status = fmt.Sprintf("Downloading %d episode(s)…", len(episodes))
	m.screen = screenDownloads
	m.layout()

	dir := filepath.Join(m.outDir, safeName(m.providerName()), safeName(m.anime.Title))
	go runDownloads(ctx, m.prov, m.st, m.anime, dir, m.quality, episodes, ch)
	return m, waitFor(ch)
}

func (m Model) View() string {
	if m.width < 20 {
		m.width = 80
	}
	header := m.header()
	footer := m.footer()
	body := m.bodyView()
	// Keep the chrome at the window edges and center the screen body in the
	// space between them. Place is a no-op when the body is taller than that.
	rule := faintStyle.Render(strings.Repeat("─", m.viewWidth()))
	innerHeight := max(0, m.height-lipgloss.Height(header)-lipgloss.Height(footer)-3)
	body = lipgloss.Place(m.viewWidth(), innerHeight, lipgloss.Center, lipgloss.Center, body)
	view := lipgloss.JoinVertical(lipgloss.Left, header, rule, "", body, "", footer)
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, view)
}

func (m Model) bodyView() string {
	var body string
	switch m.screen {
	case screenHome:
		body = m.homeView()
	case screenResults:
		body = m.resultsView()
	case screenGenres:
		body = m.genresView()
	case screenBrowse:
		body = m.browseView()
	case screenEpisodes:
		body = m.episodesView()
	case screenDownloads:
		body = m.downloadsView()
	case screenLibrary:
		body = m.libraryView()
	case screenProviders:
		body = m.providersView()
	case screenSetup:
		body = m.setupView()
	case screenBusy:
		body = m.busyView()
	case screenHelp:
		body = m.helpView()
	case screenSettings:
		body = m.settingsView()
	case screenCover:
		body = m.coverView()
	}
	if m.err != nil {
		body += "\n" + errStyle.MaxWidth(max(10, m.viewWidth()-2)).Render("error: "+strings.SplitN(m.err.Error(), "\n", 2)[0])
	} else if m.status != "" && m.screen != screenBusy {
		body += "\n" + dimStyle.MaxWidth(max(10, m.viewWidth()-2)).Render(m.status)
	}
	return body
}

func (m Model) header() string {
	left := titleStyle.Render("weeb-box")
	if m.screen != screenHome {
		left += "  " + dimStyle.Render(m.screenName())
	}
	left += "  " + m.creatorLink()
	right := lipgloss.JoinHorizontal(lipgloss.Center, pill(m.providerLabel()), " ", pill("v1"))
	gap := max(1, m.viewWidth()-lipgloss.Width(left)-lipgloss.Width(right))
	return lipgloss.JoinHorizontal(lipgloss.Center, left, strings.Repeat(" ", gap), right)
}

func (m Model) providerName() string {
	if m.prov == nil {
		return "no provider"
	}
	return m.prov.Name()
}

func (m Model) providerLabel() string {
	if m.screen == screenEpisodes || m.screen == screenDownloads {
		return m.providerName()
	}
	active := m.activeProviders()
	if len(active) > 1 {
		return fmt.Sprintf("%d providers", len(active))
	}
	if len(active) == 1 {
		return active[0].Name()
	}
	return "no providers"
}

func (m Model) screenName() string {
	switch m.screen {
	case screenResults:
		return "search"
	case screenGenres:
		return "browse"
	case screenBrowse:
		return "browse"
	case screenEpisodes:
		return "episodes"
	case screenDownloads:
		return "downloads"
	case screenLibrary:
		return "library"
	case screenProviders:
		return "providers"
	case screenSetup:
		return "setup"
	case screenBusy:
		return "working"
	case screenHelp:
		return "help"
	case screenSettings:
		return "settings"
	case screenCover:
		return "cover"
	default:
		return ""
	}
}

func (m Model) footer() string {
	items := []string{hint("↑↓", "move"), hint("enter", "select"), hint("esc", "back"), hint("?", "help")}
	switch m.screen {
	case screenHome:
		if m.focus == focusSearch {
			items = []string{hint("enter", "search"), hint("tab", "history"), hint("ctrl+b", "browse"), hint("ctrl+l", "library"), hint("ctrl+h", "help"), hint("esc", "quit")}
		} else {
			items = []string{hint("enter", "open"), hint("tab", "next"), hint("ctrl+b", "browse"), hint("ctrl+l", "library"), hint("ctrl+h", "help"), hint("esc", "search")}
		}
	case screenGenres:
		items = []string{hint("enter", "browse genre"), hint("esc", "back")}
	case screenBrowse:
		items = []string{hint("enter", "search title"), hint("n/p", "page"), hint("i", "cover"), hint("esc", "genres")}
	case screenResults:
		items = []string{hint("↑↓", "move"), hint("enter", "episodes"), hint("i", "cover"), hint("ctrl+b", "browse"), hint("esc", "back")}
	case screenEpisodes:
		items = []string{hint("enter", "play"), hint("d", "download"), hint("a", "rest"), hint("i", "cover"), hint("v", "queue"), hint("esc", "back")}
	case screenDownloads:
		items = []string{hint("x", "cancel"), hint("r", "retry"), hint("o", "folder"), hint("esc", "back")}
	case screenLibrary:
		items = []string{hint("enter", "play"), hint("o", "folder"), hint("r", "rescan"), hint("esc", "back")}
	case screenProviders:
		items = []string{hint("space", "toggle"), hint("r", "refresh"), hint("esc", "back")}
	case screenSetup:
		items = []string{hint("r", "recheck"), hint("enter", "continue")}
	case screenBusy:
		items = []string{hint("ctrl+c", "quit")}
	case screenHelp:
		items = []string{hint("esc", "back"), hint("q", "quit")}
	case screenSettings:
		if m.dirEditing {
			items = []string{hint("enter", "save"), hint("esc", "cancel")}
		} else {
			items = []string{hint("↑↓", "row"), hint("space", "cycle"), hint("enter", "edit"), hint("esc", "back")}
		}
	case screenCover:
		items = []string{hint("esc", "back")}
	}
	sep := dimStyle.Render("  ·  ")
	fixed := ""
	if m.screen != screenSettings && m.screen != screenBusy {
		fixed = hint("ctrl+o", "settings")
	}
	avail := m.viewWidth()
	if fixed != "" {
		avail -= lipgloss.Width(fixed) + lipgloss.Width(sep)
	}
	for len(items) > 1 && lipgloss.Width(strings.Join(items, sep)) > avail {
		items = items[:len(items)-1]
	}
	out := strings.Join(items, sep)
	if fixed != "" {
		out += sep + fixed
	}
	return out
}

func (m Model) creatorLink() string {
	return dimStyle.Render("(developed by ") +
		"\x1b]8;;" + creatorURL + "\x1b\\" +
		dimStyle.Render("@chotathanos") +
		"\x1b]8;;\x1b\\" +
		dimStyle.Render(")")
}

// creatorHit reports whether an absolute terminal cell is on the header credit.
func (m Model) creatorHit(x, y int) bool {
	if y != 0 {
		return false
	}
	prefix := "weeb-box"
	if m.screen != screenHome {
		prefix += "  " + m.screenName()
	}
	prefix += "  "
	start := m.leftMargin() + lipgloss.Width(prefix)
	return x >= start && x < start+lipgloss.Width(m.creatorLink())
}

func (m Model) homeView() string {
	content := m.viewWidth()
	half := (content - 2) / 2
	inner := max(18, half-4)

	logo := titleStyle.Render(`╦ ╦╔═╗╔═╗╔╗ ╔╦╗╔═╗═╗ ╦
║║║║╣ ║╣ ╠╩╗ ║║║ ║╔╩╦╝
╚╩╝╚═╝╚═╝╚═╝═╩╝╚═╝╩ ╚═`)
	tagline := dimStyle.Render("anime, without the pop-ups") + faintStyle.Render("  ·  search / stream / download / library")

	searchStyleShell := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorLine).
		Padding(0, 1).
		Width(content - 4)
	if m.focus == focusSearch {
		searchStyleShell = searchStyleShell.BorderForeground(colorAccent)
	}
	searchBox := searchStyleShell.Render(m.search.View())

	resumeBody := m.resume.View()
	if len(m.resume.Items()) == 0 {
		resumeBody = dimStyle.Render("Nothing in progress yet")
	}
	recentBody := m.recent.View()
	if len(m.recent.Items()) == 0 {
		recentBody = dimStyle.Render("No searches yet")
	}
	left := panel("Continue watching", resumeBody, inner, m.focus == focusResume)
	right := panel("Recent searches", recentBody, inner, m.focus == focusRecent)

	filesLabel := fmt.Sprintf("%d files", m.stats.Downloads)
	if m.stats.Downloads == 1 {
		filesLabel = "1 file"
	}
	stats := dimStyle.Render("library ") +
		lipgloss.NewStyle().Foreground(colorText).Render(filesLabel) +
		dimStyle.Render("  ·  ") + lipgloss.NewStyle().Foreground(colorText).Render(humanBytes(m.stats.Bytes)) +
		dimStyle.Render("  ·  ") + lipgloss.NewStyle().Foreground(colorText).Render(fmt.Sprintf("%d watched", m.stats.Watched))
	parts := []string{logo}
	if m.height >= 28 {
		parts = append(parts, tagline)
	}
	parts = append(parts,
		"",
		searchBox,
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right),
	)
	// The status line under the body needs a row; drop the stats line rather
	// than push the header off-screen on short terminals.
	if m.height >= 28 || (m.status == "" && m.err == nil) {
		parts = append(parts, "", stats)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) resultsView() string {
	content := m.viewWidth()
	selected, _ := m.main.SelectedItem().(item)
	body := m.main.View()
	if len(m.main.Items()) == 0 {
		body = dimStyle.Render("No results")
	}
	crumb := m.crumbs("Home", fmt.Sprintf("%q", m.search.Value()))
	if m.previewSplit() {
		leftOuter := m.resultsLeftOuter()
		cols, _ := m.previewCoverSize()
		return lipgloss.JoinVertical(lipgloss.Left,
			crumb,
			"",
			lipgloss.JoinHorizontal(lipgloss.Top,
				panel("Results", body, max(10, leftOuter-2), true),
				"  ",
				panel("Preview", m.previewBody(selected, cols), cols+2, false),
			),
		)
	}
	parts := []string{crumb, "", panel("Results", body, content-4, true)}
	if line := m.compactMeta(selected); line != "" {
		parts = append(parts, "", dimStyle.MaxWidth(content-2).Render(line))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// previewSplit reports whether the results screen is wide enough for the side
// preview column.
func (m Model) previewSplit() bool {
	return m.width >= 100
}

// previewCoverSize returns the half-block art size for the results/browse side
// panel. One cell is one pixel column and two pixel rows, so a 2:3 cover wants
// rows ≈ cols*3/4. The height cap mirrors layout(): the panel body is
// m.height-13 rows and the art leaves five lines for title, rating and tags.
func (m Model) previewCoverSize() (cols, rows int) {
	switch {
	case m.width >= 120:
		cols = 38
	case m.width >= 100:
		cols = 32
	default:
		cols = 24
	}
	if maxRows := max(6, m.height-18); cols*3/4 > maxRows {
		cols = maxRows * 4 / 3
	}
	cols = max(minPreviewCols, cols)
	return cols, cols * 3 / 4
}

// coverViewerSize returns the full-screen half-block cover size: about 60% of
// the terminal, clamped so the panel still fits at 80x24.
func (m Model) coverViewerSize() (cols, rows int) {
	cols = max(10, m.width*3/5)
	rows = cols * 3 / 4
	if maxRows := max(3, m.height-16); rows > maxRows {
		cols = max(10, maxRows*4/3)
		rows = cols * 3 / 4
	}
	if maxCols := max(10, m.viewWidth()-8); cols > maxCols {
		cols = maxCols
		rows = cols * 3 / 4
	}
	return cols, rows
}

// resultsLeftOuter is the rendered width of the results list column.
func (m Model) resultsLeftOuter() int {
	content := m.viewWidth()
	if !m.previewSplit() {
		return content
	}
	// Preview panel width is cols+2 content + 2 border, plus a 2-column gap.
	cols, _ := m.previewCoverSize()
	return content - (cols + 4) - 2
}

func (m Model) genresView() string {
	content := m.viewWidth()
	inner := content - 4
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "browse"),
		"",
		panel("Genres", m.main.View(), inner, true),
	)
}

func (m Model) browseView() string {
	content := m.viewWidth()
	inner := content - 4
	title := m.browseLabel()
	if m.browseLoad {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "browse", m.browseGenre),
			"",
			panel(title, m.spinner.View()+" Loading "+m.browseGenre+"…", inner, true),
		)
	}
	if len(m.main.Items()) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "browse", m.browseGenre),
			"",
			panel(title, dimStyle.Render("No titles on this page."), inner, true),
		)
	}
	selected, _ := m.main.SelectedItem().(item)
	if m.previewSplit() {
		leftOuter := m.resultsLeftOuter()
		cols, _ := m.previewCoverSize()
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "browse", m.browseGenre),
			"",
			lipgloss.JoinHorizontal(lipgloss.Top,
				panel(title, m.main.View(), max(10, leftOuter-2), true),
				"  ",
				panel("Preview", m.previewBody(selected, cols), cols+2, false),
			),
		)
	}
	parts := []string{
		m.crumbs("Home", "browse", m.browseGenre),
		"",
		panel(title, m.main.View(), inner, true),
	}
	if line := m.compactMeta(selected); line != "" {
		parts = append(parts, "", dimStyle.MaxWidth(content-2).Render(line))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// browseLabel is the Popular panel title, including the current page.
func (m Model) browseLabel() string {
	page := m.browsePage
	if page < 1 {
		page = 1
	}
	return fmt.Sprintf("Popular · page %d", page)
}

// coverView is the half-block full-screen cover with its metadata.
func (m Model) coverView() string {
	content := m.viewWidth()
	inner := content - 4
	if m.coverViewLoad {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "cover"),
			"",
			panel("Cover", m.spinner.View()+" Loading cover…", inner, true),
		)
	}
	// Keep the art inside what is left after the header, footer, crumbs,
	// metadata lines and the status row.
	artWidth, artHeight := m.coverViewerSize()
	art := lipgloss.NewStyle().MaxWidth(artWidth).MaxHeight(artHeight).Render(strings.TrimRight(m.coverViewArt, "\n"))
	name := m.coverInfo.Title
	if name == "" {
		name = "Cover"
	}
	lines := []string{art, "", lipgloss.NewStyle().Bold(true).Foreground(colorText).MaxWidth(inner - 2).Render(name)}
	badge := ratingBadge(m.coverInfo)
	if m.coverInfo.Adult {
		lines = append(lines, errStyle.Render(badge))
	} else {
		lines = append(lines, dimStyle.Render(badge))
	}
	if len(m.coverInfo.Genres) > 0 {
		lines = append(lines, dimStyle.MaxWidth(inner-2).Render(strings.Join(m.coverInfo.Genres, " · ")))
	}
	if len(m.coverInfo.Tags) > 0 {
		lines = append(lines, faintStyle.MaxWidth(inner-2).Render(strings.Join(m.coverInfo.Tags[:min(5, len(m.coverInfo.Tags))], " · ")))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "cover"),
		"",
		panel("Cover", strings.Join(lines, "\n"), inner, true),
	)
}

func (m Model) episodesView() string {
	content := m.viewWidth()
	leftOuter := content * 56 / 100
	rightOuter := content - leftOuter - 2
	leftInner := max(20, leftOuter-4)
	rightInner := max(20, rightOuter-4)

	body := m.main.View()
	if len(m.main.Items()) == 0 {
		body = dimStyle.Render("No episodes")
	}
	selected, _ := m.main.SelectedItem().(item)
	detail := m.detailView(selected.episode, rightInner)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "search", m.anime.Title),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top,
			panel("Episodes", body, leftInner, true),
			"  ",
			panel("Episode", detail, rightInner, false),
		),
	)
}

func (m Model) detailView(ep provider.Episode, width int) string {
	if ep.Number == 0 {
		return dimStyle.Render("Select an episode")
	}
	var lines []string
	lines = append(lines, dimStyle.Render(fmt.Sprintf("EPISODE %03d", ep.Number)))
	title := ep.Title
	if title == "" {
		title = fmt.Sprintf("Episode %d", ep.Number)
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(colorText).MaxWidth(width-2).Render(title))
	lines = append(lines, dimStyle.MaxWidth(width-2).Render(m.anime.Title))
	if info, ok := m.metaCache[metadata.Key(m.anime.Title)]; ok {
		meta := ratingBadge(info)
		if len(info.Genres) > 0 {
			meta += " · " + strings.Join(info.Genres, ", ")
		}
		lines = append(lines, dimStyle.MaxWidth(width-2).Render(meta))
		if len(info.Tags) > 0 {
			lines = append(lines, faintStyle.MaxWidth(width-2).Render(strings.Join(info.Tags[:min(4, len(info.Tags))], " · ")))
		}
	}
	lines = append(lines, "")

	if p, ok := m.progressByURL[ep.URL]; ok && p.Position > 0 {
		lines = append(lines, faintStyle.Render("Watch progress"))
		lines = append(lines, bar(progressRatio(p), max(10, width-4)))
		lines = append(lines, dimStyle.Render(fmt.Sprintf("%s · resume at %s", progressPct(p), clock(p.Position))))
		lines = append(lines, "")
	}
	if _, ok := m.downloadsByNum[ep.Number]; ok {
		lines = append(lines, okStyle.Render("✓ downloaded"))
		lines = append(lines, "")
	}
	lines = append(lines,
		action("enter", "Play online", "mpv · resume", width),
		action("d", "Download episode", "HLS", width),
		action("a", "Download from here", "to the end", width),
	)
	return strings.Join(lines, "\n")
}

func (m Model) setupView() string {
	content := m.viewWidth()
	inner := content - 4
	statusLine := func(status deps.Status) string {
		mark := okStyle.Render("✓")
		if !status.Found {
			mark = errStyle.Render("✕")
		}
		detail := status.Detail
		if detail == "" {
			detail = "not installed"
		}
		name := lipgloss.NewStyle().Width(8).Render(status.Name)
		return mark + " " + name + " " + dimStyle.MaxWidth(max(10, inner-14)).Render(detail)
	}
	lines := []string{
		titleStyle.Render("Dependencies"),
		statusLine(m.deps.FFmpeg),
		statusLine(m.deps.MPV),
		statusLine(m.deps.UOSC),
		statusLine(m.deps.Theme),
		"",
	}
	if m.deps.Ready() {
		lines = append(lines, okStyle.Render("Everything is installed."))
	} else {
		lines = append(lines, titleStyle.Render("Install"))
		for _, command := range m.deps.Commands {
			lines = append(lines, "  "+dimStyle.MaxWidth(max(10, inner-6)).Render(command))
		}
		lines = append(lines, "", faintStyle.Render("Full commands: weeb-box -check"))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "setup"),
		"",
		panel("Setup", strings.Join(lines, "\n"), inner, true),
	)
}

func (m Model) providerItems() []list.Item {
	items := make([]list.Item, 0, len(m.providers))
	for _, p := range m.providers {
		state, ok := m.providerState[p.Name()]
		enabled := true
		if ok {
			enabled = state.Enabled
		}
		errText := state.LastError
		if i := strings.IndexByte(errText, '\n'); i >= 0 {
			errText = errText[:i]
		}
		if len(errText) > 90 {
			errText = errText[:90] + "…"
		}
		items = append(items, providerItem{
			name: p.Name(), url: p.SiteURL(), enabled: enabled,
			lastOK: state.LastOK, lastError: errText,
		})
	}
	return items
}

func (m Model) activeProviders() []provider.Provider {
	var out []provider.Provider
	for _, p := range m.providers {
		if state, ok := m.providerState[p.Name()]; ok && !state.Enabled {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (m Model) openProviders() (tea.Model, tea.Cmd) {
	m.screen, m.providerLoading, m.err, m.status = screenProviders, true, nil, ""
	m.main.SetItems(m.providerItems())
	m.main.Select(0)
	m.layout()
	return m, tea.Batch(m.spinner.Tick, providerStateCmd(m.st))
}

func (m Model) providersView() string {
	content := m.viewWidth()
	inner := content - 4
	if m.providerLoading {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "providers"),
			"",
			panel("Providers", m.spinner.View()+" Loading provider status…", inner, true),
		)
	}
	if len(m.providers) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "providers"),
			"",
			panel("Providers", dimStyle.Render("No providers configured."), inner, true),
		)
	}
	enabled := 0
	for _, p := range m.providers {
		if state, ok := m.providerState[p.Name()]; !ok || state.Enabled {
			enabled++
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "providers", fmt.Sprintf("%d/%d enabled", enabled, len(m.providers))),
		"",
		panel("Providers", m.main.View(), inner, true),
	)
}

func (m Model) settingsView() string {
	content := m.viewWidth()
	inner := content - 4

	enabled := 0
	for _, p := range m.providers {
		if state, ok := m.providerState[p.Name()]; !ok || state.Enabled {
			enabled++
		}
	}
	dbPath := "(none)"
	if m.st != nil {
		dbPath = m.st.Path()
	}
	dirValue := m.outDir
	dirNote := "enter to edit"
	if m.dirEditing {
		dirValue = m.dirInput.View()
		dirNote = "enter save · esc cancel"
	}
	rows := []struct {
		label, value, note string
	}{
		{"Preferred quality", m.quality, "space / ← → cycle"},
		{"Download directory", dirValue, dirNote},
		{"Database", dbPath, "-db requires restart"},
		{"Providers", fmt.Sprintf("%d/%d enabled", enabled, len(m.providers)), "ctrl+p providers"},
		{"Hide adult", yesNo(m.hideAdult), "space / enter toggle"},
		{"Color theme", themeLabel(m.theme), "space / ← → cycle"},
		{"Font", "terminal setting", "IBM Plex Mono recommended"},
	}
	labelWidth := 20
	lines := make([]string, len(rows))
	for i, row := range rows {
		line := dimStyle.Width(labelWidth).Render(row.label) +
			lipgloss.NewStyle().Foreground(colorText).Render(row.value)
		if row.note != "" {
			line += dimStyle.Render("  " + row.note)
		}
		if i == m.settingsRow {
			line = titleStyle.Render("› ") + line
		} else {
			line = "  " + line
		}
		lines[i] = lipgloss.NewStyle().MaxWidth(max(10, inner-2)).Render(line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "settings"),
		"",
		panel("Settings", strings.Join(lines, "\n"), inner, true),
	)
}

func (m Model) libraryView() string {
	content := m.viewWidth()
	inner := content - 4
	if m.libraryLoading {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "library"),
			"",
			panel("Library", m.spinner.View()+" Scanning "+m.libraryDir, inner, true),
		)
	}
	if len(m.libraryFiles) == 0 {
		body := dimStyle.Render("No downloaded episodes found.\n\n" + m.libraryDir + "\n\nPress r to rescan, or o to open the folder.")
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "library"),
			"",
			panel("Library", body, inner, true),
		)
	}
	var total int64
	for _, file := range m.libraryFiles {
		total += file.Size
	}
	label := fmt.Sprintf("%d files · %s", len(m.libraryFiles), humanBytes(total))
	if len(m.libraryFiles) == 1 {
		label = fmt.Sprintf("1 file · %s", humanBytes(total))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "library", label),
		"",
		panel("Library", m.main.View(), inner, true),
	)
}

func (m Model) downloadsView() string {
	content := m.viewWidth()
	inner := content - 4
	if len(m.queue) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "downloads"),
			"",
			panel("Queue", dimStyle.Render("No downloads yet. Press d on an episode."), inner, true),
		)
	}
	var lines []string
	for i, q := range m.queue {
		marker := " "
		state := "queued"
		color := dimStyle
		switch q.state {
		case qActive:
			marker = "▸"
			state = fmt.Sprintf("%.0f%%", q.percent*100)
			color = titleStyle
		case qDone:
			marker = "✓"
			state = "done"
			color = okStyle
		case qSkipped:
			marker = "✓"
			state = "already downloaded"
			color = okStyle
		case qFailed:
			marker = "✕"
			state = "failed"
			color = errStyle
		}
		line := fmt.Sprintf("%s E%03d", marker, q.episode.Number)
		if q.episode.Title != "" {
			avail := inner - 2 - lipgloss.Width(line) - len(state) - 3
			if avail > 6 {
				line += "  " + scrollText(q.episode.Title, m.scrollOffset(), avail)
			}
		}
		line += dimStyle.Render(strings.Repeat(" ", max(1, inner-2-lipgloss.Width(line)-len(state))) + state)
		lines = append(lines, color.Render(line))
		if q.state == qActive {
			barWidth := max(10, inner-9-lipgloss.Width(q.status))
			lines = append(lines, "      "+bar(q.percent, barWidth)+" "+dimStyle.Render(q.status))
			lines = append(lines, "      "+dimStyle.MaxWidth(max(10, inner-8)).Render(downloadStats(q)))
		}
		if (q.state == qDone || q.state == qSkipped) && q.bytes > 0 {
			lines = append(lines, "      "+dimStyle.Render(humanBytes(q.bytes)))
		}
		if q.err != nil {
			lines = append(lines, "      "+errStyle.MaxWidth(max(10, inner-8)).MaxHeight(3).Render(q.err.Error()))
		}
		if i < len(m.queue)-1 {
			lines = append(lines, "")
		}
	}
	if limit := max(3, m.height-13); len(lines) > limit {
		lines = append(lines[:limit-1], dimStyle.Render("… more episodes"))
	}
	queueLabel := fmt.Sprintf("%d episodes", len(m.queue))
	if len(m.queue) == 1 {
		queueLabel = "1 episode"
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "downloads", queueLabel),
		"",
		panel("Queue", strings.Join(lines, "\n"), inner, true),
	)
}

func (m Model) busyView() string {
	content := m.viewWidth()
	inner := content - 4
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("weeb-box", m.screenName()),
		"",
		panel("Working", m.spinner.View()+" "+m.status, inner, true),
	)
}

func (m Model) helpView() string {
	content := m.viewWidth()
	half := (content - 2) / 2
	inner := max(18, half-4)
	nav := strings.Join([]string{
		keyline("↑ ↓", "move selection"),
		keyline("enter", "open / confirm"),
		keyline("esc / q", "back / quit"),
		keyline("ctrl+h", "toggle help"),
		keyline("ctrl+b", "browse by genre"),
		keyline("ctrl+p", "providers"),
		keyline("ctrl+o", "settings"),
		keyline("ctrl+l", "downloaded library"),
		keyline("i", "view cover"),
	}, "\n")
	mediaKeys := strings.Join([]string{
		keyline("enter", "play online"),
		keyline("d", "download episode"),
		keyline("a", "download from here"),
		keyline("x", "cancel download"),
		keyline("r", "retry failed"),
		keyline("o", "open download folder"),
		keyline("subs", "auto-loaded (mpv)"),
	}, "\n")
	if navLines, mediaLines := strings.Count(nav, "\n"), strings.Count(mediaKeys, "\n"); navLines < mediaLines {
		nav += strings.Repeat("\n", mediaLines-navLines)
	} else if mediaLines < navLines {
		mediaKeys += strings.Repeat("\n", navLines-mediaLines)
	}
	dbPath := "(none)"
	if m.st != nil {
		dbPath = m.st.Path()
	}
	storage := strings.Join([]string{
		dimStyle.MaxWidth(content - 6).Render("videos  " + m.outDir),
		dimStyle.MaxWidth(content - 6).Render("database " + dbPath),
	}, "\n")
	parts := []string{
		m.crumbs("Home", "help"),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top,
			panel("Navigation", nav, inner, true),
			"  ",
			panel("Playback & downloads", mediaKeys, inner, false),
		),
	}
	// Short terminals cannot fit the storage panel without pushing the header
	// off-screen; settings already shows both paths.
	if m.height >= 28 {
		parts = append(parts, "", panel("Storage", storage, content-4, false))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) crumbs(parts ...string) string {
	var out []string
	for i, part := range parts {
		if i > 0 {
			out = append(out, faintStyle.Render(" / "))
		}
		if i == len(parts)-1 {
			out = append(out, lipgloss.NewStyle().Foreground(colorText).Render(part))
		} else {
			out = append(out, dimStyle.Render(part))
		}
	}
	return lipgloss.NewStyle().MaxWidth(max(10, m.viewWidth()-2)).Render(strings.Join(out, ""))
}

func (m Model) viewWidth() int {
	if m.width <= 0 {
		return 80
	}
	return max(20, min(m.width-4, 110))
}

func (m *Model) layout() {
	content := m.viewWidth()
	m.search.Width = max(10, content-8)
	halfInner := max(18, (content-2)/2-4)
	// Leave room for the logo, search box, panels, stats, header and footer.
	homeRows := max(1, min(8, m.height-23))
	m.resume.SetSize(max(18, halfInner-2), homeRows)
	m.recent.SetSize(max(18, halfInner-2), homeRows)
	if m.screen == screenEpisodes {
		leftInner := max(20, content*56/100-4)
		// Panels wrap their body at width-2 (they pad by one column); keep
		// list lines inside that so long titles cannot add rows.
		m.main.SetSize(max(20, leftInner-2), max(5, m.height-13))
	} else if m.screen == screenResults && m.previewSplit() {
		m.main.SetSize(max(20, m.resultsLeftOuter()-6), max(5, m.height-13))
	} else {
		m.main.SetSize(max(20, content-6), max(5, m.height-13))
	}
}

func panel(title, body string, width int, focused bool) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorLine).
		Padding(0, 1).
		Width(max(10, width))
	if focused {
		style = style.BorderForeground(colorAccent)
	}
	return style.Render(titleStyle.Render(title) + "\n" + body)
}

func pill(text string) string {
	return lipgloss.NewStyle().
		Foreground(colorMuted).
		Background(colorKeyBg).
		Padding(0, 1).
		Render(text)
}

func keyline(key, desc string) string {
	return dimStyle.Render(desc) + "  " + keycap(key)
}

func keycap(key string) string {
	return lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorKeyBg).
		Padding(0, 1).
		Render(key)
}

func hint(key, desc string) string {
	return keycap(key) + " " + dimStyle.Render(desc)
}

func action(key, what, note string, width int) string {
	line := keycap(key) + "  " + lipgloss.NewStyle().Foreground(colorText).Render(what)
	if max(28, lipgloss.Width(line))+len(note) > width-2 {
		return line
	}
	return line + dimStyle.Render(strings.Repeat(" ", max(1, 28-lipgloss.Width(line)))+note)
}

func bar(ratio float64, width int) string {
	if width < 1 {
		width = 1
	}
	ratio = math.Max(0, math.Min(1, ratio))
	filled := int(math.Round(ratio * float64(width)))
	return titleStyle.Render(strings.Repeat("█", filled)) + faintStyle.Render(strings.Repeat("░", width-filled))
}

func progressRatio(p store.Progress) float64 {
	if p.Duration <= 0 {
		return 0
	}
	return p.Position / p.Duration
}

func progressPct(p store.Progress) string {
	if p.Duration <= 0 {
		return fmt.Sprintf("%s watched", clock(p.Position))
	}
	return fmt.Sprintf("%.0f%%", progressRatio(p)*100)
}

func clock(seconds float64) string {
	if seconds <= 0 {
		return "00:00"
	}
	total := int(seconds)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func downloadStats(q *queueItem) string {
	var parts []string
	if q.bytes > 0 {
		parts = append(parts, humanBytes(q.bytes))
	}
	if q.rate > 0 {
		parts = append(parts, humanBytes(int64(q.rate))+"/s")
	} else if q.speed > 0 {
		parts = append(parts, fmt.Sprintf("%.1fx", q.speed))
	}
	if q.eta > 0 {
		parts = append(parts, "ETA "+clock(q.eta.Seconds()))
	}
	if len(parts) == 0 {
		return "starting ffmpeg…"
	}
	return strings.Join(parts, " · ")
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func ago(unix int64) string {
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

type depsMsg deps.Report

func checkDepsCmd() tea.Cmd {
	return func() tea.Msg { return depsMsg(deps.Check()) }
}

type providerStateMsg struct {
	states map[string]store.ProviderState
	err    error
}

func providerStateCmd(st *store.Store) tea.Cmd {
	return func() tea.Msg {
		if st == nil {
			return providerStateMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		states, err := st.ProviderStates(ctx)
		return providerStateMsg{states: states, err: err}
	}
}

type settingsMsg struct {
	quality   string
	theme     string
	hideAdult bool
	err       error
}

type metaMsg struct {
	key  string
	info metadata.Info
	err  error
}

// coverArtEntry keeps rendered art with the cell size it was drawn for, so a
// stale render from a previous terminal size is never displayed.
type coverArtEntry struct {
	art  string
	cols int
	rows int
}

type coverMsg struct {
	key  string
	art  string
	cols int
	rows int
	err  error
}

type coverViewMsg struct {
	art  string
	cols int
	rows int
	err  error
}

type nativeCoverMsg struct {
	path string
	err  error
}

type nativeCoverDoneMsg struct {
	path string
	err  error
}

type browseMsg struct {
	genre string
	page  int
	items []metadata.Info
	err   error
}

// metaCmd serves a title from the SQLite cache when it is younger than
// metaTTL, otherwise it looks it up and caches the result.
func metaCmd(st *store.Store, meta metaProvider, title string) tea.Cmd {
	key := metadata.Key(title)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if st != nil {
			payload, fetchedAt, ok, err := st.GetAnimeMeta(ctx, key)
			if err == nil && ok && time.Since(fetchedAt) < metaTTL {
				var info metadata.Info
				if json.Unmarshal(payload, &info) == nil {
					return metaMsg{key: key, info: info}
				}
			}
		}
		info, err := meta.Lookup(ctx, title)
		if err != nil {
			return metaMsg{key: key, err: err}
		}
		if st != nil {
			if payload, err := json.Marshal(info); err == nil {
				_ = st.PutAnimeMeta(ctx, key, string(payload))
			}
		}
		return metaMsg{key: key, info: info}
	}
}

func coverCmd(covers coverRenderer, key, url string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		art, err := covers.Render(ctx, url, cols, rows)
		return coverMsg{key: key, art: art, cols: cols, rows: rows, err: err}
	}
}

// coverViewCmd renders the full-screen half-block cover.
func coverViewCmd(covers coverRenderer, url string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		art, err := covers.Render(ctx, url, cols, rows)
		return coverViewMsg{art: art, cols: cols, rows: rows, err: err}
	}
}

// nativeCoverCmd renders the inline escape and stages it in a temp file for
// the viewer process to cat.
func nativeCoverCmd(covers nativeCoverRenderer, url string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		escape, err := covers.RenderNative(ctx, url, cols, rows)
		if err != nil {
			return nativeCoverMsg{err: err}
		}
		file, err := os.CreateTemp("", "weeb-box-cover-*.esc")
		if err != nil {
			return nativeCoverMsg{err: err}
		}
		_, writeErr := file.WriteString(escape)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(file.Name())
			if writeErr != nil {
				return nativeCoverMsg{err: writeErr}
			}
			return nativeCoverMsg{err: closeErr}
		}
		return nativeCoverMsg{path: file.Name()}
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func browseCmd(meta metaProvider, genre string, page int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := meta.Browse(ctx, genre, page)
		return browseMsg{genre: genre, page: page, items: items, err: err}
	}
}

func loadSettingsCmd(st *store.Store) tea.Cmd {
	return func() tea.Msg {
		if st == nil {
			return settingsMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		quality, err := st.GetSetting(ctx, "quality", "auto")
		if err != nil {
			return settingsMsg{err: err}
		}
		theme, err := st.GetSetting(ctx, "theme", "carbon-dark")
		if err != nil {
			return settingsMsg{err: err}
		}
		hideAdult, err := st.GetSetting(ctx, "hide_adult", "no")
		return settingsMsg{quality: quality, theme: theme, hideAdult: hideAdult == "yes", err: err}
	}
}

type homeMsg struct {
	recents  []store.Search
	progress []store.Progress
	stats    store.Stats
	err      error
}

type searchResult struct {
	provider provider.Provider
	anime    provider.Anime
}

type searchMsg struct {
	results []searchResult
	query   string
	failed  int
	err     error
}

type episodesMsg struct {
	episodes  []provider.Episode
	progress  map[string]store.Progress
	downloads map[int]store.Download
	err       error
}

type streamMsg struct {
	stream   provider.Stream
	anime    provider.Anime
	episode  provider.Episode
	start    float64
	duration float64
	err      error
}

type playbackDoneMsg struct {
	err error
}

type dlEvent struct {
	index   int
	state   queueState
	percent float64
	bytes   int64
	rate    float64
	speed   float64
	eta     time.Duration
	status  string
	err     error
	done    bool
}

func loadHomeCmd(st *store.Store, libraryRoot string) tea.Cmd {
	return func() tea.Msg {
		if st == nil {
			return homeMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		recents, err := st.RecentSearches(ctx, 8)
		if err != nil {
			return homeMsg{err: err}
		}
		progress, err := st.RecentProgress(ctx, 6)
		if err != nil {
			return homeMsg{err: err}
		}
		stats, err := st.Stats(ctx)
		if files, scanErr := lib.Scan(libraryRoot); scanErr == nil {
			stats.Downloads = len(files)
			stats.Bytes = 0
			for _, file := range files {
				stats.Bytes += file.Size
			}
		}
		return homeMsg{recents: recents, progress: progress, stats: stats, err: err}
	}
}

func searchCmd(st *store.Store, providers []provider.Provider, query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if st != nil {
			_ = st.AddSearch(ctx, query)
		}
		if len(providers) == 0 {
			return searchMsg{query: query, err: fmt.Errorf("all providers are disabled")}
		}

		type response struct {
			index  int
			animes []provider.Anime
			err    error
		}
		ch := make(chan response, len(providers))
		for i, p := range providers {
			go func(i int, p provider.Provider) {
				animes, err := p.Search(ctx, query)
				if st != nil {
					if err != nil {
						_ = st.MarkProviderError(ctx, p.Name(), err.Error())
					} else {
						_ = st.MarkProviderOK(ctx, p.Name())
					}
				}
				ch <- response{index: i, animes: animes, err: err}
			}(i, p)
		}

		ordered := make([][]provider.Anime, len(providers))
		failed := 0
		var firstErr error
		for range providers {
			result := <-ch
			if result.err != nil {
				failed++
				if firstErr == nil {
					firstErr = result.err
				}
				continue
			}
			ordered[result.index] = result.animes
		}

		var results []searchResult
		for i, p := range providers {
			for _, anime := range ordered[i] {
				results = append(results, searchResult{provider: p, anime: anime})
			}
		}
		if len(results) == 0 && firstErr != nil {
			return searchMsg{query: query, failed: failed, err: firstErr}
		}
		return searchMsg{query: query, results: results, failed: failed}
	}
}

func episodesCmd(st *store.Store, p provider.Provider, anime provider.Anime) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		episodes, err := p.Episodes(ctx, anime)
		if err != nil {
			return episodesMsg{err: err}
		}
		if st == nil {
			return episodesMsg{episodes: episodes}
		}
		progress, _ := st.ProgressForAnime(ctx, p.Name(), anime.URL)
		downloads, _ := st.DownloadsForAnime(ctx, p.Name(), anime.URL)
		return episodesMsg{episodes: episodes, progress: progress, downloads: downloads}
	}
}

func streamCmd(st *store.Store, p provider.Provider, anime provider.Anime, episode provider.Episode) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		stream, err := p.Stream(ctx, episode)
		if err != nil {
			return streamMsg{err: err}
		}
		var saved store.Progress
		if st != nil {
			saved, _, _ = st.Progress(ctx, p.Name(), episode.URL)
		}
		return streamMsg{
			stream:   stream,
			anime:    anime,
			episode:  episode,
			start:    saved.Position,
			duration: saved.Duration,
		}
	}
}

func waitFor(ch <-chan dlEvent) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return dlEvent{done: true}
		}
		msg, ok := <-ch
		if !ok {
			return dlEvent{done: true}
		}
		return msg
	}
}

func runDownloads(ctx context.Context, p provider.Provider, st *store.Store, anime provider.Anime, dir, quality string, episodes []provider.Episode, ch chan<- dlEvent) {
	defer close(ch)
	for i, ep := range episodes {
		if !sendEvent(ctx, ch, dlEvent{index: i, state: qActive, status: "resolving stream…"}) {
			return
		}
		stream, err := p.Stream(ctx, ep)
		if err != nil {
			sendEvent(ctx, ch, dlEvent{index: i, state: qFailed, err: err, done: true})
			return
		}
		name := fmt.Sprintf("%s - E%03d.mp4", safeName(anime.Title), ep.Number)
		dest := filepath.Join(dir, name)
		if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
			if st != nil {
				_ = st.RecordDownload(ctx, store.Download{
					Provider: p.Name(), EpisodeURL: ep.URL, AnimeURL: anime.URL, AnimeTitle: anime.Title,
					EpisodeNumber: ep.Number, Path: dest, Bytes: info.Size(),
				})
			}
			_, _ = media.DownloadSidecar(ctx, stream, dest)
			if !sendEvent(ctx, ch, dlEvent{index: i, state: qSkipped, percent: 1, bytes: info.Size(), status: "already on disk"}) {
				return
			}
			continue
		}
		if !sendEvent(ctx, ch, dlEvent{index: i, state: qActive, status: "downloading…"}) {
			return
		}
		err = media.Download(ctx, stream, dest, quality, func(p media.Progress) {
			sendEvent(ctx, ch, dlEvent{
				index: i, state: qActive, percent: p.Percent,
				bytes: p.Bytes, rate: p.Rate, speed: p.Speed, eta: p.ETA, status: "downloading…",
			})
		})
		if err != nil {
			sendEvent(ctx, ch, dlEvent{index: i, state: qFailed, err: err, done: true})
			return
		}
		_, _ = media.DownloadSidecar(ctx, stream, dest)
		var size int64
		if info, err := os.Stat(dest); err == nil {
			size = info.Size()
		}
		if st != nil {
			_ = st.RecordDownload(ctx, store.Download{
				Provider: p.Name(), EpisodeURL: ep.URL, AnimeURL: anime.URL, AnimeTitle: anime.Title,
				EpisodeNumber: ep.Number, Path: dest, Bytes: size,
			})
		}
		if !sendEvent(ctx, ch, dlEvent{index: i, state: qDone, percent: 1, bytes: size, status: "saved"}) {
			return
		}
	}
	sendEvent(ctx, ch, dlEvent{index: -1, done: true})
}

func sendEvent(ctx context.Context, ch chan<- dlEvent, event dlEvent) bool {
	select {
	case ch <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func expandPath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func safeName(name string) string {
	name = strings.TrimSpace(unsafeName.ReplaceAllString(name, "_"))
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return "unknown"
	}
	return name
}

func newThemedDelegate(state *scrollState) marqueeDelegate {
	d := marqueeDelegate{DefaultDelegate: list.NewDefaultDelegate(), state: state}
	d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(colorText)
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(colorMuted)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(colorAccent).BorderLeftForeground(colorAccent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(colorAccent)
	d.Styles.DimmedTitle = d.Styles.DimmedTitle.Foreground(colorMuted)
	d.Styles.DimmedDesc = d.Styles.DimmedDesc.Foreground(colorFaint)
	return d
}

func (m *Model) refreshTheme() {
	m.search.PromptStyle = lipgloss.NewStyle().Foreground(colorAccent)
	m.search.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	m.search.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorFaint)
	m.spinner.Style = lipgloss.NewStyle().Foreground(colorAccent)
	delegate := newThemedDelegate(m.scroll)
	m.resume.SetDelegate(delegate)
	m.recent.SetDelegate(delegate)
	m.main.SetDelegate(delegate)
}

type themeSpec struct {
	name, label                             string
	accent, muted, faint, line, text, keyBg string
	err, ok                                 string
}

var themeSpecs = []themeSpec{
	{"carbon-dark", "Carbon Dark", "#78a9ff", "#8d8d8d", "#6f6f6f", "#393939", "#f4f4f4", "#262626", "#fa4d56", "#42be65"},
	{"carbon-light", "Carbon Light", "#0043ce", "#525252", "#8d8d8d", "#e0e0e0", "#161616", "#e0e0e0", "#da1e28", "#198038"},
	{"tokyo-night", "Tokyo Night", "#7aa2f7", "#565f89", "#414868", "#2a2e42", "#c0caf5", "#1f2335", "#f7768e", "#9ece6a"},
	{"mono", "Mono", "#e6e6e6", "#8c8c8c", "#5c5c5c", "#3a3a3a", "#f2f2f2", "#2a2a2a", "#d0d0d0", "#d0d0d0"},
}

var (
	colorAccent lipgloss.TerminalColor = lipgloss.Color("#78a9ff")
	colorMuted  lipgloss.TerminalColor = lipgloss.Color("#8d8d8d")
	colorFaint  lipgloss.TerminalColor = lipgloss.Color("#6f6f6f")
	colorLine   lipgloss.TerminalColor = lipgloss.Color("#393939")
	colorText   lipgloss.TerminalColor = lipgloss.Color("#f4f4f4")
	colorKeyBg  lipgloss.TerminalColor = lipgloss.Color("#262626")
	colorErr    lipgloss.TerminalColor = lipgloss.Color("#fa4d56")
	colorOK     lipgloss.TerminalColor = lipgloss.Color("#42be65")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	dimStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	faintStyle = lipgloss.NewStyle().Foreground(colorFaint)
	errStyle   = lipgloss.NewStyle().Foreground(colorErr)
	okStyle    = lipgloss.NewStyle().Foreground(colorOK)
)

func applyTheme(name string) {
	spec := themeSpecs[0]
	for _, candidate := range themeSpecs {
		if candidate.name == name {
			spec = candidate
			break
		}
	}
	colorAccent = lipgloss.Color(spec.accent)
	colorMuted = lipgloss.Color(spec.muted)
	colorFaint = lipgloss.Color(spec.faint)
	colorLine = lipgloss.Color(spec.line)
	colorText = lipgloss.Color(spec.text)
	colorKeyBg = lipgloss.Color(spec.keyBg)
	colorErr = lipgloss.Color(spec.err)
	colorOK = lipgloss.Color(spec.ok)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	dimStyle = lipgloss.NewStyle().Foreground(colorMuted)
	faintStyle = lipgloss.NewStyle().Foreground(colorFaint)
	errStyle = lipgloss.NewStyle().Foreground(colorErr)
	okStyle = lipgloss.NewStyle().Foreground(colorOK)
}

func themeLabel(name string) string {
	for _, spec := range themeSpecs {
		if spec.name == name {
			return spec.label
		}
	}
	return themeSpecs[0].label
}

func (m *Model) cycleTheme(step int) {
	index := 0
	for i, spec := range themeSpecs {
		if spec.name == m.theme {
			index = i
			break
		}
	}
	n := len(themeSpecs)
	m.theme = themeSpecs[((index+step)%n+n)%n].name
	applyTheme(m.theme)
	m.refreshTheme()
	if m.st != nil {
		_ = m.st.SetSetting(context.Background(), "theme", m.theme)
	}
}
