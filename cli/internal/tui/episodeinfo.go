package tui

import (
	"context"
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponder2000/weeb-box/internal/filler"
	"github.com/ponder2000/weeb-box/internal/media"
	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/store"
)

// A filler list changes only when someone edits the site, so a month is plenty.
const fillerTTL = 30 * 24 * time.Hour

type fillerMsg struct {
	title string
	list  filler.List
	err   error
}

// fillerCmd serves the cached list for a title, otherwise fetches it.
func fillerCmd(st *store.Store, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		key := "filler:" + metadata.Key(title)
		if st != nil {
			payload, fetchedAt, ok, err := st.GetAnimeMeta(ctx, key)
			if err == nil && ok && time.Since(fetchedAt) < fillerTTL {
				var list filler.List
				if json.Unmarshal(payload, &list) == nil {
					return fillerMsg{title: title, list: list}
				}
			}
		}
		list, err := filler.New("").Lookup(ctx, title)
		if err != nil {
			return fillerMsg{title: title, err: err}
		}
		if st != nil {
			if payload, err := json.Marshal(list); err == nil {
				_ = st.PutAnimeMeta(ctx, key, string(payload))
			}
		}
		return fillerMsg{title: title, list: list}
	}
}

// applyFiller only trusts a list whose numbering matches this adapter's, so a
// provider that splits a series into seasons cannot badge the wrong episodes.
func (m *Model) applyFiller(msg fillerMsg) tea.Cmd {
	m.fillerLoading = false
	m.fillerTitle = msg.title
	if msg.err == nil && len(m.episodes) > 0 && msg.list.Max() == m.episodes[len(m.episodes)-1].Number {
		kinds := make(map[int]filler.Kind, len(msg.list.Entries))
		for _, entry := range msg.list.Entries {
			if _, seen := kinds[entry.Number]; !seen {
				kinds[entry.Number] = entry.Kind
			}
		}
		m.fillerKinds = kinds
		if m.screen == screenEpisodes {
			index := m.main.Index()
			m.main.SetItems(m.episodeItems())
			m.main.Select(index)
		}
	} else if msg.err != nil && m.screen == screenEpisodes {
		m.status = "filler list unavailable"
	}

	// AniList's title arrived while the provider title was being looked up.
	if len(m.fillerKinds) == 0 && m.fillerRetry != "" && m.fillerRetry != msg.title {
		retry := m.fillerRetry
		m.fillerRetry = ""
		m.fillerTitle = retry
		m.fillerLoading = true
		return fillerCmd(m.st, retry)
	}
	return nil
}

type variantsMsg struct {
	url  string
	list []string
	err  error
}

// variantsCmd resolves the stream once and asks the HLS master playlist which
// heights it offers. shortcut: playback resolves the stream again afterwards.
func variantsCmd(p provider.Provider, episode provider.Episode) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		stream, err := p.Stream(ctx, episode)
		if err != nil {
			return variantsMsg{url: episode.URL, err: err}
		}
		list, err := media.Variants(ctx, nil, stream)
		return variantsMsg{url: episode.URL, list: list, err: err}
	}
}

// ensureVariants probes the selected episode once; results and failures are
// cached per episode URL.
func (m *Model) ensureVariants() tea.Cmd {
	if m.prov == nil {
		return nil
	}
	selected, ok := m.main.SelectedItem().(item)
	if !ok || selected.episode.URL == "" {
		return nil
	}
	url := selected.episode.URL
	if _, cached := m.variantsByURL[url]; cached {
		return nil
	}
	if m.variantLoading[url] {
		return nil
	}
	m.variantLoading[url] = true
	return variantsCmd(m.prov, selected.episode)
}
