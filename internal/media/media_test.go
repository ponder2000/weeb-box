package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider"
)

func TestDownloadArgsOrdering(t *testing.T) {
	src := provider.Stream{URL: "https://cdn.example/master.m3u8", Referer: "https://megaplay.buzz/"}
	args := downloadArgs(src, "/tmp/x.mp4.part")

	i := slices.Index(args, "-i")
	if i < 0 {
		t.Fatalf("no -i in %v", args)
	}
	for _, opt := range []string{"-allowed_segment_extensions", "-extension_picky", "-headers"} {
		if idx := slices.Index(args, opt); idx > i {
			t.Errorf("%s must come before -i", opt)
		}
	}
	if args[i+1] != src.URL {
		t.Errorf("input after -i = %q, want %q", args[i+1], src.URL)
	}
	if args[len(args)-1] != "/tmp/x.mp4.part" {
		t.Errorf("output = %q", args[len(args)-1])
	}
	if f := slices.Index(args, "-f"); f < 0 || f+1 >= len(args) || args[f+1] != "mp4" {
		t.Errorf("missing explicit output muxer: %v", args)
	}
}

func TestInputArgsWithoutReferer(t *testing.T) {
	args := inputArgs(provider.Stream{})
	if len(args) != 4 || args[0] != "-allowed_segment_extensions" || slices.Contains(args, "-headers") {
		t.Errorf("inputArgs = %v", args)
	}
}

func TestDownloadSidecar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("missing referer")
		}
		_, _ = w.Write([]byte("WEBVTT\n\n00:00.000 --> 00:01.000\nhello\n"))
	}))
	defer server.Close()

	video := filepath.Join(t.TempDir(), "Anime - E001.mp4")
	sub := provider.Subtitle{URL: server.URL + "/sub.vtt", Label: "English", Default: true}
	path, err := DownloadSidecar(context.Background(), provider.Stream{
		Referer: "https://megaplay.buzz/", Subtitles: []provider.Subtitle{sub},
	}, video)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSuffix(video, ".mp4") + ".vtt"; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.HasPrefix(string(data), "WEBVTT") {
		t.Fatalf("subtitle = %q err=%v", data, err)
	}
}

// Downloads a generated HLS stream through the real ffmpeg path so the .part
// muxer, progress parsing, and rename are covered.
func TestDownloadLocalHLS(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=160x90:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-c:v", "libx264", "-c:a", "aac", "-shortest",
		"-f", "hls", "-hls_time", "1", "-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(dir, "seg%03d.ts"),
		filepath.Join(dir, "index.m3u8"))
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate HLS: %v %s", err, out)
	}

	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "out.mp4")
	var maxBytes int64
	err := Download(context.Background(), provider.Stream{URL: server.URL + "/index.m3u8"}, dest, "", func(p Progress) {
		if p.Bytes > maxBytes {
			maxBytes = p.Bytes
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil || info.Size() == 0 {
		t.Fatalf("output: %v size=%d", err, info.Size())
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("part file still exists")
	}
	if maxBytes <= 0 {
		t.Fatalf("no bytes reported")
	}
}

func TestDefaultSubtitlePrefersEnglish(t *testing.T) {
	sub, ok := defaultSubtitle([]provider.Subtitle{
		{Label: "Arabic", Default: true},
		{Label: "English"},
	})
	if !ok || sub.Label != "English" {
		t.Fatalf("sub = %+v ok=%v", sub, ok)
	}
}

func TestMPVArgsLocal(t *testing.T) {
	args := strings.Join(mpvArgs("Frieren — E001", 12, "", "/tmp/ep.vtt", "/tmp/mpv.sock"), " ")
	for _, want := range []string{
		"--terminal=no",
		"--force-window=yes",
		"--force-media-title=Frieren — E001",
		"--sub-file=/tmp/ep.vtt",
		"--start=12.000",
		"--input-ipc-server=/tmp/mpv.sock",
		"--cache-secs=30",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %s", want, args)
		}
	}
}

func TestPlayLocalMissing(t *testing.T) {
	if _, err := PlayLocal(filepath.Join(t.TempDir(), "missing.mp4"), "missing", 0); err == nil {
		t.Fatal("expected an error")
	}
}

// A Player launches without mpv so the start/wait lifecycle stays covered.
func TestPlayerStartWait(t *testing.T) {
	p := &Player{Cmd: exec.Command("sh", "-c", "exit 0")}
	if err := p.Wait(); err == nil {
		t.Fatal("Wait before Start should fail")
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("second Start should fail")
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait = %v", err)
	}
}

func TestPlayerStartCancel(t *testing.T) {
	p := &Player{Cmd: exec.Command("sh", "-c", "sleep 10")}
	ctx, cancel := context.WithCancel(context.Background())
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled player exited cleanly")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not kill the player")
	}
}

