package deps

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckMissingAndCommands(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	mpvHome := t.TempDir()
	t.Setenv("MPV_HOME", mpvHome)

	report := Check()
	if report.Ready() {
		t.Fatal("expected missing dependencies")
	}
	if report.FFmpeg.Found || report.MPV.Found || report.UOSC.Found || report.Theme.Found {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Commands) == 0 {
		t.Fatal("expected install commands")
	}

	var out bytes.Buffer
	Print(&out, report)
	text := out.String()
	for _, want := range []string{"weeb-box dependency check", "ffmpeg", "mpv", "uosc", "theme"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestCheckInstalledUOSC(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	mpvHome := t.TempDir()
	t.Setenv("MPV_HOME", mpvHome)
	if err := os.MkdirAll(filepath.Join(mpvHome, "scripts", "uosc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mpvHome, "script-opts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mpvHome, "script-opts", "uosc.conf"), []byte("top_bar=always\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report := Check()
	if !report.UOSC.Found || !report.Theme.Found {
		t.Fatalf("report = %+v", report)
	}
	for _, command := range report.Commands {
		if strings.Contains(command, "uosc") {
			t.Fatalf("unexpected uosc install command with uosc present: %s", command)
		}
	}
}
