package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestProviderStates(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.MarkProviderOK(ctx, "gogoanime"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProviderEnabled(ctx, "anizone", false); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkProviderError(ctx, "kissanime", "boom"); err != nil {
		t.Fatal(err)
	}
	states, err := st.ProviderStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !states["gogoanime"].Enabled || states["gogoanime"].LastOK == 0 {
		t.Fatalf("gogo = %+v", states["gogoanime"])
	}
	if states["anizone"].Enabled {
		t.Fatalf("anizone = %+v", states["anizone"])
	}
	if states["kissanime"].LastError != "boom" {
		t.Fatalf("kissanime = %+v", states["kissanime"])
	}
	// Marking a provider OK must not re-enable a disabled one.
	if err := st.MarkProviderOK(ctx, "anizone"); err != nil {
		t.Fatal(err)
	}
	states, err = st.ProviderStates(ctx)
	if err != nil || states["anizone"].Enabled {
		t.Fatalf("anizone after mark = %+v err=%v", states["anizone"], err)
	}
}

func TestSearchProgressAndDownloads(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.AddSearch(ctx, "frieren"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSearch(ctx, "one piece"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSearch(ctx, "frieren"); err != nil {
		t.Fatal(err)
	}
	searches, err := st.RecentSearches(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(searches) != 2 || searches[0].Query != "frieren" || searches[0].Uses != 2 {
		t.Fatalf("searches = %+v", searches)
	}

	episode := Progress{
		Provider: "gogoanime", EpisodeURL: "https://x/ep1", AnimeURL: "https://x/anime",
		AnimeTitle: "Frieren", EpisodeNumber: 1, Position: 120, Duration: 1440,
	}
	if err := st.SaveProgress(ctx, episode); err != nil {
		t.Fatal(err)
	}
	episode.Position = 300
	if err := st.SaveProgress(ctx, episode); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.Progress(ctx, "gogoanime", "https://x/ep1")
	if err != nil || !ok || got.Position != 300 {
		t.Fatalf("progress = %+v ok=%v err=%v", got, ok, err)
	}
	progress, err := st.ProgressForAnime(ctx, "gogoanime", "https://x/anime")
	if err != nil || len(progress) != 1 {
		t.Fatalf("anime progress = %+v err=%v", progress, err)
	}
	if err := st.SaveProgress(ctx, Progress{
		Provider: "local", EpisodeURL: "/tmp/ep1.mp4", AnimeURL: "/tmp/ep1.mp4",
		AnimeTitle: "Frieren", EpisodeNumber: 1, Position: 10, Duration: 1440,
	}); err != nil {
		t.Fatal(err)
	}
	local, err := st.ProgressForProvider(ctx, "local")
	if err != nil || len(local) != 1 || local["/tmp/ep1.mp4"].Position != 10 {
		t.Fatalf("local progress = %+v err=%v", local, err)
	}

	if err := st.RecordDownload(ctx, Download{
		Provider: "gogoanime", EpisodeURL: "https://x/ep1", AnimeURL: "https://x/anime",
		AnimeTitle: "Frieren", EpisodeNumber: 1, Path: "/tmp/ep1.mp4", Bytes: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordDownload(ctx, Download{
		Provider: "gogoanime", EpisodeURL: "https://x/ep1", AnimeURL: "https://x/anime",
		AnimeTitle: "Frieren", EpisodeNumber: 1, Path: "/tmp/ep1.mp4", Bytes: 2000,
	}); err != nil {
		t.Fatal(err)
	}
	downloads, err := st.DownloadsForAnime(ctx, "gogoanime", "https://x/anime")
	if err != nil || len(downloads) != 1 || downloads[1].Bytes != 2000 {
		t.Fatalf("downloads = %+v err=%v", downloads, err)
	}
	stats, err := st.Stats(ctx)
	if err != nil || stats.Downloads != 1 || stats.Bytes != 2000 || stats.Watched != 2 {
		t.Fatalf("stats = %+v err=%v", stats, err)
	}
}

func TestRecentProgressGroupsAnime(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	save := func(provider, episodeURL, animeURL, title string, episode int, position float64) {
		t.Helper()
		if err := st.SaveProgress(ctx, Progress{
			Provider: provider, EpisodeURL: episodeURL,
			AnimeURL: animeURL, AnimeTitle: title,
			EpisodeNumber: episode, Position: position, Duration: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for ep := 1; ep <= 3; ep++ {
		save("gogoanime", "https://x/frieren-e"+string(rune('0'+ep)), "https://x/frieren", "Frieren", ep, float64(ep*10))
	}
	save("gogoanime", "https://x/other-e1", "https://x/other", "Other", 1, 5)
	save("local", "/tmp/frieren-e1.mp4", "/tmp/frieren-e1.mp4", "Frieren", 1, 10)
	save("local", "/tmp/frieren-e2.mp4", "/tmp/frieren-e2.mp4", "Frieren", 2, 20)

	got, err := st.RecentProgress(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("groups = %d: %+v", len(got), got)
	}
	groups := map[string]Progress{}
	for _, p := range got {
		groups[p.Provider+"|"+p.AnimeTitle] = p
	}
	if g := groups["gogoanime|Frieren"]; g.Episodes != 3 || g.EpisodeNumber != 3 || g.Position != 30 {
		t.Errorf("gogo group = %+v", g)
	}
	if g := groups["local|Frieren"]; g.Episodes != 2 || g.EpisodeNumber != 2 || g.EpisodeURL != "/tmp/frieren-e2.mp4" {
		t.Errorf("local group = %+v", g)
	}
	if g := groups["gogoanime|Other"]; g.Episodes != 1 {
		t.Errorf("other group = %+v", g)
	}
}
