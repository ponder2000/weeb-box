// Package metadata fetches anime artwork, genres, tags, and age ratings from
// AniList (GraphQL) and Jikan (MyAnimeList REST).
package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultAniListURL = "https://graphql.anilist.co"
	DefaultJikanURL   = "https://api.jikan.moe/v4"
	userAgent         = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0 Safari/537.36"
	maxBody           = 5 << 20
	maxTags           = 8
	perPage           = 20
)

// Genres lists the AniList genres accepted by Browse.
var Genres = []string{
	"Action", "Adventure", "Comedy", "Drama", "Ecchi", "Fantasy", "Horror",
	"Mahou Shoujo", "Mecha", "Music", "Mystery", "Psychological", "Romance",
	"Sci-Fi", "Slice of Life", "Sports", "Supernatural", "Thriller",
}

// Info is one anime's display metadata.
type Info struct {
	Title       string
	CoverURL    string
	Genres      []string
	Tags        []string
	Rating      string // age rating: PG-13, R, R+, Rx, 18+, Not rated
	Adult       bool
	Score       float64 // AniList averageScore (0-100), 0 when unrated
	Description string
}

type Client struct {
	HTTP       *http.Client
	AniListURL string
	JikanURL   string
}

func New() *Client {
	return &Client{
		HTTP:       &http.Client{Timeout: 20 * time.Second},
		AniListURL: DefaultAniListURL,
		JikanURL:   DefaultJikanURL,
	}
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Key normalizes a title into a cache key.
func Key(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

const mediaFields = `
	title { romaji english }
	coverImage { large }
	genres
	tags { name isMediaSpoiler }
	isAdult
	averageScore
	description(asHtml: false)
`

const lookupQuery = `query ($search: String) { Media(search: $search, type: ANIME) {` + mediaFields + `} }`

const browseQuery = `query ($genre: String, $page: Int) { Page(page: $page, perPage: 20) {
	media(genre: $genre, type: ANIME, isAdult: false, sort: POPULARITY_DESC) {` + mediaFields + `}
} }`

type anilistMedia struct {
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
	} `json:"title"`
	CoverImage struct {
		Large string `json:"large"`
	} `json:"coverImage"`
	Genres []string `json:"genres"`
	Tags   []struct {
		Name           string `json:"name"`
		IsMediaSpoiler bool   `json:"isMediaSpoiler"`
	} `json:"tags"`
	IsAdult      bool    `json:"isAdult"`
	AverageScore float64 `json:"averageScore"`
	Description  string  `json:"description"`
}

type anilistResponse struct {
	Data struct {
		Media anilistMedia `json:"Media"`
		Page  struct {
			Media []anilistMedia `json:"media"`
		} `json:"Page"`
	} `json:"Data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (m anilistMedia) info() Info {
	info := Info{
		Title:       m.Title.English,
		CoverURL:    m.CoverImage.Large,
		Genres:      m.Genres,
		Adult:       m.IsAdult,
		Score:       m.AverageScore,
		Description: m.Description,
	}
	if info.Title == "" {
		info.Title = m.Title.Romaji
	}
	for _, tag := range m.Tags {
		if len(info.Tags) == maxTags {
			break
		}
		if !tag.IsMediaSpoiler {
			info.Tags = append(info.Tags, tag.Name)
		}
	}
	return info
}

// Lookup resolves metadata for one title. A Jikan failure only costs the age
// rating, which falls back to "18+" or "Not rated" from AniList.
func (c *Client) Lookup(ctx context.Context, title string) (Info, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Info{}, errors.New("metadata: empty title")
	}
	var resp anilistResponse
	if err := c.post(ctx, lookupQuery, map[string]any{"search": title}, &resp); err != nil {
		return Info{}, err
	}
	info := resp.Data.Media.info()
	if info.Title == "" {
		return Info{}, fmt.Errorf("metadata: no AniList entry for %q", title)
	}
	rating, adult, err := c.jikanRating(ctx, title)
	if err != nil || rating == "" {
		info.Rating = fallbackRating(info.Adult)
	} else {
		info.Adult = info.Adult || adult
		info.Rating = rating
	}
	return info, nil
}

// Browse lists currently popular anime in one genre. Ratings come from AniList
// only, so they read "Not rated" until Lookup fills them in.
func (c *Client) Browse(ctx context.Context, genre string, page int) ([]Info, error) {
	if page < 1 {
		page = 1
	}
	var genreVar any
	if genre = strings.TrimSpace(genre); genre != "" {
		genreVar = genre
	}
	var resp anilistResponse
	vars := map[string]any{"genre": genreVar, "page": page}
	if err := c.post(ctx, browseQuery, vars, &resp); err != nil {
		return nil, err
	}
	items := make([]Info, 0, len(resp.Data.Page.Media))
	for _, media := range resp.Data.Page.Media {
		info := media.info()
		info.Rating = fallbackRating(info.Adult)
		items = append(items, info)
	}
	return items, nil
}

func fallbackRating(adult bool) string {
	if adult {
		return "18+"
	}
	return "Not rated"
}

func isAdultRating(rating string) bool {
	return strings.HasPrefix(rating, "R+") || strings.HasPrefix(rating, "Rx")
}

// jikanRating returns Jikan's age rating code ("PG-13", "R", "R+", "Rx").
func (c *Client) jikanRating(ctx context.Context, title string) (string, bool, error) {
	endpoint := strings.TrimRight(c.JikanURL, "/") + "/anime?q=" + url.QueryEscape(title) + "&limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.client().Do(req)
	if err != nil {
		return "", false, fmt.Errorf("metadata: jikan: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("metadata: jikan: %s", status(resp))
	}
	var body struct {
		Data []struct {
			Rating string `json:"rating"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return "", false, fmt.Errorf("metadata: jikan: %w", err)
	}
	if len(body.Data) == 0 {
		return "", false, nil
	}
	code := body.Data[0].Rating
	if i := strings.Index(code, " - "); i >= 0 {
		code = code[:i]
	}
	code = strings.TrimSpace(code)
	return code, isAdultRating(code), nil
}

func (c *Client) post(ctx context.Context, query string, vars map[string]any, out *anilistResponse) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.AniListURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("metadata: anilist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata: anilist: %s", status(resp))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("metadata: anilist: %w", err)
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("metadata: anilist: %s", out.Errors[0].Message)
	}
	return nil
}

// status reads a short slice of the body for the error message.
func status(resp *http.Response) string {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	text := strings.TrimSpace(string(snippet))
	if text == "" {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, text)
}
