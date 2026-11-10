// Package media downloads and plays provider streams through ffmpeg.
package media

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0 Safari/537.36"

// mpv exits with these codes when the in-player bindings ask for another
// episode; the TUI reads them instead of adding an IPC direction.
const (
	ExitNext = 10
	ExitPrev = 11
)

// ExitCode reports a player's exit code, 0 for a clean exit.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// Progress is one ffmpeg progress sample. Bytes and Speed may be zero when
// ffmpeg has not reported them yet.
type Progress struct {
	Percent float64
	Bytes   int64
	Rate    float64 // bytes per second, measured from consecutive samples
	Speed   float64 // multiples of realtime, as reported by ffmpeg
	ETA     time.Duration
}

// Download writes src to dest using ffmpeg. It writes dest.part first so an
// interrupted download never masquerades as a complete episode. quality is
// forwarded to SelectQuality; a failed pick keeps the master playlist.
func Download(ctx context.Context, src provider.Stream, dest string, quality string, onProgress func(Progress)) error {
	if selected, err := SelectQuality(ctx, nil, src, quality); err == nil {
		src = selected
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	defer os.Remove(part)

	// ffprobe can take seconds on HLS; do not hold up ffmpeg for it. Percent
	// starts reporting as soon as the probe finishes.
	var durationBits atomic.Uint64
	go func() {
		if seconds := probeDuration(ctx, src); seconds > 0 {
			durationBits.Store(math.Float64bits(seconds))
		}
	}()
	args := downloadArgs(src, part)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	var posUS, bytesWritten, lastBytes int64
	var speed, rate float64
	var lastAt time.Time
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "out_time_us="):
			if value, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "out_time_us=")), 10, 64); err == nil {
				posUS = value
				now := time.Now()
				if lastAt.IsZero() {
					lastBytes, lastAt = bytesWritten, now
				} else if elapsed := now.Sub(lastAt).Seconds(); elapsed >= 0.5 {
					rate = float64(bytesWritten-lastBytes) / elapsed
					lastBytes, lastAt = bytesWritten, now
				}
				if onProgress != nil {
					duration := math.Float64frombits(durationBits.Load())
					sample := progressSample(duration, posUS, bytesWritten, speed)
					sample.Rate = rate
					onProgress(sample)
				}
			}
		case strings.HasPrefix(line, "total_size="):
			if value, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "total_size=")), 10, 64); err == nil {
				bytesWritten = value
			}
		case strings.HasPrefix(line, "speed="):
			if value, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "speed=")), "x"), 64); err == nil {
				speed = value
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return os.Rename(part, dest)
}

func progressSample(duration float64, posUS, bytesWritten int64, speed float64) Progress {
	p := Progress{Bytes: bytesWritten, Speed: speed}
	if duration <= 0 {
		return p
	}
	position := float64(posUS) / 1e6
	p.Percent = math.Max(0, math.Min(1, position/duration))
	if speed > 0 && position < duration {
		p.ETA = time.Duration((duration - position) / speed * float64(time.Second))
	}
	return p
}

