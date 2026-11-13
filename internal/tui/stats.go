package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/store"
)

type watchStatsMsg struct {
	rows  []store.WatchStat
	stats store.Stats
	err   error
}

func loadWatchStatsCmd(st *store.Store) tea.Cmd {
	return func() tea.Msg {
		if st == nil {
			return watchStatsMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rows, err := st.WatchStats(ctx)
		if err != nil {
			return watchStatsMsg{err: err}
		}
		stats, err := st.Stats(ctx)
		return watchStatsMsg{rows: rows, stats: stats, err: err}
	}
}

// ensureStatsGenres looks up metadata for the top titles so the genre split has
// something to draw. Ten titles keeps the concurrent AniList burst small.
func (m *Model) ensureStatsGenres() tea.Cmd {
	var cmds []tea.Cmd
	for i, row := range m.statsRows {
		if i >= 10 {
			break
		}
		key := metadata.Key(row.AnimeTitle)
		if _, ok := m.metaCache[key]; ok || m.metaLoading[key] || m.metaFailed[key] {
			continue
		}
		m.metaLoading[key] = true
		cmds = append(cmds, metaCmd(m.st, m.meta, row.AnimeTitle))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m Model) openStats() (tea.Model, tea.Cmd) {
	m.screen, m.statsLoading, m.err, m.status = screenStats, true, nil, ""
	m.layout()
	return m, tea.Batch(m.spinner.Tick, loadWatchStatsCmd(m.st))
}

func (m Model) statsView() string {
	content := m.viewWidth()
	inner := content - 4
	if len(m.statsRows) == 0 {
		body := titleStyle.Render("Nothing watched yet")
		body += "\n" + dimStyle.Render("Play an episode, then press s again.")
		if m.statsLoading {
			body = m.spinner.View() + " Reading watch history…"
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			m.crumbs("Home", "stats"),
			"",
			panel("Watch time", body, inner, true),
		)
	}

	total, episodes := 0.0, 0
	for _, row := range m.statsRows {
		total += row.Seconds
		episodes += row.Episodes
	}

	left := []string{
		lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(watchTime(total)),
		dimStyle.Render("total watch time"),
		"",
		lipgloss.NewStyle().Foreground(colorOK).Render(fmt.Sprintf("%d episodes", episodes)) +
			dimStyle.Render(fmt.Sprintf(" · %d titles", len(m.statsRows))),
		dimStyle.Render(fmt.Sprintf("%.1f days", total/86400)),
	}
	if m.stats.Downloads > 0 {
		left = append(left, dimStyle.Render(fmt.Sprintf("%d downloads · %s", m.stats.Downloads, humanBytes(m.stats.Bytes))))
	}
	if last := newestWatch(m.statsRows); last > 0 {
		left = append(left, dimStyle.Render("last watched "+ago(last)))
	}

	genres := genreShares(m)
	right := make([]string, 0, 8)
	switch {
	case len(genres) == 0 && m.statsLoading:
		right = append(right, m.spinner.View()+" Loading genres…")
	case len(genres) == 0:
		right = append(right, dimStyle.Render("No genres yet."))
		right = append(right, faintStyle.Render("AniList metadata for the top titles is unavailable."))
	default:
		limit := 8
		if m.height > 0 && m.height-16 < limit {
			limit = max(3, m.height-16)
		}
		if len(genres) < limit {
			limit = len(genres)
		}
		maxSec := genres[0].seconds
		barWidth := max(8, (content-2)/2-26)
		palette := []lipgloss.TerminalColor{colorAccent, colorOK, colorWarn, colorText}
		for i, share := range genres[:limit] {
			name := lipgloss.NewStyle().
				Foreground(palette[i%len(palette)]).
				Width(12).
				Render(clipText(share.genre, 12))
			pct := fmt.Sprintf("%3.0f%%", 100*share.seconds/total)
			right = append(right, name+" "+bar(share.seconds/maxSec, barWidth)+" "+dimStyle.Render(pct))
		}
	}

	body := []string{
		m.crumbs("Home", "stats"),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top,
			panel("Watch time", strings.Join(left, "\n"), inner/2, true),
			"  ",
			panel("Genres", strings.Join(right, "\n"), inner/2, false),
		),
	}
	if m.height >= 30 && len(m.statsRows) > 1 {
		body = append(body, "", panel("Top titles", m.topTitles(), inner, false))
	}
	body = append(body, "", faintStyle.Render("r refresh · s or esc back"))
	return lipgloss.JoinVertical(lipgloss.Left, body...)
}

// topTitles ranks the longest-watched titles with a proportional bar.
func (m Model) topTitles() string {
	limit := 6
	if len(m.statsRows) < limit {
		limit = len(m.statsRows)
	}
	maxSec := m.statsRows[0].Seconds
	barWidth := max(8, m.viewWidth()-46)
	lines := make([]string, limit)
	for i, row := range m.statsRows[:limit] {
		title := lipgloss.NewStyle().Width(30).Render(clipText(row.AnimeTitle, 30))
		episodes := dimStyle.Render(fmt.Sprintf("%3d ep", row.Episodes))
		lines[i] = fmt.Sprintf("%s %s %s", title, episodes, bar(row.Seconds/maxSec, barWidth))
	}
	return strings.Join(lines, "\n")
}

type genreShare struct {
	genre   string
	seconds float64
}

// genreShares splits each title's time evenly across its genres, so a 10-hour
// Action/Comedy title counts five hours in each.
func genreShares(m Model) []genreShare {
	byGenre := map[string]float64{}
	for _, row := range m.statsRows {
		info, ok := m.metaCache[metadata.Key(row.AnimeTitle)]
		if !ok || len(info.Genres) == 0 {
			continue
		}
		share := row.Seconds / float64(len(info.Genres))
		for _, genre := range info.Genres {
			byGenre[genre] += share
		}
	}
	shares := make([]genreShare, 0, len(byGenre))
	for genre, seconds := range byGenre {
		shares = append(shares, genreShare{genre: genre, seconds: seconds})
	}
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].seconds == shares[j].seconds {
			return shares[i].genre < shares[j].genre
		}
		return shares[i].seconds > shares[j].seconds
	})
	return shares
}

// watchTime renders seconds as a compact human duration.
func watchTime(seconds float64) string {
	minutes := int(seconds / 60)
	switch {
	case minutes >= 24*60:
		return fmt.Sprintf("%dd %dh", minutes/(24*60), (minutes/60)%24)
	case minutes >= 60:
		return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func newestWatch(rows []store.WatchStat) int64 {
	var newest int64
	for _, row := range rows {
		if row.LastWatched > newest {
			newest = row.LastWatched
		}
	}
	return newest
}

// clipText shortens to n runes with an ellipsis, keeping rows aligned.
func clipText(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n < 2 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
