// Package hianime adapts hianime.ms (Hianime v2 theme + MegaPlay backup player).
package hianime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	DefaultBaseURL = "https://hianime.ms"
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

func (a *Adapter) Name() string { return "hianime" }

func (a *Adapter) Search(ctx context.Context, query string) ([]provider.Anime, error) {
	body, err := a.get(ctx, a.BaseURL+"/search?q="+url.QueryEscape(query))
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
	return parseEpisodes(bytes.NewReader(body), anime.Title)
}

func (a *Adapter) Stream(ctx context.Context, episode provider.Episode) (provider.Stream, error) {
	page, err := a.get(ctx, episode.URL)
	if err != nil {
		return provider.Stream{}, err
	}
	cfg, err := parseWatch(page)
	if err != nil {
		return provider.Stream{}, err
	}
	if cfg.Token == "" {
		return provider.Stream{}, fmt.Errorf("hianime: no stream token on %s", episode.URL)
	}
	lang := cfg.Lang
	if lang == "" {
		lang = "sub"
	}
	if lang != "sub" && lang != "dub" && lang != "hsub" {
		return provider.Stream{}, fmt.Errorf("hianime: unexpected lang %q", lang)
	}
	playerURL, err := a.playerURL(ctx, cfg.Token, lang)
	if err != nil {
		return provider.Stream{}, err
	}
	return megaplay.Resolve(ctx, a.Client, playerURL, episode.URL)
}

// playerURL follows /stream/<token>/<lang> to the MegaPlay page it redirects to.
// shortcut: assumes Hianime always 302s that path to megaplay.buzz instead of
// proxying the player itself; if the player starts being served in-page, fix here.
func (a *Adapter) playerURL(ctx context.Context, token, lang string) (string, error) {
	rawURL := a.BaseURL + "/stream/" + url.PathEscape(token) + "/" + lang
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", a.BaseURL+"/")
	resp, err := a.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	final := resp.Request.URL.String()
	if final == rawURL {
		return "", fmt.Errorf("hianime: %s did not redirect to a player", rawURL)
	}
	return final, nil
}

func (a *Adapter) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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

func parseSearch(r io.Reader) ([]provider.Anime, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	var out []provider.Anime
	seen := map[string]bool{}
	doc.Find("article.hk-tile").Each(func(_ int, article *goquery.Selection) {
		link := article.Find(".hk-tile__name a").First()
		href, ok := link.Attr("href")
		href = strings.TrimSpace(href)
		if !ok || href == "" || seen[href] {
			return
		}
		title := strings.TrimSpace(link.Text())
		if title == "" {
			title, _ = link.Attr("title")
			title = strings.TrimSpace(title)
		}
		if title == "" {
			return
		}
		var meta []string
		article.Find(".hk-tile__meta span").Each(func(_ int, s *goquery.Selection) {
			if value := strings.TrimSpace(s.Text()); value != "" {
				meta = append(meta, value)
			}
		})
		seen[href] = true
		out = append(out, provider.Anime{Title: title, URL: href, Meta: strings.Join(meta, " · ")})
	})
	return out, nil
}

// parseEpisodes reads the details page grid. Movies have no grid, just a
// "Watch the movie" link, so they come back as a single episode.
func parseEpisodes(r io.Reader, animeTitle string) ([]provider.Episode, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	var out []provider.Episode
	seen := map[string]bool{}
	doc.Find("a.hk-ep[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" || seen[href] {
			return
		}
		num, err := strconv.Atoi(strings.TrimSpace(s.AttrOr("data-ep", "")))
		if err != nil {
			match := episodeNum.FindStringSubmatch(href)
			if len(match) != 2 {
				return
			}
			num, err = strconv.Atoi(match[1])
			if err != nil {
				return
			}
		}
		title := strings.TrimSpace(s.AttrOr("data-name", ""))
		if title == "" {
			title = strings.TrimSpace(s.AttrOr("title", ""))
		}
		seen[href] = true
		out = append(out, provider.Episode{Number: num, Title: title, URL: href})
	})
	if len(out) == 0 {
		if href, ok := doc.Find(`a[href*="/watch-"]`).First().Attr("href"); ok && strings.TrimSpace(href) != "" {
			out = append(out, provider.Episode{Number: 1, Title: animeTitle, URL: strings.TrimSpace(href)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Number == out[j].Number {
			return out[i].URL < out[j].URL
		}
		return out[i].Number < out[j].Number
	})
	return out, nil
}

type watchConfig struct {
	Movie bool   `json:"movie"`
	Ep    int    `json:"ep"`
	Token string `json:"token"`
	Lang  string `json:"lang"`
}

func parseWatch(page []byte) (watchConfig, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
	if err != nil {
		return watchConfig{}, err
	}
	raw, ok := doc.Find("[data-hk-watch]").First().Attr("data-hk-watch")
	if !ok || strings.TrimSpace(raw) == "" {
		return watchConfig{}, fmt.Errorf("hianime: no data-hk-watch config")
	}
	var cfg watchConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return watchConfig{}, fmt.Errorf("hianime: watch config: %w", err)
	}
	return cfg, nil
}