// DownloadSidecar saves the default subtitle next to a downloaded video as
// "<video>.vtt", which mpv and VLC auto-load.
func DownloadSidecar(ctx context.Context, src provider.Stream, videoPath string) (string, error) {
	sub, ok := defaultSubtitle(src.Subtitles)
	if !ok {
		return "", nil
	}
	dest := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".vtt"
	if err := fetchSubtitle(ctx, sub, src.Referer, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Player is an external player started without taking over the terminal.
// Cleanup must be called after the player exits.
type Player struct {
	Cmd     *exec.Cmd
	socket  string
	dir     string
	started bool
}

// Start launches the player detached from the terminal; ctx cancels it.
func (p *Player) Start(ctx context.Context) error {
	if p == nil || p.Cmd == nil {
		return errors.New("no player command")
	}
	if p.started {
		return errors.New("player already started")
	}
	// CommandContext kills the player when ctx is done, so quitting the TUI
	// closes the window too.
	cmd := exec.CommandContext(ctx, p.Cmd.Path, p.Cmd.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	p.Cmd, p.started = cmd, true
	return nil
}

// Wait blocks until the player exits.
func (p *Player) Wait() error {
	if p == nil || !p.started {
		return errors.New("player not started")
	}
	return p.Cmd.Wait()
}

func (p *Player) Cleanup() {
	if p.dir != "" {
		os.RemoveAll(p.dir)
	}
}

// Play starts mpv when available, otherwise ffplay. start is a resume position
// in seconds. Only mpv exposes live progress and external subtitle files.
// quality is forwarded to SelectQuality; a failed pick keeps the master playlist.
func Play(ctx context.Context, src provider.Stream, title string, start float64, quality string) (*Player, error) {
	if selected, err := SelectQuality(ctx, nil, src, quality); err == nil {
		src = selected
	}
	dir, socket, err := playerDir()
	if err != nil {
		return nil, err
	}
	subArg := ""
	if sub, ok := defaultSubtitle(src.Subtitles); ok {
		subPath := filepath.Join(dir, "sub.vtt")
		fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = fetchSubtitle(fetchCtx, sub, src.Referer, subPath)
		cancel()
		if err == nil {
			subArg = subPath
		} else {
			// mpv receives the Referer globally, so it can fetch the URL itself.
			subArg = sub.URL
		}
	}

	if _, err := exec.LookPath("mpv"); err == nil {
		return &Player{Cmd: exec.Command("mpv", append(mpvPlayArgs(title, start, src.Referer, subArg, socket, dir), src.URL)...), socket: socket, dir: dir}, nil
	}
	if _, err := exec.LookPath("ffplay"); err == nil {
		args := ffplayArgs(title, start, src)
		return &Player{Cmd: exec.Command("ffplay", append(args, src.URL)...), dir: dir}, nil
	}
	os.RemoveAll(dir)
	return nil, errors.New("install mpv for controls and resume, or ffplay for basic playback")
}

// PlayLocal streams a downloaded file. A matching "<video>.vtt" sidecar is
// attached automatically. No Referer or HLS options are needed.
func PlayLocal(path, title string, start float64) (*Player, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	subArg := strings.TrimSuffix(path, filepath.Ext(path)) + ".vtt"
	if _, err := os.Stat(subArg); err != nil {
		subArg = ""
	}
	if _, err := exec.LookPath("mpv"); err == nil {
		dir, socket, err := playerDir()
		if err != nil {
			return nil, err
		}
		args := mpvPlayArgs(title, start, "", subArg, socket, dir)
		return &Player{Cmd: exec.Command("mpv", append(args, path)...), socket: socket, dir: dir}, nil
	}
	if _, err := exec.LookPath("ffplay"); err == nil {
		return &Player{Cmd: exec.Command("ffplay", append(ffplayArgs(title, start, provider.Stream{}), path)...)}, nil
	}
	return nil, errors.New("install mpv for controls and resume, or ffplay for basic playback")
}

func playerDir() (dir, socket string, err error) {
	dir, err = os.MkdirTemp("", "weeb-box-")
	if err != nil {
		return "", "", err
	}
	if runtime.GOOS != "windows" {
		socket = filepath.Join(dir, "mpv.sock")
	}
	return dir, socket, nil
}

// mpvPlayArgs builds the mpv command line and adds the per-play input bindings.
func mpvPlayArgs(title string, start float64, referer, subFile, socket, dir string) []string {
	args := mpvArgs(title, start, referer, subFile, socket)
	if conf, err := writeInputConf(dir); err == nil {
		args = append(args, "--input-conf="+conf)
	}
	return args
}

// writeInputConf writes the next/prev bindings into dir, keeping the user's own
// bindings first. mpv's quit <code> is how the TUI learns which way to go.
func writeInputConf(dir string) (string, error) {
	path := filepath.Join(dir, "input.conf")
	body := "> quit 10\n< quit 11\nn quit 10\np quit 11\n"
	if user, err := os.ReadFile(filepath.Join(MPVConfigDir(), "input.conf")); err == nil && len(user) > 0 {
		body = strings.TrimRight(string(user), "\n") + "\n" + body
	}
	return path, os.WriteFile(path, []byte(body), 0o644)
}

func mpvArgs(title string, start float64, referer, subFile, socket string) []string {
	args := []string{
		"--force-window=yes",
		// The TUI owns the terminal; mpv only drives its own window.
		"--terminal=no",
		"--osd-level=1",
		"--hwdec=auto-safe",
		// Keep only a short forward window; HLS is fetched on demand.
		"--cache=yes",
		"--cache-secs=30",
		"--demuxer-readahead-secs=15",
		"--demuxer-max-bytes=48MiB",
		"--demuxer-max-back-bytes=8MiB",
		// Carbon-styled built-in controls unless uosc replaces them.
		"--osd-font=IBM Plex Mono",
		"--osd-font-size=32",
		"--osd-color=#f4f4f4",
		"--osd-border-color=#161616",
		"--osd-shadow-offset=0",
		"--osd-bar-align-y=0.9",
		"--osd-bar-w=90",
		"--osd-bar-h=3",
		"--sub-font=IBM Plex Mono",
		"--sub-font-size=42",
		"--sub-color=#f4f4f4",
		"--sub-border-color=#161616",
		"--sub-border-size=2.2",
		"--sub-shadow-offset=0",
		"--title=" + title,
		// uosc's top bar reads media-title; without this it shows master.m3u8.
		"--force-media-title=" + title,
	}
	if UOSCInstalled() {
		args = append(args, "--osc=no")
	} else {
		args = append(args, "--osc=yes")
	}
	if referer != "" {
		args = append(args, "--http-header-fields=Referer: "+referer)
	}
	if start > 1 {
		args = append(args, fmt.Sprintf("--start=%.3f", start))
	}
	if subFile != "" {
		args = append(args, "--sub-file="+subFile)
	}
	if socket != "" {
		args = append(args, "--input-ipc-server="+socket)
	}
	return args
}

func ffplayArgs(title string, start float64, src provider.Stream) []string {
	// ffplay has keyboard controls but no OSC; put the controls in the title.
	windowTitle := "weeb-box · " + title + " · space pause · ←/→ seek · f fullscreen"
	args := []string{"-autoexit", "-loglevel", "error", "-window_title", windowTitle}
	if start > 1 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", start))
	}
	if src.URL != "" {
		args = append(args, inputArgs(src)...)
	}
	return args
}

// WatchProgress polls mpv for its position and reports it until ctx is done or
// the player exits. It is a no-op for ffplay, which has no IPC.
func (p *Player) WatchProgress(ctx context.Context, onUpdate func(position, duration float64)) {
	if p.socket == "" {
		return
	}
	client, err := dialIPC(ctx, p.socket)
	if err != nil {
		return
	}
	defer client.conn.Close()

	duration, _, _ := client.get("duration")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if duration <= 0 {
				// mpv only reports duration once the file has loaded.
				if d, ok, err := client.get("duration"); err == nil && ok {
					duration = d
				}
			}
			position, ok, err := client.get("time-pos")
			if err != nil || !ok {
				return
			}
			onUpdate(position, duration)
		}
	}
}

