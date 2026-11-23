package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/ponder2000/weeb-box/internal/filler"
	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/store"
)

const (
	episodeInspectorWidth = 32
	episodeInspectorGap   = 3
	episodeCompactActions = 3 // Crumb, series name, metadata.
	episodeCatalogHeader  = 3 // Heading, rule, column labels.
	episodeStatusWidth    = 8
)

type episodeDelegate struct{}

func (episodeDelegate) Height() int                         { return 1 }
func (episodeDelegate) Spacing() int                        { return 0 }
func (episodeDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func episodeState(p store.Progress) token {
	switch {
	case p.Position > 0 && p.Duration > 0:
		return token{text: progressPct(p), kind: tokAccent}
	case p.Position > 0:
		return token{text: "Started", kind: tokAccent}
	case p.Duration > 0:
		return token{text: "✓ done", kind: tokOK}
	default:
		return token{text: "Unplayed", kind: tokMuted}
	}
}

func (episodeDelegate) Render(w io.Writer, l list.Model, index int, data list.Item) {
	it, ok := data.(item)
	if !ok {
		return
	}
	ep := it.episode
	number := fmt.Sprintf("E%03d", ep.Number)
	titleWidth := max(1, l.Width()-episodeStatusWidth-10)
	title := strings.Join(strings.Fields(ep.Title), " ")
	if title == "" {
		title = fmt.Sprintf("Episode %d", ep.Number)
	}
	state := episodeState(store.Progress{})
	if len(it.tokens) > 0 {
		state = it.tokens[0]
		for _, badge := range it.tokens[1:] {
			if s := " · " + badge.text; runewidth.StringWidth(s)+8 <= titleWidth {
				title = runewidth.Truncate(title, titleWidth-runewidth.StringWidth(s), "…") + s
			}
		}
	}
	left := runewidth.FillRight(number, 6) + " " +
		runewidth.FillRight(runewidth.Truncate(title, titleWidth, "…"), titleWidth) + " "
	status := runewidth.FillLeft(runewidth.Truncate(state.text, episodeStatusWidth, "…"), episodeStatusWidth)
	if index == l.Index() {
		fmt.Fprint(w, lipgloss.NewStyle().Foreground(colorText).Background(colorKeyBg).
			Width(l.Width()).MaxWidth(l.Width()).Render("▸ "+left+status))
		return
	}
	fmt.Fprint(w, lipgloss.NewStyle().MaxWidth(l.Width()).Render("  "+
		lipgloss.NewStyle().Foreground(colorText).Render(left)+
		lipgloss.NewStyle().Foreground(colorFor(state.kind)).Render(status)))
}

func (m Model) episodeWide() bool {
	return m.previewSplit() && m.height >= 20
}

func (m Model) episodeCatalogWidth() int {
	width := m.viewWidth()
	if m.episodeWide() {
		width -= episodeInspectorWidth + episodeInspectorGap
	}
	return width
}

func (m Model) episodeBodyHeight() int {
	// Reserve a status/error line even when it's empty so pagination stays put.
	return m.bodyHeight() - 1
}

func (m Model) episodeRowsTop() int {
	if m.episodeWide() {
		return episodeCompactActions + 1 + episodeCatalogHeader
	}
	return episodeCompactActions + 3 + episodeCatalogHeader // Actions, progress, details.
}

func (m Model) episodeCatalogHit(bx, by int) bool {
	top := m.episodeRowsTop()
	return bx >= 0 && bx < m.episodeCatalogWidth() &&
		by >= top && by < top+m.episodeVisibleRows()
}

func (m Model) episodeVisibleRows() int {
	return m.main.Paginator.ItemsOnPage(len(m.main.Items()))
}

func (m Model) episodeActionRow() int {
	if m.episodeWide() {
		return m.episodeRowsTop() + max(1, m.episodeVisibleRows()) + 1
	}
	return episodeCompactActions
}

func (m Model) episodeCoverSize() (cols, rows int) {
	// Fit portrait art between the inspector heading and its series facts.
	rows = min(episodeInspectorWidth*3/4, max(1, m.episodeBodyHeight()-9))
	return rows * 4 / 3, rows
}

func (m *Model) ensureEpisodePreview() tea.Cmd {
	if info, ok := m.coverSelection(); ok {
		if m.episodeWide() && !(m.hideAdult && info.Adult) {
			return m.ensureCover(info)
		}
		return nil
	}
	return m.ensureMeta(m.anime.Title)
}

func (m Model) episodesView() string {
	width := m.episodeCatalogWidth()
	info, known := m.coverSelection()
	var meta []string
	if known {
		meta = append(meta, ratingBadge(info))
		if len(info.Genres) > 0 {
			meta = append(meta, strings.Join(info.Genres[:min(2, len(info.Genres))], " / "))
		}
		if !m.episodeWide() && info.Duration > 0 {
			meta = append(meta, fmt.Sprintf("%dm per episode", info.Duration))
		}
	}
	meta = append(meta, m.providerName())
	selected, _ := m.main.SelectedItem().(item)
	ep := selected.episode
	lines := []string{
		m.crumbs("Home", "episodes"),
		titleStyle.Render(runewidth.Truncate(m.anime.Title, m.viewWidth(), "…")),
		dimStyle.Render(runewidth.Truncate(strings.Join(meta, " · "), m.viewWidth(), "…")),
	}
	body := dimStyle.Render("No episodes")
	if m.episodeVisibleRows() > 0 {
		body = strings.TrimRight(m.main.View(), "\n ")
	}
	count := fmt.Sprintf("%d episodes", len(m.main.Items()))
	if m.main.Paginator.TotalPages > 1 {
		count += fmt.Sprintf(" · %d/%d", m.main.Paginator.Page+1, m.main.Paginator.TotalPages)
	}
	label := "Episode catalog"
	gap := width - runewidth.StringWidth(label) - runewidth.StringWidth(count)
	heading := titleStyle.Render(label)
	if gap > 0 {
		heading += strings.Repeat(" ", gap) + dimStyle.Render(count)
	}
	columns := "  NO.    EPISODE"
	if width < runewidth.StringWidth(columns)+episodeStatusWidth {
		columns = "  #      EP"
	}
	catalog := strings.Join([]string{
		heading,
		faintStyle.Render(strings.Repeat("─", width)),
		dimStyle.Render(runewidth.FillRight(columns, width-episodeStatusWidth) + "  STATUS"),
		body,
	}, "\n")

	var buttons []string
	for _, action := range m.episodeActions(ep) {
		buttons = append(buttons, action.label)
	}
	details := []string{
		lipgloss.NewStyle().MaxWidth(width).Render(strings.Join(buttons, "  ")),
		m.episodeProgress(ep, width),
		m.episodeDetails(ep, width),
	}
	if m.episodeWide() {
		label := fmt.Sprintf("E%03d", ep.Number)
		if ep.Number == 0 {
			label = ""
		} else if ep.Title != "" {
			label += "  /  " + strings.Join(strings.Fields(ep.Title), " ")
		}
		left := strings.Join(append([]string{catalog, titleStyle.Render(runewidth.Truncate(label, width, "…"))}, details...), "\n")
		columns := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(width).Render(left),
			strings.Repeat(" ", episodeInspectorGap), m.episodeInspector(info, known))
		lines = append(lines, "", columns)
	} else {
		lines = append(lines, details...)
		lines = append(lines, catalog)
	}
	return strings.Join(lines, "\n")
}

