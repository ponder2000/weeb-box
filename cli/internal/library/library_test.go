package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	root := t.TempDir()
	anime := filepath.Join(root, "gogoanime", "Frieren")
	if err := os.MkdirAll(anime, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(anime, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Frieren - E001.mp4", "video")
	write("Frieren - E001.vtt", "WEBVTT")
	write("Frieren - E002.mkv", "video2")
	write("Frieren - E003.mp4.part", "partial")
	write("notes.txt", "ignore")

	files, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Anime != "Frieren" || files[0].Episode != 1 || !files[0].HasSubs {
		t.Errorf("first = %+v", files[0])
	}
	if files[1].Episode != 2 || files[1].HasSubs {
		t.Errorf("second = %+v", files[1])
	}
	if files[0].Title() != "Frieren — E001" {
		t.Errorf("title = %q", files[0].Title())
	}
}

func TestScanMissingRoot(t *testing.T) {
	files, err := Scan(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(files) != 0 {
		t.Fatalf("files=%v err=%v", files, err)
	}
}
