package media

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider/gogo"
)

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
