package store

import (
	"context"
	"database/sql"
	"time"
)

// GetAnimeMeta returns the cached metadata payload for key, if any.
func (s *Store) GetAnimeMeta(ctx context.Context, key string) ([]byte, time.Time, bool, error) {
	var payload string
	var fetchedAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM anime_meta WHERE key=?`, key).Scan(&payload, &fetchedAt)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	return []byte(payload), time.Unix(fetchedAt, 0), true, nil
}

func (s *Store) PutAnimeMeta(ctx context.Context, key, payload string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO anime_meta(key, payload, fetched_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET payload=excluded.payload, fetched_at=excluded.fetched_at`,
		key, payload, time.Now().Unix())
	return err
}
