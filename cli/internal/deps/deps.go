// Package deps reports whether the external playback/download tools are present
// and prints the commands needed to install the missing ones.
package deps

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/ponder2000/weeb-box/internal/media"
)

type Status struct {
	Name   string
	Found  bool
	Detail string
}

type Report struct {
	FFmpeg   Status
	MPV      Status
	UOSC     Status
	Theme    Status
	Commands []string
}

func (r Report) NeedsSetup() bool {
	return !r.FFmpeg.Found || !r.MPV.Found || !r.UOSC.Found || !r.Theme.Found
}

func (r Report) Ready() bool { return !r.NeedsSetup() }

func Check() Report {
	ffmpeg, _ := exec.LookPath("ffmpeg")
	mpv, _ := exec.LookPath("mpv")
	themePath := filepath.Join(media.MPVConfigDir(), "script-opts", "uosc.conf")

	report := Report{
		FFmpeg: Status{Name: "ffmpeg", Found: ffmpeg != "", Detail: fallback(ffmpeg, "not installed")},
		MPV:    Status{Name: "mpv", Found: mpv != "", Detail: fallback(mpv, "not installed")},
		UOSC:   Status{Name: "uosc", Found: media.UOSCInstalled()},
		Theme:  Status{Name: "theme", Found: media.UOSCThemeInstalled(), Detail: themePath},
	}
	if report.UOSC.Found {
		report.UOSC.Detail = filepath.Join(media.MPVConfigDir(), "scripts", "uosc")
	} else {
		report.UOSC.Detail = "not installed"
	}
	report.Commands = commands(report)
	return report
}

func Print(w io.Writer, report Report) {
	fmt.Fprintln(w, "weeb-box dependency check")
	fmt.Fprintln(w)
	for _, status := range []Status{report.FFmpeg, report.MPV, report.UOSC, report.Theme} {
		mark := "x"
		if status.Found {
			mark = "ok"
		}
		fmt.Fprintf(w, "  %-2s %-8s %s\n", mark, status.Name, status.Detail)
	}
	if len(report.Commands) > 0 {
		fmt.Fprintln(w, "\nInstall the missing pieces:")
		for _, command := range report.Commands {
			fmt.Fprintf(w, "  %s\n", command)
		}
	}
}

func commands(report Report) []string {
	var out []string
	if !report.FFmpeg.Found || !report.MPV.Found {
		switch runtime.GOOS {
		case "darwin":
			out = append(out, "brew install ffmpeg mpv")
		case "windows":
			out = append(out, "scoop install ffmpeg mpv")
		default:
			out = append(out, "sudo apt install ffmpeg mpv")
		}
	}
	if !report.UOSC.Found {
		if runtime.GOOS == "windows" {
			out = append(out, `powershell -c "irm https://raw.githubusercontent.com/tomasklaen/uosc/HEAD/installers/windows.ps1 | iex"`)
		} else {
			out = append(out, `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/tomasklaen/uosc/HEAD/installers/unix.sh)"`)
		}
	}
	if !report.Theme.Found {
		if runtime.GOOS == "windows" {
			out = append(out,
				`New-Item -ItemType Directory -Force -Path "$env:APPDATA\mpv\script-opts"`,
				fmt.Sprintf(`Copy-Item %q "$env:APPDATA\mpv\script-opts\uosc.conf"`, themeSource()),
			)
		} else {
			configDir := media.MPVConfigDir()
			out = append(out,
				fmt.Sprintf("mkdir -p %q", filepath.Join(configDir, "script-opts")),
				fmt.Sprintf("cp %q %q", themeSource(), filepath.Join(configDir, "script-opts", "uosc.conf")),
			)
		}
	}
	return out
}

func themeSource() string {
	if cwd, err := os.Getwd(); err == nil {
		path := filepath.Join(cwd, "themes", "mpv", "uosc.conf")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "themes/mpv/uosc.conf"
}

func fallback(value, empty string) string {
	if value == "" {
		return empty
	}
	return value
}
