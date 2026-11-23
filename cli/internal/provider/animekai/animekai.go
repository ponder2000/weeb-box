// Package animekai adapts animekai.com.co (WordPress JSON search + MegaPlay player).
package animekai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/ponder2000/weeb-box/internal/provider"
	"github.com/ponder2000/weeb-box/internal/provider/megaplay"
)

const (
	DefaultBaseURL = "https://animekai.com.co"
	userAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0 Safari/537.36"
)

var episodeNum = regexp.MustCompile(`-episode-(\d+)`)

type Adapter struct {
	BaseURL string
	Client  *http.Client
}

func New(baseURL string) *Adapter {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Adapter{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *Adapter) Name() string { return "animekai" }

func (a *Adapter) SiteURL() string { return a.BaseURL }

func (a *Adapter) Search(ctx context.Context, query string) ([]provider.Anime, error) {
	body, err := a.get(ctx, a.BaseURL+"/wp-json/wp/v2/search?search="+url.QueryEscape(query)+"&per_page=20")
	if err != nil {
		return nil, err
	}
	return parseSearch(bytes.NewReader(body))
}

func (a *Adapter) Episodes(ctx context.Context, anime provider.Anime) ([]provider.Episode, error) {
	body, err := a.get(ctx, anime.URL)
	if err != nil {
		return nil, err
	}
	return parseEpisodes(bytes.NewReader(body))
}

func (a *Adapter) Stream(ctx context.Context, episode provider.Episode) (provider.Stream, error) {
	page, err := a.get(ctx, episode.URL)
	if err != nil {
		return provider.Stream{}, err
	}
	playerURL, err := firstAttr(bytes.NewReader(page), `iframe[src*="newplayer"]`, "src")
	if err != nil {
		return provider.Stream{}, fmt.Errorf("episode player: %w", err)
	}
	playerURL = a.resolve(episode.URL, playerURL)

	player, err := a.get(ctx, playerURL)
	if err != nil {
		return provider.Stream{}, err
	}
	streamURL, err := firstAttr(bytes.NewReader(player), `iframe[src*="megaplay"]`, "src")
	if err != nil {
		streamURL, err = firstAttr(bytes.NewReader(player), "iframe", "src")
		if err != nil {
			return provider.Stream{}, fmt.Errorf("stream player: %w", err)
		}
	}
	streamURL = a.resolve(playerURL, streamURL)
	return megaplay.Resolve(ctx, a.Client, streamURL, playerURL)
}

func (a *Adapter) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.abs(rawURL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req.URL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

func (a *Adapter) abs(raw string) string { return a.resolve(a.BaseURL+"/", raw) }

func (a *Adapter) resolve(base, raw string) string {
	raw = strings.TrimSpace(raw)
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if ref.IsAbs() {
		return ref.String()
	}
	b, berr := url.Parse(strings.TrimSpace(base))
	if berr != nil || b.Scheme == "" {
		return raw
	}
	return b.ResolveReference(ref).String()
}

func parseSearch(r io.Reader) ([]provider.Anime, error) {
	var items []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Subtype string `json:"subtype"`
	}
	if err := json.NewDecoder(r).Decode(&items); err != nil {
		return nil, fmt.Errorf("animekai: search json: %w", err)
	}
	var out []provider.Anime
	seen := map[string]bool{}
	for _, item := range items {
		link := strings.TrimSpace(item.URL)
		if link == "" || seen[link] {
			continue
		}
		if item.Subtype != "anime" && !strings.Contains(link, "/anime/") {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(item.Title))
		if title == "" {
			continue
		}
		seen[link] = true
		out = append(out, provider.Anime{Title: title, URL: link})
	}
	return out, nil
}

func parseEpisodes(r io.Reader) ([]provider.Episode, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	// The episode grid is the show's own list; unscoped "-episode-" links also
	// match the recommended-series sidebar on a live page.
	list := doc.Find(`#ak-episodes a[href*="-episode-"]`)
	if list.Length() == 0 {
		list = doc.Find(`a[href*="-episode-"]`)
	}
	var out []provider.Episode
	seen := map[string]bool{}
	list.Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || seen[href] {
			return
		}
		match := episodeNum.FindStringSubmatch(href)
		if len(match) != 2 {
			return
		}
		num, err := strconv.Atoi(match[1])
		if err != nil {
			return
		}
		seen[href] = true
		out = append(out, provider.Episode{Number: num, Title: strings.TrimSpace(s.Text()), URL: href})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Number == out[j].Number {
			return out[i].URL < out[j].URL
		}
		return out[i].Number < out[j].Number
	})
	return out, nil
}

func firstAttr(r io.Reader, selector, attr string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return "", err
	}
	value, ok := doc.Find(selector).First().Attr(attr)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("no %s on %s", attr, selector)
	}
	return strings.TrimSpace(value), nil
}
