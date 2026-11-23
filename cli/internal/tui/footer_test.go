package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ponder2000/weeb-box/internal/provider"
)

func TestFooterDownloadActivity(t *testing.T) {
	m := New(nil, t.TempDir(), nil)
	m.screen, m.width = screenEpisodes, 80
	check := func(m Model, want string) string {
		t.Helper()
		footer := m.footer()
		if !strings.Contains(footer, want) {
			t.Errorf("footer %q missing %q", footer, want)
		}
		if lipgloss.Height(footer) != 1 || lipgloss.Width(footer) > m.viewWidth() {
			t.Errorf("footer does not fit one row at width %d: %q", m.viewWidth(), footer)
		}
		return footer
	}
	check(m, "Download idle")

	active := func(m Model) Model {
		m.queue = []*queueItem{{episode: provider.Episode{Number: 8}, state: qQueued}}
		m.downloading = true
		m.dlCh = make(chan dlEvent)
		check(m, "E008 queued")
		updated, _ := m.Update(dlEvent{index: 0, state: qActive, percent: .64, rate: 8.25 * 1024 * 1024})
		return updated.(Model)
	}
	progress := active(m)
	if footer := check(progress, "E008 64% · 8.2 MB/s"); !strings.Contains(footer, "play") {
		t.Errorf("footer %q missing the episode shortcut", footer)
	}
	narrow := progress
	narrow.width = 24
	check(narrow, "E008 64% 8.2 MB/s")

	completed, _ := progress.Update(dlEvent{index: 0, state: qDone, percent: 1})
	check(completed.(Model), "Finishing…")
	completed, _ = completed.Update(dlEvent{index: -1, done: true})
	check(completed.(Model), "Download complete")

	progress = active(m)
	failed, _ := progress.Update(dlEvent{index: 0, state: qFailed, err: errors.New("test failure"), done: true})
	check(failed.(Model), "E008 failed")

	progress = active(m)
	progress.screen = screenDownloads
	cancelled, _ := progress.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	check(cancelled.(Model), "Download cancelled")
}
