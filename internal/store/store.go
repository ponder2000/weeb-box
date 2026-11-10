// Package store persists search history, playback progress, and the download index.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

type Search struct {
	Query    string
	LastUsed int64
	Uses     int
}

type Progress struct {
	Provider      string
	EpisodeURL    string
	AnimeURL      string
	AnimeTitle    string
	EpisodeNumber int
	Position      float64
	Duration      float64
	UpdatedAt     int64
	Episodes      int // recorded episodes for the anime; RecentProgress only
}

type Download struct {
	Provider      string
	EpisodeURL    string
	AnimeURL      string
	AnimeTitle    string
	EpisodeNumber int
	Path          string
	Bytes         int64
	CompletedAt   int64
}

type Stats struct {
	Downloads int
	Bytes     int64
	Watched   int
}

type ProviderState struct {
	Name      string
	Enabled   bool
	LastOK    int64
	LastError string
}

const schema = `
CREATE TABLE IF NOT EXISTS search_history (
	query     TEXT PRIMARY KEY,
	last_used INTEGER NOT NULL,
	uses      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_search_recent ON search_history(last_used DESC);

CREATE TABLE IF NOT EXISTS watch_progress (
	provider       TEXT NOT NULL,
	episode_url    TEXT NOT NULL,
	anime_url      TEXT NOT NULL,
	anime_title    TEXT NOT NULL,
	episode_number INTEGER NOT NULL,
	position       REAL NOT NULL DEFAULT 0,
	duration       REAL NOT NULL DEFAULT 0,
	updated_at     INTEGER NOT NULL,
	PRIMARY KEY (provider, episode_url)
);
CREATE INDEX IF NOT EXISTS idx_progress_recent ON watch_progress(updated_at DESC);

CREATE TABLE IF NOT EXISTS downloads (
	provider       TEXT NOT NULL,
	episode_url    TEXT NOT NULL,
	anime_url      TEXT NOT NULL,
	anime_title    TEXT NOT NULL,
	episode_number INTEGER NOT NULL,
	path           TEXT NOT NULL,
	bytes          INTEGER NOT NULL DEFAULT 0,
	completed_at   INTEGER NOT NULL,
	PRIMARY KEY (provider, episode_url)
);
CREATE INDEX IF NOT EXISTS idx_downloads_recent ON downloads(completed_at DESC);

CREATE TABLE IF NOT EXISTS provider_state (
	name       TEXT PRIMARY KEY,
	enabled    INTEGER NOT NULL DEFAULT 1,
	last_ok    INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS anime_meta (
	key        TEXT PRIMARY KEY,
	payload    TEXT NOT NULL,
	fetched_at INTEGER NOT NULL
);
`

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var dir string
	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(home, "Library", "Application Support", "weeb-box")
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		dir = filepath.Join(base, "weeb-box")
	default:
		if base := os.Getenv("XDG_DATA_HOME"); base != "" {
			dir = filepath.Join(base, "weeb-box")
		} else {
			dir = filepath.Join(home, ".local", "share", "weeb-box")
		}
	}
	return filepath.Join(dir, "weeb-box.db"), nil
}

func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Path() string { return s.path }

func (s *Store) AddSearch(ctx context.Context, query string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO search_history(query, last_used, uses) VALUES(?, ?, 1)
		ON CONFLICT(query) DO UPDATE SET last_used=excluded.last_used, uses=uses+1`,
		query, time.Now().Unix())
	return err
}

func (s *Store) RecentSearches(ctx context.Context, limit int) ([]Search, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT query, last_used, uses FROM search_history ORDER BY last_used DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Search
	for rows.Next() {
		var item Search
		if err := rows.Scan(&item.Query, &item.LastUsed, &item.Uses); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ClearSearches(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM search_history`)
	return err
}

// DeleteSearch removes one saved query.
func (s *Store) DeleteSearch(ctx context.Context, query string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM search_history WHERE query = ?`, query)
	return err
}

// DeleteProgress removes every recorded episode for one anime. Local files are
// grouped by title because each file has its own anime URL.
func (s *Store) DeleteProgress(ctx context.Context, provider, animeURL, animeTitle string) error {
	if provider == "local" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM watch_progress WHERE provider = 'local' AND anime_title = ?`, animeTitle)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM watch_progress WHERE provider = ? AND anime_url = ?`, provider, animeURL)
	return err
}

func (s *Store) SaveProgress(ctx context.Context, p Progress) error {
	if p.UpdatedAt == 0 {
		p.UpdatedAt = time.Now().Unix()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO watch_progress
			(provider, episode_url, anime_url, anime_title, episode_number, position, duration, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, episode_url) DO UPDATE SET
			anime_url=excluded.anime_url,
			anime_title=excluded.anime_title,
			episode_number=excluded.episode_number,
			position=excluded.position,
			duration=excluded.duration,
			updated_at=excluded.updated_at`,
		p.Provider, p.EpisodeURL, p.AnimeURL, p.AnimeTitle, p.EpisodeNumber, p.Position, p.Duration, p.UpdatedAt)
	return err
}

func (s *Store) Progress(ctx context.Context, provider, episodeURL string) (Progress, bool, error) {
	var p Progress
	err := s.db.QueryRowContext(ctx, `
		SELECT provider, episode_url, anime_url, anime_title, episode_number, position, duration, updated_at
		FROM watch_progress WHERE provider=? AND episode_url=?`, provider, episodeURL).
		Scan(&p.Provider, &p.EpisodeURL, &p.AnimeURL, &p.AnimeTitle, &p.EpisodeNumber, &p.Position, &p.Duration, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return Progress{}, false, nil
	}
	return p, err == nil, err
}