func (m Model) episodeInspector(info metadata.Info, known bool) string {
	width := episodeInspectorWidth
	lines := []string{titleStyle.Render("Series inspector"), faintStyle.Render(strings.Repeat("─", width))}
	cols, rows := m.episodeCoverSize()
	switch {
	case m.hideAdult && info.Adult:
		lines = append(lines, errStyle.Render("18+ hidden"))
	case info.CoverURL != "":
		key := metadata.Key(info.Title)
		if art, ok := m.coverArt[key]; ok && art.cols == cols && art.rows == rows {
			lines = append(lines, lipgloss.PlaceHorizontal(width, lipgloss.Center, art.art))
		} else if m.coverLoading[key] {
			lines = append(lines, faintStyle.Render("Loading cover…"))
		}
	}
	title := info.Title
	if title == "" {
		title = m.anime.Title
	}
	lines = append(lines, titleStyle.Render(runewidth.Truncate(title, width, "…")))
	if known {
		rating := ratingBadge(info)
		if info.Duration > 0 {
			rating += fmt.Sprintf(" · %dm per episode", info.Duration)
		}
		lines = append(lines, dimStyle.Render(runewidth.Truncate(rating, width, "…")))
		for _, parts := range [][]string{info.Genres, info.Tags} {
			if len(parts) > 0 {
				lines = append(lines, dimStyle.Render(runewidth.Truncate(strings.Join(parts, " · "), width, "…")))
			}
		}
	}
	return strings.Join(append(lines, dimStyle.Render(runewidth.Truncate("Source · "+m.providerName(), width, "…"))), "\n")
}

