package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/store"
)

func TestEpisodeCatalogRowsAndMouse(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			episodes := make([]provider.Episode, 25)
			for i := range episodes {
				episodes[i] = provider.Episode{Number: i + 1, URL: fmt.Sprint(i + 1), Title: strings.Repeat("魔法 Adventure ", 10)}
			}
			m := episodeModel(t, episodes...)
			m.width, m.height = width, 24
			m.progressByURL["1"] = store.Progress{Duration: 1440}
			m.progressByURL["2"] = store.Progress{Position: 840, Duration: 1440}
			m.main.SetItems(m.episodeItems())
			m.layout()
			m.main.Select(1)
			lines := strings.Split(m.episodesView(), "\n")
			for i, want := range []string{"✓ done", "58%", "Unplayed"} {
				line := lines[m.episodeRowsTop()+i]
				at := strings.Index(line, want)
				if at < 0 || lipgloss.Width(line[:at+len(want)]) != m.episodeCatalogWidth() {
					t.Fatalf("row %d has a misaligned %q: %q", i, want, line)
				}
			}
			if !strings.HasPrefix(lines[m.episodeRowsTop()+1], "▸ E002") ||
				!strings.Contains(m.episodesView(), "58% · 14:00 / 24:00") {
				t.Fatal("missing row selection or playback progress")
			}
			if m.episodeWide() != strings.Contains(m.episodesView(), "Series inspector") {
				t.Fatal("inspector layout does not match the terminal width")
			}

			// Ignore the heading and the inspector, but accept the full catalog width.
			top := m.bodyOffset() + m.episodeRowsTop()
			left := m.leftMargin()
			if got := m.scrollMouse(left, top-1, 1); got.main.Index() != 1 {
				t.Fatal("wheel moved the selection outside the catalog")
			}
			if m.episodeWide() {
				if got := m.scrollMouse(left+m.episodeCatalogWidth()+1, top, 1); got.main.Index() != 1 {
					t.Fatal("wheel moved the selection over the inspector")
				}
			}
			m = m.scrollMouse(left+m.episodeCatalogWidth()-1, top, 1)
			if m.main.Index() != 2 {
				t.Fatal("wheel did not move the selection inside the catalog")
			}
			m.main.Select(m.main.Paginator.PerPage)
			want := m.main.Index() + 1
			click := tea.MouseMsg{X: left, Y: top + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
			updated, _ := m.handleMouse(click)
			m = updated.(Model)
			if m.main.Index() != want {
				t.Fatalf("page-two mouse row selected index %d, want %d", m.main.Index(), want)
			}
			updated, cmd := m.handleMouse(click)
			if got := updated.(Model); got.screen != screenBusy || got.busy != busyStream || cmd == nil {
				t.Fatal("double-click did not play the selected episode")
			}
		})
	}
}

func TestEpisodeCoverLoadsAndRerendersAfterResize(t *testing.T) {
	m := episodeModel(t, provider.Episode{Number: 1, URL: "ep"})
	m.width, m.height = 120, 24
	m.fillerTitle = m.anime.Title
	renderer := &fakeCover{art: "REAL COVER"}
	m.covers = renderer
	m.layout()

	info := metadata.Info{Title: m.anime.Title, CoverURL: "cover", Rating: "PG-13", Genres: []string{"Fantasy"}}
	updated, render := m.Update(metaMsg{key: metadata.Key(m.anime.Title), info: info})
	m = updated.(Model)
	if render == nil {
		t.Fatal("metadata did not request wide Episodes art")
	}
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("resize duplicated the in-flight render")
	}
	stale := render().(coverMsg)
	updated, cmd = m.Update(stale)
	m = updated.(Model)
	if cmd == nil || strings.Contains(m.episodesView(), "REAL COVER") {
		t.Fatal("stale art was not replaced")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if renderer.calls != 2 || renderer.rows == stale.rows || !strings.Contains(m.episodesView(), "REAL COVER") {
		t.Fatal("wide inspector did not display the newly sized render")
	}

	updated, cmd = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	view := m.episodesView()
	if cmd != nil || strings.Contains(view, "REAL COVER") || !strings.Contains(view, "PG-13") || !strings.Contains(view, "Fantasy") {
		t.Fatalf("compact render should retain metadata without art:\n%s", view)
	}
}

func TestEpisodeMarkWatchedActionCompletesInProgressEpisode(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ep := provider.Episode{Number: 1, URL: "ep"}
	m := episodeModel(t, ep)
	m.st = st
	m.progressByURL[ep.URL] = store.Progress{Position: 840, Duration: 1440}
	m.main.SetItems(m.episodeItems())
	m.layout()
	actions := m.episodeActions(ep)
	x := m.leftMargin() + lipgloss.Width(actions[0].label) + lipgloss.Width(actions[1].label) + 4
	y := m.bodyOffset() + m.episodeActionRow()
	click := tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(click)
	m = updated.(Model)
	saved, ok, err := st.Progress(context.Background(), m.providerName(), ep.URL)
	if err != nil || !ok || saved.Position != 0 || saved.Duration != 1440 {
		t.Fatalf("marked progress = %+v, found=%v, err=%v", saved, ok, err)
	}
	if !strings.Contains(m.episodesView(), "Mark unwatched") || !strings.Contains(m.episodesView(), "✓ done") {
		t.Fatal("catalog or action did not reflect the updated status")
	}
}

func TestEpisodePlaybackCompletionUpdatesCatalogWithoutStartingACommand(t *testing.T) {
	ep := provider.Episode{Number: 1, URL: "ep"}
	m := episodeModel(t, ep)
	m.main.SetItems(m.episodeItems())
	m.screen, m.playReturn = screenPlaying, screenEpisodes
	m.playInfo = playbackInfo{Provider: m.providerName(), AnimeURL: m.anime.URL, EpisodeURL: ep.URL, EpisodeNumber: ep.Number}
	m.playProg = &playProgress{}
	m.playProg.set(60, 120)
	m.width, m.height = 120, 24

	updated, cmd := m.Update(playbackDoneMsg{})
	m = updated.(Model)
	if m.screen != screenEpisodes || cmd != nil {
		t.Fatal("returning to the wide catalog should not start another command")
	}
	view := m.episodesView()
	if !strings.Contains(view, "50%") || !strings.Contains(view, "Resume at 01:00") {
		t.Fatalf("catalog did not reflect new playback position:\n%s", view)
	}
}