func (s *Store) RecentProgress(ctx context.Context, limit int) ([]Progress, error) {
	// One row per anime: the latest watched episode plus how many episodes have
	// progress. Local files are grouped by title because their anime URL is the
	// file path.
	rows, err := s.db.QueryContext(ctx, `
		WITH grouped AS (
			SELECT provider, episode_url, anime_url, anime_title, episode_number, position, duration, updated_at,
				COUNT(*) OVER (PARTITION BY provider, grp) AS episodes,
				ROW_NUMBER() OVER (PARTITION BY provider, grp ORDER BY updated_at DESC, episode_number DESC) AS rn
			FROM (
				SELECT *, CASE WHEN provider = 'local' THEN anime_title ELSE anime_url END AS grp
				FROM watch_progress
			)
		)
		SELECT provider, episode_url, anime_url, anime_title, episode_number, position, duration, updated_at, episodes
		FROM grouped WHERE rn = 1 ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Progress
	for rows.Next() {
		var p Progress
		if err := rows.Scan(&p.Provider, &p.EpisodeURL, &p.AnimeURL, &p.AnimeTitle, &p.EpisodeNumber, &p.Position, &p.Duration, &p.UpdatedAt, &p.Episodes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ProgressForAnime(ctx context.Context, provider, animeURL string) (map[string]Progress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, episode_url, anime_url, anime_title, episode_number, position, duration, updated_at
		FROM watch_progress WHERE provider=? AND anime_url=?`, provider, animeURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Progress{}
	for rows.Next() {
		var p Progress
		if err := rows.Scan(&p.Provider, &p.EpisodeURL, &p.AnimeURL, &p.AnimeTitle, &p.EpisodeNumber, &p.Position, &p.Duration, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out[p.EpisodeURL] = p
	}
	return out, rows.Err()
}

func (s *Store) ProgressForProvider(ctx context.Context, provider string) (map[string]Progress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, episode_url, anime_url, anime_title, episode_number, position, duration, updated_at
		FROM watch_progress WHERE provider=?`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Progress{}
	for rows.Next() {
		var p Progress
		if err := rows.Scan(&p.Provider, &p.EpisodeURL, &p.AnimeURL, &p.AnimeTitle, &p.EpisodeNumber, &p.Position, &p.Duration, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out[p.EpisodeURL] = p
	}
	return out, rows.Err()
}

func (s *Store) RecordDownload(ctx context.Context, d Download) error {
	if d.CompletedAt == 0 {
		d.CompletedAt = time.Now().Unix()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO downloads
			(provider, episode_url, anime_url, anime_title, episode_number, path, bytes, completed_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, episode_url) DO UPDATE SET
			path=excluded.path, bytes=excluded.bytes, completed_at=excluded.completed_at`,
		d.Provider, d.EpisodeURL, d.AnimeURL, d.AnimeTitle, d.EpisodeNumber, d.Path, d.Bytes, d.CompletedAt)
	return err
}

func (s *Store) DownloadsForAnime(ctx context.Context, provider, animeURL string) (map[int]Download, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, episode_url, anime_url, anime_title, episode_number, path, bytes, completed_at
		FROM downloads WHERE provider=? AND anime_url=?`, provider, animeURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]Download{}
	for rows.Next() {
		var d Download
		if err := rows.Scan(&d.Provider, &d.EpisodeURL, &d.AnimeURL, &d.AnimeTitle, &d.EpisodeNumber, &d.Path, &d.Bytes, &d.CompletedAt); err != nil {
			return nil, err
		}
		out[d.EpisodeNumber] = d
	}
	return out, rows.Err()
}

func (s *Store) ProviderStates(ctx context.Context) (map[string]ProviderState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, enabled, last_ok, last_error FROM provider_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ProviderState{}
	for rows.Next() {
		var state ProviderState
		var enabled int
		if err := rows.Scan(&state.Name, &enabled, &state.LastOK, &state.LastError); err != nil {
			return nil, err
		}
		state.Enabled = enabled != 0
		out[state.Name] = state
	}
	return out, rows.Err()
}

func (s *Store) SetProviderEnabled(ctx context.Context, name string, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_state(name, enabled, last_ok, last_error) VALUES(?, ?, 0, '')
		ON CONFLICT(name) DO UPDATE SET enabled=excluded.enabled`, name, value)
	return err
}

func (s *Store) MarkProviderOK(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_state(name, enabled, last_ok, last_error) VALUES(?, 1, ?, '')
		ON CONFLICT(name) DO UPDATE SET last_ok=excluded.last_ok, last_error=''`, name, time.Now().Unix())
	return err
}

func (s *Store) MarkProviderError(ctx context.Context, name, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_state(name, enabled, last_ok, last_error) VALUES(?, 1, 0, ?)
		ON CONFLICT(name) DO UPDATE SET last_error=excluded.last_error`, name, message)
	return err
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(bytes), 0) FROM downloads`).Scan(&stats.Downloads, &stats.Bytes); err != nil {
		return stats, err
	}
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM watch_progress WHERE position > 0`).Scan(&stats.Watched)
	return stats, err
}
