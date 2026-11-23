package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/ponder2000/weeb-box/internal/metadata"
)

func (m Model) homeHeroHeight() int {
	if m.height < 24 {
		return 6
	}
	return 8
}

func (m Model) homeHistoryTop() int   { return 2 + m.homeHeroHeight() + 3 } // Search/spacer, hero, spacer/heading/rule.
func (m Model) homeHistoryWidth() int { return (m.viewWidth() - 2) * 55 / 100 }

func (m Model) featuredIndex() int {
	if m.focus == focusResume {
		return m.resume.Index()
	}
	for i, row := range m.resume.Items() {
		if it, ok := row.(item); ok && it.progress.Position > 0 {
			return i
		}
	}
	return 0
}

func (m Model) featuredItem() (item, bool) {
	rows := m.resume.Items()
	index := m.featuredIndex()
	if index < 0 || index >= len(rows) {
		return item{}, false
	}
	it, ok := rows[index].(item)
	return it, ok
}

func (m Model) showHomeCover() bool { return m.width >= 100 && m.height >= 30 }

func (m *Model) ensureHomeCover() tea.Cmd {
	it, ok := m.featuredItem()
	if !ok || !m.showHomeCover() {
		return nil
	}
	if info, ok := m.metaCache[metadata.Key(it.title)]; ok {
		if m.hideAdult && info.Adult {
			return nil
		}
		return m.ensureCover(info)
	}
	return m.ensureMeta(it.title)
}

func (m Model) homeResumeButton() string {
	label := "↵ Open episode"
	if it, ok := m.featuredItem(); ok && it.progress.Position > 0 {
		label = "↵ Resume episode"
	}
	return primaryAction(label)
}

func (m Model) homeHero() string {
	it, ok := m.featuredItem()
	width, height := m.viewWidth(), m.homeHeroHeight()
	cols, rows := m.homeCoverSize()
	info := m.metaCache[metadata.Key(it.title)]
	art := m.coverArt[metadata.Key(info.Title)]
	showArt := ok && m.showHomeCover() && !(m.hideAdult && info.Adult) &&
		art.art != "" && art.cols == cols && art.rows == rows
	if showArt {
		width -= 32
	}

	lines := []string{titleStyle.Render("CONTINUE WATCHING") + dimStyle.Render("  /  NEXT UP")}
	if ok {
		p := it.progress
		label := fmt.Sprintf("E%03d", p.EpisodeNumber)
		if p.Position > 0 && p.Duration > 0 {
			label += "  ·  " + progressPct(p) + " watched"
		}
		lines = append(lines,
			lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(runewidth.Truncate(strings.Join(strings.Fields(it.title), " "), width-4, "…")),
			dimStyle.Render(label),
		)
		if height >= 8 {
			progress := ""
			if p.Position > 0 {
				progress = clock(p.Position)
				if p.Duration > 0 {
					progress = bar(progressRatio(p), min(30, max(4, width-24))) + "  " + clock(p.Position) + " / " + clock(p.Duration)
				}
			}
			lines = append(lines, "", progress)
		}
		lines = append(lines, m.homeResumeButton())
	} else {
		lines = append(lines, "", dimStyle.Render("Find a series to start watching."))
	}
	border := colorLine
	if m.focus == focusResume || (ok && m.focus == focusSearch) {
		border = colorAccent
	}
	card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Width(width-2).Height(height-2).Padding(0, 1).
		Render(strings.Join(lines, "\n"))
	if showArt {
		label := titleStyle.Render("SERIES")
		if len(info.Genres) > 0 {
			label += "\n" + dimStyle.Render(strings.Join(info.Genres[:min(2, len(info.Genres))], "\n"))
		}
		label += "\n" + dimStyle.Render(ratingBadge(info))
		cover := lipgloss.JoinHorizontal(lipgloss.Top, art.art, "  ", lipgloss.NewStyle().MaxWidth(28-cols).Render(label))
		return lipgloss.JoinHorizontal(lipgloss.Top, card, "  ", cover)
	}
	return card
}

func (m Model) homeCoverSize() (cols, rows int) {
	rows = m.homeHeroHeight()
	return rows * 4 / 3, rows
}

func (m Model) homeView() string {
	content := m.viewWidth()
	marker := "  "
	if m.focus == focusSearch {
		marker = titleStyle.Render("▸ ")
	}
	search := lipgloss.NewStyle().Width(content).Padding(0, 1).Background(colorKeyBg).Render(marker + m.search.View())
	column := func(title, body string, width int, focused bool) string {
		style := dimStyle.Bold(true)
		if focused {
			style = titleStyle
		}
		rule := faintStyle.Render(strings.Repeat("─", width))
		return lipgloss.NewStyle().Width(width).Render(style.Render(title) + "\n" + rule + "\n" + body)
	}
	// Bubbles fills the viewport even on sparse pages; keep that blank space
	// outside the centered dashboard instead of between history and the stats.
	resume := strings.TrimRight(m.resume.View(), "\n ")
	recent := strings.TrimRight(m.recent.View(), "\n ")
	if len(m.resume.Items()) == 0 {
		resume = dimStyle.Render("No watch history")
	}
	if len(m.recent.Items()) == 0 {
		recent = dimStyle.Render("No searches yet")
	}
	left := m.homeHistoryWidth()
	history := lipgloss.JoinHorizontal(lipgloss.Top,
		column("Watch history", resume, left, m.focus == focusResume), "  ",
		column("Recent searches", recent, content-left-2, m.focus == focusRecent))
	parts := []string{search, "", m.homeHero(), "", history}
	if m.height >= 30 {
		files := fmt.Sprintf("%d files", m.stats.Downloads)
		if m.stats.Downloads == 1 {
			files = "1 file"
		}
		stats := fmt.Sprintf("%s  ·  %s on disk  ·  %d watched", files, humanBytes(m.stats.Bytes), m.stats.Watched)
		parts = append(parts, "", lipgloss.NewStyle().MaxWidth(content).Render(titleStyle.Render("Library")+"  "+dimStyle.Render(stats)))
	}
	return strings.Join(parts, "\n")
}