func OpenFolder(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// OpenURL opens a link in the user's default browser.
func OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// start is a cmd builtin and needs the empty title argument.
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func defaultSubtitle(subtitles []provider.Subtitle) (provider.Subtitle, bool) {
	if len(subtitles) == 0 {
		return provider.Subtitle{}, false
	}
	// Prefer English, then the site's default, then the first track. Only this
	// one file is fetched; the rest stay on the server until needed.
	for _, sub := range subtitles {
		if strings.HasPrefix(strings.ToLower(sub.Label), "english") {
			return sub, true
		}
	}
	for _, sub := range subtitles {
		if sub.Default {
			return sub, true
		}
	}
	return subtitles[0], true
}

func fetchSubtitle(ctx context.Context, sub provider.Subtitle, referer, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subtitle: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, io.LimitReader(resp.Body, 10<<20))
	return err
}

func MPVConfigDir() string {
	if home := os.Getenv("MPV_HOME"); home != "" {
		return home
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mpv")
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "mpv")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "mpv")
}

func UOSCInstalled() bool {
	dir := MPVConfigDir()
	if dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "scripts", "uosc"))
	return err == nil && info.IsDir()
}

func UOSCThemeInstalled() bool {
	dir := MPVConfigDir()
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "script-opts", "uosc.conf"))
	return err == nil
}

type ipcClient struct {
	conn net.Conn
	r    *bufio.Reader
	next int
}

func dialIPC(ctx context.Context, path string) (*ipcClient, error) {
	deadline := time.Now().Add(8 * time.Second)
	var lastErr error
	for {
		conn, err := net.Dial("unix", path)
		if err == nil {
			return &ipcClient{conn: conn, r: bufio.NewReader(conn)}, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (c *ipcClient) get(property string) (float64, bool, error) {
	c.next++
	id := c.next
	payload, _ := json.Marshal(map[string]any{
		"command":    []any{"get_property", property},
		"request_id": id,
	})
	c.conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.conn.Write(append(payload, '\n')); err != nil {
		return 0, false, err
	}
	for {
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			return 0, false, err
		}
		var response struct {
			Data      *float64 `json:"data"`
			Error     string   `json:"error"`
			RequestID int      `json:"request_id"`
		}
		if json.Unmarshal(line, &response) != nil || response.RequestID != id {
			continue
		}
		if response.Error != "success" {
			return 0, false, fmt.Errorf("mpv: %s", response.Error)
		}
		if response.Data == nil {
			return 0, false, nil
		}
		return *response.Data, true, nil
	}
}

func probeDuration(ctx context.Context, src provider.Stream) float64 {
	args := []string{"-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1"}
	args = append(args, inputArgs(src)...)
	out, err := exec.CommandContext(ctx, "ffprobe", append(args, src.URL)...).Output()
	if err != nil {
		return 0
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return seconds
}

// downloadArgs builds the ffmpeg command line for a download. Input options
// must precede "-i"; ffmpeg applies them to the input that follows.
func downloadArgs(src provider.Stream, part string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}
	args = append(args, inputArgs(src)...)
	// ".part" has no recognizable extension, so ffmpeg needs the muxer explicitly.
	return append(args, "-i", src.URL, "-c", "copy", "-progress", "pipe:1", "-nostats", "-f", "mp4", part)
}

// inputArgs returns the input options shared by every ffmpeg, ffprobe and
// ffplay call. They must come before the input URL.
func inputArgs(src provider.Stream) []string {
	// gogo streams are HLS whose segments live on rotating hosts with decoy
	// extensions (.jpg/.html); ffmpeg 9 rejects those unless told otherwise.
	args := []string{"-allowed_segment_extensions", "ALL", "-extension_picky", "0"}
	if src.Referer == "" {
		return args
	}
	return append(args, "-headers", "Referer: "+src.Referer+"\r\n")
}
