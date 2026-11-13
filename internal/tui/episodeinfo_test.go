package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/ponder2000/weeb-box/internal/filler"
	"github.com/ponder2000/weeb-box/internal/metadata"
	"github.com/ponder2000/weeb-box/internal/provider"
)

func episodeModel(t *testing.T, episodes ...provider.Episode) Model {
	m := New([]provider.Provider{stubProvider{}}, t.TempDir(), nil)
	m.prov = stubProvider{}
	m.anime = provider.Anime{Title: "Show", URL: "show"}
	m.episodes = episodes
	items := make([]list.Item, len(episodes))
	for i, ep := range episodes {
		items[i] = item{title: ep.Title, episode: ep}
	}
	m.main.SetItems(items)
	m.screen = screenEpisodes
	return m
}

func TestApplyFillerBadgesAlignedList(t *testing.T) {
	m := episodeModel(t, provider.Episode{Number: 1}, provider.Episode{Number: 2}, provider.Episode{Number: 3})
	m.applyFiller(fillerMsg{list: filler.List{Entries: []filler.Entry{
		{Number: 2, Kind: filler.Filler},
		{Number: 3, Kind: filler.Mixed},
		{Number: 1, Kind: filler.Canon},
	}}})

	if len(m.fillerKinds) != 3 {
		t.Fatalf("kinds = %+v", m.fillerKinds)
	}
	items := m.main.Items()
	if kind, _ := items[1].(item).episode.Number, 0; kind != 2 {
		t.Fatalf("items reordered: %#v", items)
	}
	desc := items[1].(item).desc
	if !strings.Contains(desc, "filler") {
		t.Errorf("episode 2 desc = %q", desc)
	}
	if got := items[2].(item).desc; !strings.Contains(got, "mixed") {
		t.Errorf("episode 3 desc = %q", got)
	}
	if got := items[0].(item).desc; strings.Contains(got, "filler") || strings.Contains(got, "mixed") {
		t.Errorf("canon episode badged: %q", got)
	}
}

func TestApplyFillerRejectsMisalignedNumbering(t *testing.T) {
	m := episodeModel(t, provider.Episode{Number: 1}, provider.Episode{Number: 2})
	// A season-split provider still ends at E2, but the site's list ends at 220.
	m.applyFiller(fillerMsg{list: filler.List{Entries: []filler.Entry{
		{Number: 26, Kind: filler.Filler},
		{Number: 220, Kind: filler.Filler},
	}}})
	if len(m.fillerKinds) != 0 {
		t.Fatalf("misaligned list applied: %+v", m.fillerKinds)
	}

	m.applyFiller(fillerMsg{err: errors.New("cloudflare")})
	if m.fillerLoading {
		t.Error("loading flag not cleared on error")
	}
	if !strings.Contains(m.status, "filler list unavailable") {
		t.Errorf("status = %q", m.status)
	}
}

func TestDetailShowsDurationQualityAndFiller(t *testing.T) {
	m := episodeModel(t, provider.Episode{Number: 2, URL: "ep2", Title: "Second"})
	m.width, m.height = 120, 40
	m.metaCache[metadata.Key("Show")] = metadata.Info{Genres: []string{"Action"}, Duration: 24}
	m.fillerKinds[2] = filler.Filler
	m.variantsByURL["ep2"] = []string{"1080p", "720p", "480p"}

	view := m.detailView(m.episodes[0], 44)
	for _, want := range []string{"24m per episode", "quality 1080p · 720p · 480p", "FILLER EPISODE"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail missing %q:\n%s", want, view)
		}
	}

	// Canon episodes get no filler banner, and a failed probe shows no quality.
	m.fillerKinds[2] = filler.Canon
	m.variantsByURL["ep2"] = nil
	view = m.detailView(m.episodes[0], 44)
	if strings.Contains(view, "FILLER EPISODE") || strings.Contains(view, "quality") {
		t.Errorf("unexpected lines:\n%s", view)
	}

	// While the probe is in flight the pane says so.
	delete(m.variantsByURL, "ep2")
	m.variantLoading["ep2"] = true
	if view := m.detailView(m.episodes[0], 44); !strings.Contains(view, "checking quality") {
		t.Errorf("missing loading hint:\n%s", view)
	}
}

func TestEnsureVariantsProbesOnce(t *testing.T) {
	m := episodeModel(t, provider.Episode{Number: 1, URL: "ep1"})
	m.main.Select(0)

	if cmd := m.ensureVariants(); cmd == nil {
		t.Fatal("no probe command")
	}
	if !m.variantLoading["ep1"] {
		t.Error("probe not marked in flight")
	}
	if cmd := m.ensureVariants(); cmd != nil {
		t.Error("second probe fired while the first is in flight")
	}

	// A finished probe, success or failure, is cached.
	updated, _ := m.Update(variantsMsg{url: "ep1", list: []string{"720p"}})
	m = updated.(Model)
	if got := m.variantsByURL["ep1"]; len(got) != 1 || got[0] != "720p" {
		t.Fatalf("variants = %v", got)
	}
	if cmd := m.ensureVariants(); cmd != nil {
		t.Error("cached episode probed again")
	}

	updated, _ = m.Update(variantsMsg{url: "ep1", err: errors.New("410")})
	m = updated.(Model)
	if _, ok := m.variantsByURL["ep1"]; !ok {
		t.Error("failed probe not cached")
	}
	if m.variantLoading["ep1"] {
		t.Error("loading flag left set after a failure")
	}
}

func TestDetailFillerOnlyWhenNotCanon(t *testing.T) {
	m := episodeModel(t, provider.Episode{Number: 5, URL: "ep5"})
	m.fillerKinds[5] = filler.Mixed
	if view := m.detailView(m.episodes[0], 40); !strings.Contains(view, "MIXED EPISODE") {
		t.Errorf("mixed episode not labelled:\n%s", view)
	}
}

func TestFillerRetriesWithAnilistTitle(t *testing.T) {
	m := episodeModel(t, provider.Episode{Number: 1}, provider.Episode{Number: 2})
	m.anime = provider.Anime{Title: "Show (Dub)", URL: "show"}
	m.fillerTitle = "Show (Dub)"
	m.metaCache = map[string]metadata.Info{}
	m.screen = screenEpisodes

	// AniList knows the clean title, so a second lookup is worth one request.
	updated, cmd := m.Update(metaMsg{key: metadata.Key("Show (Dub)"), info: metadata.Info{Title: "Show"}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("no retry command")
	}
	if m.fillerTitle != "Show" || !m.fillerLoading {
		t.Errorf("fillerTitle = %q loading = %v", m.fillerTitle, m.fillerLoading)
	}

	// Arriving mid-flight, the clean title is remembered for after the failure.
	m.fillerLoading = true
	m.fillerKinds = map[int]filler.Kind{}
	m.fillerRetry = ""
	updated, _ = m.Update(metaMsg{key: metadata.Key("Show (Dub)"), info: metadata.Info{Title: "Show Again"}})
	m = updated.(Model)
	if m.fillerRetry != "Show Again" {
		t.Errorf("fillerRetry = %q", m.fillerRetry)
	}
	retry := m.applyFiller(fillerMsg{title: "Show (Dub)", err: errors.New("not found")})
	if retry == nil {
		t.Fatal("no retry after the provider-title lookup failed")
	}
	if m.fillerTitle != "Show Again" {
		t.Errorf("retry title = %q", m.fillerTitle)
	}
}
