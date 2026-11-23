// Package library discovers downloaded video files on disk.
package library

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type File struct {
	Path    string
	Name    string
	Anime   string
	Episode int
	Size    int64
	ModTime time.Time
	HasSubs bool
}

var (
	episodeName = regexp.MustCompile(`^(.*) - E(\d+)\.([A-Za-z0-9]+)$`)
	videoExt    = map[string]bool{
		".mp4": true, ".mkv": true, ".webm": true, ".avi": true,
		".mov": true, ".m4v": true, ".ts": true,
	}
)

// Scan walks root for video files. A missing root is an empty library, not an
// error. Files named "<anime> - E001.mp4" keep their episode number; anything
// else falls back to the filename.
func Scan(root string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !videoExt[ext] {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		base := filepath.Base(path)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		anime := filepath.Base(filepath.Dir(path))
		episode := 0
		if match := episodeName.FindStringSubmatch(base); len(match) == 4 {
			anime = strings.TrimSpace(match[1])
			if n, err := strconv.Atoi(match[2]); err == nil {
				episode = n
			}
		}
		_, subErr := os.Stat(strings.TrimSuffix(path, filepath.Ext(path)) + ".vtt")
		files = append(files, File{
			Path: path, Name: name, Anime: anime, Episode: episode,
			Size: info.Size(), ModTime: info.ModTime(), HasSubs: subErr == nil,
		})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Anime != files[j].Anime {
			return files[i].Anime < files[j].Anime
		}
		if files[i].Episode != files[j].Episode {
			return files[i].Episode < files[j].Episode
		}
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func (f File) Title() string {
	if f.Episode > 0 {
		return fmt.Sprintf("%s — E%03d", f.Anime, f.Episode)
	}
	return f.Name
}
