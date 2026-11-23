package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/store"
)

func TestHomeFeaturedResume(t *testing.T) {
	src := stubProvider{}
	m := New([]provider.Provider{src}, t.TempDir(), nil)
	progress := []store.Progress{
		{AnimeTitle: "Finished", Duration: 1440},
		{AnimeTitle: "In progress", Provider: src.Name(), EpisodeNumber: 7, Position: 840, Duration: 1440},
	}
	updated, _ := m.Update(homeMsg{progress: progress})
	m = updated.(Model)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 40}} {
		updated, _ = m.Update(size)
		m = updated.(Model)
		view := m.View()
		if !strings.Contains(m.homeHero(), "In progress") || !strings.Contains(view, "14:00 / 24:00") {
			t.Fatalf("size %v: resume details missing", size)
		}
		clicked := false
		for y, line := range strings.Split(view, "\n") {
			i := strings.Index(line, "Resume episode")
			if i < 0 {
				continue
			}
			got, cmd := m.handleMouse(tea.MouseMsg{X: lipgloss.Width(line[:i]), Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if got.(Model).busy != busyStream || cmd == nil || got.(Model).resume.Index() != 1 {
				t.Fatalf("size %v: click did not resume featured show", size)
			}
			clicked = true
			break
		}
		if !clicked {
			t.Fatalf("size %v: missing Resume button", size)
		}
	}
}

func TestHomeHeroCoverOnlyOnWideTerminals(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	updated, _ := m.Update(homeMsg{progress: []store.Progress{{AnimeTitle: "Frieren", Position: 10}}})
	m = updated.(Model)
	m.width, m.height = 120, 40
	key := metadata.Key("Frieren")
	m.metaCache[key] = metadata.Info{Title: "Frieren", Rating: "PG-13"}
	cols, rows := m.homeCoverSize()
	m.coverArt[key] = coverArtEntry{cols: cols, rows: rows, art: fakeArt(cols, rows)}
	if !strings.Contains(m.homeHero(), "PG-13") {
		t.Fatal("wide hero did not show the cover inspector")
	}
	m.width, m.height = 80, 24
	if strings.Contains(m.homeHero(), "PG-13") {
		t.Fatal("compact hero should leave room for history")
	}
}

func TestHomeCentersDashboardAfterResize(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	updated, _ := m.Update(homeMsg{
		progress: []store.Progress{{AnimeTitle: "Frieren", EpisodeNumber: 7, Position: 840, Duration: 1440}},
		stats:    store.Stats{Downloads: 1},
	})
	m = updated.(Model)
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 22}, {Width: 80, Height: 24}, {Width: 184, Height: 70}} {
		updated, _ = m.Update(size)
		m = updated.(Model)
		lines := strings.Split(m.View(), "\n")
		if len(lines) != size.Height || lipgloss.Width(m.View()) != size.Width {
			t.Fatalf("%v: frame did not fit", size)
		}
		find := func(text string) int {
			for i, line := range lines {
				if strings.Contains(line, text) {
					return i
				}
			}
			t.Fatalf("%v: %q not rendered", size, text)
			return -1
		}
		search := find("Search anime")
		if size.Height == 70 {
			history := find("Watch history")
			stats := find("1 file")
			if stats-history > 5 {
				t.Fatalf("history and library stats separated by %d rows", stats-history)
			}
			top := search - m.bodyStart()
			bottom := size.Height - 3 - stats
			if top < 1 || bottom < top || bottom > top+1 {
				t.Fatalf("dashboard isn't vertically centered: top=%d bottom=%d", top, bottom)
			}
			if m.viewWidth() <= 110 {
				t.Fatal("wide terminal still using the old width cap")
			}
		}
		m.search.Blur()
		m.focus = focusResume
		updated, _ = m.clickMouse(m.leftMargin()+4, search)
		if updated.(Model).focus != focusSearch {
			t.Fatalf("%v: click missed the centered search input", size)
		}
	}
}
