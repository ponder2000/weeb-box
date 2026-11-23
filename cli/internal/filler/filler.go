// Package filler reports which episodes of an anime are filler, using
// animefillerlist.com's per-show episode tables.
package filler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	DefaultBaseURL = "https://www.animefillerlist.com"
	userAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0 Safari/537.36"
)

// Kind is an episode's canon status.
type Kind string

const (
	Filler Kind = "filler"
	Mixed  Kind = "mixed"
	Canon  Kind = "canon"
)

// Entry is one episode row.
type Entry struct {
	Number int
	Kind   Kind
}

// List is one show's episode table.
type List struct {
	Title   string
	URL     string
	Entries []Entry
}

// KindFor reports the episode's kind; ok is false when the list has no row.
func (l List) KindFor(number int) (Kind, bool) {
	for _, e := range l.Entries {
		if e.Number == number {
			return e.Kind, true
		}
	}
	return "", false
}

// Max is the highest episode number in the list.
func (l List) Max() int {
	max := 0
	for _, e := range l.Entries {
		if e.Number > max {
			max = e.Number
		}
	}
	return max
}

// ErrNotFound means the search had no show with an exactly matching title.
var ErrNotFound = errors.New("filler list not found")

type Client struct {
	BaseURL string
	Client  *http.Client
}

// New builds a client; baseURL is trimmed and an empty value means the live site.
func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Lookup finds the show whose normalized title exactly equals title and returns
// its episode table. Near matches are rejected on purpose: a wrong show's list
// is worse than no list.
func (c *Client) Lookup(ctx context.Context, title string) (List, error) {
	body, err := c.get(ctx, c.BaseURL+"/search/node?keys="+url.QueryEscape(title))
	if err != nil {
		return List{}, err
	}
	results, err := parseSearch(bytes.NewReader(body))
	if err != nil {
		return List{}, fmt.Errorf("search: %w", err)
	}
	want := normalize(title)
	var match *result
	for i := range results {
		if normalize(results[i].title) == want {
			match = &results[i]
			break
		}
	}
	if match == nil {
		return List{}, ErrNotFound
	}

	showURL := match.url
	if strings.HasPrefix(showURL, "/") {
		showURL = c.BaseURL + showURL
	}
	page, err := c.get(ctx, showURL)
	if err != nil {
		return List{}, err
	}
	entries, err := parseShow(bytes.NewReader(page))
	if err != nil {
		return List{}, fmt.Errorf("show: %w", err)
	}
	return List{Title: match.title, URL: showURL, Entries: entries}, nil
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req.URL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

type result struct {
	title string
	url   string
}

func parseSearch(r io.Reader) ([]result, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	var out []result
	doc.Find(`div.item-list li a[href^="/shows/"]`).Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		title, _ := s.Attr("title")
		title = strings.TrimSpace(title)
		if title == "" {
			title, _ = s.Find("img").First().Attr("alt")
			title = strings.TrimSpace(title)
		}
		if title == "" {
			title = strings.TrimSpace(s.Text())
		}
		if title == "" {
			return
		}
		out = append(out, result{title: title, url: href})
	})
	return out, nil
}

func parseShow(r io.Reader) ([]Entry, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	var out []Entry
	seen := map[int]bool{}
	doc.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		kind, ok := kindOf(tr.AttrOr("class", ""))
		if !ok {
			return
		}
		number, err := strconv.Atoi(strings.TrimSpace(tr.Find("td.Number").First().Text()))
		if err != nil || number <= 0 || seen[number] {
			return
		}
		seen[number] = true
		out = append(out, Entry{Number: number, Kind: kind})
	})
	return out, nil
}

// kindOf maps the first class token on an episode row; unknown rows are skipped.
func kindOf(class string) (Kind, bool) {
	token := strings.Fields(class)
	if len(token) == 0 {
		return "", false
	}
	switch token[0] {
	case "filler":
		return Filler, true
	case "mixed_canon/filler":
		return Mixed, true
	case "manga_canon", "anime_canon":
		return Canon, true
	}
	return "", false
}

func normalize(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	// Providers decorate titles with the audio track, e.g. "Bleach (Dub)".
	for _, suffix := range []string{"dubbed", "subbed", "dub", "sub"} {
		out = strings.TrimSuffix(out, suffix)
	}
	return out
}
