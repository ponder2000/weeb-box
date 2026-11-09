// Package provider defines the boundary every anime source adapter implements.
package provider

import "context"

type Anime struct {
	Title string
	URL   string
	Meta  string // optional display metadata, e.g. "TV · Sub · Completed"
}

type Episode struct {
	Number int
	Title  string
	URL    string
}

type Subtitle struct {
	URL     string
	Label   string
	Default bool
}

// Stream is a media URL ready for ffmpeg or a player. Referer is required by some CDNs.
type Stream struct {
	URL       string
	Referer   string
	Subtitles []Subtitle
}

type Provider interface {
	Name() string
	Search(ctx context.Context, query string) ([]Anime, error)
	Episodes(ctx context.Context, anime Anime) ([]Episode, error)
	Stream(ctx context.Context, episode Episode) (Stream, error)
}
