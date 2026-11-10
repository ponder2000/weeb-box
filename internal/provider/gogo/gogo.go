// Package gogo adapts gogoanime.or.at (WordPress "animestream" theme + MegaPlay player).
package gogo

import (
	"bytes"
	"context"
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
	DefaultBaseURL = "https://gogoanime.or.at"
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

func (a *Adapter) Name() string { return "gogoanime" }

func (a *Adapter) Search(ctx context.Context, query string) ([]provider.Anime, error) {
	body, err := a.get(ctx, a.BaseURL+"/?s="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	return parseSearch(bytes.NewReader(body))
}

func (a *Adapter) Episodes(ctx context.Context, anime provider.Anime) ([]provider.Episode, error) {
	body, err := a.get(ctx, anime.URL, nil)
	if err != nil {
		return nil, err
	}
	return parseEpisodes(bytes.NewReader(body))
}

func (a *Adapter) Stream(ctx context.Context, episode provider.Episode) (provider.Stream, error) {
	page, err := a.get(ctx, episode.URL, nil)
	if err != nil {
		return provider.Stream{}, err
	}
	playerURL, err := firstAttr(bytes.NewReader(page), `iframe[src*="newplayer"]`, "src")
	if err != nil {
		return provider.Stream{}, fmt.Errorf("episode player: %w", err)
	}
	playerURL = a.resolve(episode.URL, playerURL)

	player, err := a.get(ctx, playerURL, nil)
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

func (a *Adapter) get(ctx context.Context, rawURL string, headers http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.abs(rawURL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for key, values := range headers {
		req.Header[key] = values
	}
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

// resolve turns an iframe/`href` value into an absolute URL. Relative values
// must resolve against the page they came from, which is not always BaseURL.
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
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	var out []provider.Anime
	seen := map[string]bool{}
	doc.Find(`.postbody .listupd article.bs a[itemprop="url"]`).Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || seen[href] {
			return
		}
		title, _ := s.Attr("title")
		title = strings.TrimSpace(title)
		if title == "" {
			title = strings.TrimSpace(s.Text())
		}
		if title == "" {
			return
		}
		article := s.Closest("article.bs")
		var meta []string
		for _, selector := range []string{".typez", ".sb", ".epx"} {
			if value := strings.TrimSpace(article.Find(selector).First().Text()); value != "" {
				meta = append(meta, value)
			}
		}
		seen[href] = true
		out = append(out, provider.Anime{Title: title, URL: href, Meta: strings.Join(meta, " · ")})
	})
	return out, nil
}

func parseEpisodes(r io.Reader) ([]provider.Episode, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	var out []provider.Episode
	seen := map[string]bool{}
	doc.Find(".eplister ul li a").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || seen[href] {
			return
		}
		num, err := strconv.Atoi(strings.TrimSpace(s.Find(".epl-num").Text()))
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
		seen[href] = true
		title := strings.TrimSpace(s.Find(".epl-title").First().Text())
		out = append(out, provider.Episode{Number: num, Title: title, URL: href})
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