type episodeAction struct {
	key   tea.KeyMsg
	label string
}

func (m Model) episodeActions(ep provider.Episode) []episodeAction {
	if ep.Number == 0 {
		return nil
	}
	play, watch := "Play episode", "Mark watched"
	if p := m.progressByURL[ep.URL]; p.Position > 0 {
		play = "Resume at " + clock(p.Position)
	} else if p.Duration > 0 {
		watch = "Mark unwatched"
	}
	return []episodeAction{
		{tea.KeyMsg{Type: tea.KeyEnter}, primaryAction("↵ " + play)},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, keycap("d Download")},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}}, keycap("w " + watch)},
	}
}

func (m Model) episodeProgress(ep provider.Episode, width int) string {
	p := m.progressByURL[ep.URL]
	if p.Position <= 0 {
		return ""
	}
	if p.Duration <= 0 {
		return dimStyle.Render(clock(p.Position) + " watched")
	}
	label := progressPct(p) + " · " + clock(p.Position) + " / " + clock(p.Duration)
	barWidth := min(26, width-runewidth.StringWidth(label)-2)
	if barWidth < 1 {
		return dimStyle.Render(runewidth.Truncate(label, width, "…"))
	}
	return bar(progressRatio(p), barWidth) + "  " + dimStyle.Render(label)
}

func (m *Model) updateEpisodeProgress(finished bool) {
	info := m.playInfo
	if m.playProg == nil || info.AnimeURL != m.anime.URL || info.Provider != m.providerName() || info.EpisodeURL == "" {
		return
	}
	position, duration, ok := m.playProg.current()
	if !ok {
		return
	}
	if position >= duration-5 || finished {
		position = 0
	}
	if m.progressByURL == nil {
		m.progressByURL = map[string]store.Progress{}
	}
	m.progressByURL[info.EpisodeURL] = store.Progress{
		Provider: info.Provider, EpisodeURL: info.EpisodeURL, AnimeURL: info.AnimeURL,
		AnimeTitle: info.AnimeTitle, EpisodeNumber: info.EpisodeNumber, Position: position, Duration: duration,
	}
	index := m.main.Index()
	m.main.SetItems(m.episodeItems())
	m.main.Select(index)
}

func (m Model) episodeDetails(ep provider.Episode, width int) string {
	var details []string
	switch m.fillerKinds[ep.Number] {
	case filler.Filler, filler.Mixed:
		details = append(details, strings.ToUpper(string(m.fillerKinds[ep.Number]))+" EPISODE")
	}
	if variants := m.variantsByURL[ep.URL]; len(variants) > 0 {
		details = append(details, "quality "+strings.Join(variants, " · "))
	} else if m.variantLoading[ep.URL] {
		details = append(details, "checking quality…")
	}
	return dimStyle.Render(runewidth.Truncate(strings.Join(details, " · "), width, "…"))
}

func (m Model) clickEpisodes(bx, by int) (tea.Model, tea.Cmd) {
	if m.episodeCatalogHit(bx, by) {
		cmd := m.clickListRows(&m.main, by, m.episodeRowsTop(), screenEpisodes, 1)
		return m, tea.Batch(cmd, m.ensureVariants())
	}
	if by == m.episodeActionRow() && bx < m.episodeCatalogWidth() {
		selected, _ := m.main.SelectedItem().(item)
		for _, action := range m.episodeActions(selected.episode) {
			width := lipgloss.Width(action.label)
			if bx >= 0 && bx < width {
				return m.handleKey(action.key)
			}
			bx -= width + 2
			if bx < 0 {
				break
			}
		}
	}
	return m, nil
}
