package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponder2000/weeb-box/internal/deps"
	"github.com/ponder2000/weeb-box/internal/provider/gogo"
	"github.com/ponder2000/weeb-box/internal/store"
	"github.com/ponder2000/weeb-box/internal/tui"
)

func main() {
	baseURL := flag.String("base", "", "gogoanime base URL (default "+gogo.DefaultBaseURL+")")
	outDir := flag.String("dir", "", "download directory (default ~/Downloads/weeb-box)")
	dbPath := flag.String("db", "", "database path (default: platform app-data directory)")
	libraryDir := flag.String("library", "", "local library directory (default: -dir)")
	check := flag.Bool("check", false, "print dependency status and install commands")
	flag.Parse()

	if *check {
		report := deps.Check()
		deps.Print(os.Stdout, report)
		if !report.FFmpeg.Found || !report.MPV.Found {
			os.Exit(1)
		}
		return
	}

	dir := *outDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fail(err)
		}
		dir = filepath.Join(home, "Downloads", "weeb-box")
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		fail(err)
	}
	defer st.Close()

	model := tui.New(gogo.New(*baseURL), dir, st).SetLibrary(*libraryDir)
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := program.Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "weeb-box:", err)
	os.Exit(1)
}
