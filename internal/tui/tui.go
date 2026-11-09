// Package tui is the Bubble Tea interface for browsing, playing, and downloading.
package tui

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponder2000/weeb-box/internal/deps"
	lib "github.com/ponder2000/weeb-box/internal/library"
	"github.com/ponder2000/weeb-box/internal/media"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/store"
)

type screen int

const (
	screenHome screen = iota
	screenResults
	screenEpisodes
	screenDownloads
	screenLibrary
	screenSetup
	screenBusy
	screenHelp
)

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
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

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

	animes   []provider.Anime
	anime    provider.Anime
	episodes []provider.Episode

	progressByURL  map[string]store.Progress
	downloadsByNum map[int]store.Download

	libraryFiles   []lib.File
	libraryLoading bool
	localProgress  map[string]store.Progress

	deps       deps.Report
	setupShown bool

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

func New(p provider.Provider, outDir string, st *store.Store) Model {
	search := textinput.New()
	search.Placeholder = "search anime…"
	search.Prompt = "> "
	search.CharLimit = 100
	search.Width = 40
	search.PromptStyle = lipgloss.NewStyle().Foreground(colorAccent)
	search.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	search.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorFaint)
	search.Focus()

	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(colorText)
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(colorMuted)
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(colorAccent).BorderLeftForeground(colorAccent)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(colorAccent)
	delegate.Styles.DimmedTitle = delegate.Styles.DimmedTitle.Foreground(colorMuted)
	delegate.Styles.DimmedDesc = delegate.Styles.DimmedDesc.Foreground(colorFaint)
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

	m := Model{
		prov:           p,
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
		width:          80,
		height:         24,
	}
	m.layout()
	return m
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
		tea.SetWindowTitle("weeb-box — "+m.providerName()),
		checkDepsCmd(),
		loadHomeCmd(m.st, m.libraryDir),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
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

	case depsMsg:
		m.deps = deps.Report(msg)
		if m.deps.NeedsSetup() && !m.setupShown {
			m.screen, m.setupShown = screenSetup, true
		}
		if m.screen == screenSetup && m.deps.Ready() {
			m.screen, m.status = screenHome, "Setup complete"
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
		if len(msg.animes) == 0 {
			m.screen, m.status = screenHome, fmt.Sprintf("No results for %q", msg.query)
			return m, nil
		}
		m.animes = msg.animes
		m.main.SetItems(m.animeItems())
		m.main.Select(0)
		m.screen, m.err, m.status = screenResults, nil, ""
		m.layout()
		return m, nil

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
		return m, nil

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
		player, err := media.Play(context.Background(), msg.stream, title, msg.start)
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
		if m.screen != screenBusy && !(m.screen == screenLibrary && m.libraryLoading) {
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

	switch m.screen {
	case screenHome:
		return m.handleHomeKey(msg)
	case screenResults:
		switch key {
		case "esc":
			m.screen, m.err, m.status = screenHome, nil, ""
			m.layout()
			return m, tea.Batch(loadHomeCmd(m.st, m.libraryDir))
		case "enter":
			selected, ok := m.main.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			m.anime = selected.anime
			m.screen, m.busy, m.err = screenBusy, busyEpisodes, nil
			m.status = "Loading episodes…"
			return m, tea.Batch(m.spinner.Tick, episodesCmd(m.st, m.prov, m.anime))
		}
		var cmd tea.Cmd
		m.main, cmd = m.main.Update(msg)
		return m, cmd

	case screenEpisodes:
		switch key {
		case "esc":
			m.showResults()
			return m, nil
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
		case "esc":
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
				dir = filepath.Join(m.outDir, safeName(m.prov.Name()), safeName(m.anime.Title))
			}
			if err := media.OpenFolder(dir); err != nil {
				m.err = err
			}
			return m, nil
		}
		return m, nil

	case screenLibrary:
		switch key {
		case "esc":
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

	case screenSetup:
		switch key {
		case "esc", "enter":
			m.screen, m.err = screenHome, nil
			m.layout()
			return m, nil
		case "r":
			m.status = ""
			return m, checkDepsCmd()
		}
		return m, nil

	case screenHelp:
		if key == "esc" {
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
			return m, tea.Batch(m.spinner.Tick, searchCmd(m.st, m.prov, query))
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
		case "esc", "/":
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
		case "esc", "/":
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
			return m, tea.Batch(m.spinner.Tick, searchCmd(m.st, m.prov, selected.query))
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

// animeItems rebuilds the results list from the last search, so leaving the
// episode view returns to results instead of showing episodes there.
func (m Model) animeItems() []list.Item {
	items := make([]list.Item, len(m.animes))
	for i, anime := range m.animes {
		desc := anime.Meta
		if desc == "" {
			desc = "open to browse episodes"
		}
		items[i] = item{title: anime.Title, desc: desc, anime: anime}
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
	case screenResults, screenLibrary:
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
	}
	return m
}

func (m Model) clickMouse(x, y int) (tea.Model, tea.Cmd) {
	left := m.leftMargin()
	if x < left || x >= left+m.viewWidth() {
		return m, nil
	}
	bx, by := x-left, y-m.bodyOffset()
	if by < 0 {
		return m, nil
	}
	switch m.screen {
	case screenHome:
		return m.clickHome(bx, by)
	case screenResults:
		return m.clickList(&m.main, by, 4, screenResults)
	case screenEpisodes:
		return m.clickEpisodes(bx, by)
	case screenLibrary:
		return m.clickList(&m.main, by, 4, screenLibrary)
	}
	return m, nil
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

	dir := filepath.Join(m.outDir, safeName(m.prov.Name()), safeName(m.anime.Title))
	go runDownloads(ctx, m.prov, m.st, m.anime, dir, episodes, ch)
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
	case screenEpisodes:
		body = m.episodesView()
	case screenDownloads:
		body = m.downloadsView()
	case screenLibrary:
		body = m.libraryView()
	case screenSetup:
		body = m.setupView()
	case screenBusy:
		body = m.busyView()
	case screenHelp:
		body = m.helpView()
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
	right := lipgloss.JoinHorizontal(lipgloss.Center, pill(m.providerName()), " ", pill("v1"))
	gap := max(1, m.viewWidth()-lipgloss.Width(left)-lipgloss.Width(right))
	return lipgloss.JoinHorizontal(lipgloss.Center, left, strings.Repeat(" ", gap), right)
}

func (m Model) providerName() string {
	if m.prov == nil {
		return "no provider"
	}
	return m.prov.Name()
}

func (m Model) screenName() string {
	switch m.screen {
	case screenResults:
		return "search"
	case screenEpisodes:
		return "episodes"
	case screenDownloads:
		return "downloads"
	case screenLibrary:
		return "library"
	case screenSetup:
		return "setup"
	case screenBusy:
		return "working"
	case screenHelp:
		return "help"
	default:
		return ""
	}
}

func (m Model) footer() string {
	items := []string{hint("↑↓", "move"), hint("enter", "select"), hint("esc", "back"), hint("?", "help")}
	switch m.screen {
	case screenHome:
		if m.focus == focusSearch {
			items = []string{hint("enter", "search"), hint("tab", "history"), hint("ctrl+l", "library"), hint("ctrl+h", "help"), hint("esc", "quit")}
		} else {
			items = []string{hint("enter", "open"), hint("tab", "next"), hint("ctrl+l", "library"), hint("ctrl+h", "help"), hint("esc", "search")}
		}
	case screenEpisodes:
		items = []string{hint("enter", "play"), hint("d", "download"), hint("a", "rest"), hint("v", "queue"), hint("esc", "back")}
	case screenDownloads:
		items = []string{hint("x", "cancel"), hint("r", "retry"), hint("o", "folder"), hint("esc", "back")}
	case screenLibrary:
		items = []string{hint("enter", "play"), hint("o", "folder"), hint("r", "rescan"), hint("esc", "back")}
	case screenSetup:
		items = []string{hint("r", "recheck"), hint("enter", "continue")}
	case screenBusy:
		items = []string{hint("ctrl+c", "quit")}
	case screenHelp:
		items = []string{hint("esc", "back"), hint("q", "quit")}
	}
	sep := dimStyle.Render("  ·  ")
	for len(items) > 1 && lipgloss.Width(strings.Join(items, sep)) > m.viewWidth() {
		items = items[:len(items)-1]
	}
	return strings.Join(items, sep)
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
	inner := content - 4
	body := m.main.View()
	if len(m.main.Items()) == 0 {
		body = dimStyle.Render("No results")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", fmt.Sprintf("%q", m.search.Value())),
		"",
		panel("Results", body, inner, true),
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
		line := fmt.Sprintf("%s E%03d  %s", marker, q.episode.Number, q.episode.Title)
		if q.episode.Title == "" {
			line = fmt.Sprintf("%s E%03d", marker, q.episode.Number)
		}
		line = lipgloss.NewStyle().MaxWidth(max(10, inner-3-len(state))).Render(line)
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
		keyline("esc", "back"),
		keyline("q", "quit"),
		keyline("ctrl+h", "toggle help"),
		keyline("tab", "search / list"),
		keyline("ctrl+l", "downloaded library"),
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
	return lipgloss.JoinVertical(lipgloss.Left,
		m.crumbs("Home", "help"),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top,
			panel("Navigation", nav, inner, true),
			"  ",
			panel("Playback & downloads", mediaKeys, inner, false),
		),
		"",
		panel("Storage", storage, content-4, false),
	)
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

type homeMsg struct {
	recents  []store.Search
	progress []store.Progress
	stats    store.Stats
	err      error
}

type searchMsg struct {
	animes []provider.Anime
	query  string
	err    error
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

func searchCmd(st *store.Store, p provider.Provider, query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if st != nil {
			_ = st.AddSearch(ctx, query)
		}
		animes, err := p.Search(ctx, query)
		return searchMsg{animes: animes, query: query, err: err}
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

func runDownloads(ctx context.Context, p provider.Provider, st *store.Store, anime provider.Anime, dir string, episodes []provider.Episode, ch chan<- dlEvent) {
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
		err = media.Download(ctx, stream, dest, func(p media.Progress) {
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

func safeName(name string) string {
	name = strings.TrimSpace(unsafeName.ReplaceAllString(name, "_"))
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return "unknown"
	}
	return name
}

var (
	// IBM Carbon dark. Keep these aligned with themes/.
	colorAccent = lipgloss.AdaptiveColor{Light: "#0043ce", Dark: "#78a9ff"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#525252", Dark: "#8d8d8d"}
	colorFaint  = lipgloss.AdaptiveColor{Light: "#8d8d8d", Dark: "#6f6f6f"}
	colorLine   = lipgloss.AdaptiveColor{Light: "#e0e0e0", Dark: "#393939"}
	colorText   = lipgloss.AdaptiveColor{Light: "#161616", Dark: "#f4f4f4"}
	colorKeyBg  = lipgloss.AdaptiveColor{Light: "#e0e0e0", Dark: "#262626"}
	colorErr    = lipgloss.AdaptiveColor{Light: "#da1e28", Dark: "#fa4d56"}
	colorOK     = lipgloss.AdaptiveColor{Light: "#198038", Dark: "#42be65"}

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	dimStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	faintStyle = lipgloss.NewStyle().Foreground(colorFaint)
	errStyle   = lipgloss.NewStyle().Foreground(colorErr)
	okStyle    = lipgloss.NewStyle().Foreground(colorOK)
)
