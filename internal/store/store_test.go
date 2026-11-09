package store

import (
	"context"
	"path/filepath"
	"testing"
)

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
