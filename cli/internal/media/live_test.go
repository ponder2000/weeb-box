package media

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider/gogo"
)

func TestLiveQuality(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adapter := gogo.New("")
	animes, err := adapter.Search(ctx, "psyren")
	if err != nil || len(animes) == 0 {
		t.Fatalf("search: %v", err)
	}
	episodes, err := adapter.Episodes(ctx, animes[0])
	if err != nil || len(episodes) == 0 {
		t.Fatalf("episodes: %v", err)
	}
	stream, err := adapter.Stream(ctx, episodes[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, quality := range []string{"720p", "auto"} {
		selected, err := SelectQuality(ctx, http.DefaultClient, stream, quality)
		if err != nil {
			t.Fatalf("%s: %v", quality, err)
		}
		if selected.URL == stream.URL {
			t.Fatalf("%s did not select a variant", quality)
		}
		if len(selected.Subtitles) != len(stream.Subtitles) || selected.Referer != stream.Referer {
			t.Fatalf("%s lost metadata: %+v", quality, selected)
		}
		t.Logf("%s -> %s", quality, selected.URL)
	}
}

func TestLiveVariants(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adapter := gogo.New("")
	animes, err := adapter.Search(ctx, "psyren")
	if err != nil || len(animes) == 0 {
		t.Skipf("search unavailable: %v", err)
	}
	episodes, err := adapter.Episodes(ctx, animes[0])
	if err != nil || len(episodes) == 0 {
		t.Skipf("episodes unavailable: %v", err)
	}
	stream, err := adapter.Stream(ctx, episodes[0])
	if err != nil {
		t.Skipf("stream unavailable: %v", err)
	}
	variants, err := Variants(ctx, http.DefaultClient, stream)
	if err != nil {
		t.Skipf("variants unavailable: %v", err)
	}
	if len(variants) == 0 {
		t.Fatal("no variants")
	}
	if !slices.Contains(variants, "1080p") && !slices.Contains(variants, "720p") {
		t.Fatalf("variants = %v", variants)
	}
	t.Logf("variants: %v", variants)
}

func TestLiveDownload(t *testing.T) {
	if os.Getenv("WEEB_LIVE") == "" {
		t.Skip("set WEEB_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	adapter := gogo.New("")
	animes, err := adapter.Search(ctx, "psyren")
	if err != nil || len(animes) == 0 {
		t.Fatalf("search: %v", err)
	}
	episodes, err := adapter.Episodes(ctx, animes[0])
	if err != nil || len(episodes) == 0 {
		t.Fatalf("episodes: %v", err)
	}
	stream, err := adapter.Stream(ctx, episodes[0])
	if err != nil {
		t.Fatal(err)
	}

	dst := "/tmp/weeb-live-probe.mp4.part"
	_ = os.Remove(dst)
	defer os.Remove(dst)

	args := downloadArgs(stream, dst)
	if i := slices.Index(args, "-f"); i > 0 {
		args = append(args[:i], append([]string{"-t", "5"}, args[i:]...)...)
	}
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	info, err := os.Stat(dst)
	if err != nil || info.Size() == 0 {
		t.Fatalf("no bytes: %v", err)
	}
	t.Logf("wrote %d bytes to %s", info.Size(), dst)

	video := "/tmp/weeb-live-probe.mp4"
	subPath, err := DownloadSidecar(ctx, stream, video)
	if err != nil {
		t.Fatalf("subtitle: %v", err)
	}
	defer os.Remove(subPath)
	data, err := os.ReadFile(subPath)
	if err != nil || !strings.HasPrefix(string(data), "WEBVTT") {
		t.Fatalf("subtitle content: %q err=%v", string(data), err)
	}
	t.Logf("wrote %d bytes to %s", len(data), subPath)
}