// Feeds the real mpv parser the exact args used by the app, with output
// disabled, so an invalid OSD/subtitle/cache option fails the test.
func TestMPVArgsAccepted(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "v.mp4")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=160x90:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-c:a", "aac", "-shortest", video)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate video: %v %s", err, out)
	}
	sub := filepath.Join(dir, "v.vtt")
	if err := os.WriteFile(sub, []byte("WEBVTT\n\n00:00.000 --> 00:01.000\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--no-config"}, mpvArgs("weeb-box — E001", 0, "", sub, "")...)
	args = append(args, "--vo=null", "--ao=null", "--force-window=no", "--really-quiet", video)
	if out, err := exec.Command("mpv", args...).CombinedOutput(); err != nil {
		t.Fatalf("mpv rejected args: %v\n%s", err, out)
	}
}

func TestInputConfBindings(t *testing.T) {
	t.Setenv("MPV_HOME", t.TempDir()) // keep the real user config out of the test
	dir := t.TempDir()
	path, err := writeInputConf(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"> quit 10", "< quit 11", "n quit 10", "p quit 11"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in %q", want, data)
		}
	}
}

func TestInputConfKeepsUserBindings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MPV_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "input.conf"), []byte("q quit\nUP seek 60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := writeInputConf(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "q quit\nUP seek 60\n") {
		t.Errorf("user bindings not preserved: %q", data)
	}
	if !strings.Contains(string(data), "n quit 10") {
		t.Errorf("missing next binding: %q", data)
	}
}

func TestMPVPlayArgsIncludeInputConf(t *testing.T) {
	t.Setenv("MPV_HOME", t.TempDir())
	dir := t.TempDir()
	args := mpvPlayArgs("weeb-box — E001", 0, "", "", "", dir)
	want := "--input-conf=" + filepath.Join(dir, "input.conf")
	if !slices.Contains(args, want) {
		t.Fatalf("args = %v, want %s", args, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "input.conf")); err != nil {
		t.Fatal(err)
	}
}

func TestExitCode(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Errorf("nil err = %d", got)
	}
	for _, code := range []int{ExitNext, ExitPrev, 3} {
		err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
		if got := ExitCode(err); got != code {
			t.Errorf("ExitCode = %d, want %d", got, code)
		}
	}
	if got := ExitCode(errors.New("boom")); got != -1 {
		t.Errorf("non-exit error = %d", got)
	}
}

func TestProgressSample(t *testing.T) {
	p := progressSample(100, 25_000_000, 1_000_000, 2)
	if p.Percent < 0.24 || p.Percent > 0.26 {
		t.Errorf("percent = %v", p.Percent)
	}
	if p.Bytes != 1_000_000 || p.Speed != 2 {
		t.Errorf("progress = %+v", p)
	}
	if p.ETA < 37*time.Second || p.ETA > 38*time.Second {
		t.Errorf("eta = %v", p.ETA)
	}
}
