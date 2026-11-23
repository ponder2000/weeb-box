package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ponder2000/weeb-box/internal/provider"
)

const (
	playlistLimit = 4 << 20
	probeBytes    = 256 << 10
	probeWindow   = 2 * time.Second
	playlistTTL   = 10 * time.Second
)

type variant struct {
	bandwidth int64
	height    int
	name      string
	url       string
}

// SelectQuality swaps an HLS master playlist URL for one of its variants.
// preference is "", "auto", or an explicit height such as "1080p". Non-HLS
// streams and playlists without variants are returned unchanged.
func SelectQuality(ctx context.Context, client *http.Client, src provider.Stream, preference string) (provider.Stream, error) {
	if !isHLS(src.URL) {
		return src, nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	variants, err := fetchVariants(ctx, client, src)
	if err != nil {
		return src, err
	}
	if len(variants) == 0 {
		return src, nil
	}
	chosen := pickExplicit(variants, preference)
	if _, auto := parsePreference(preference); auto {
		chosen = autoVariant(ctx, client, src, variants)
	}
	src.URL = chosen.url
	return src, nil
}

// Variants lists the heights a master playlist offers, highest first, e.g.
// ["1080p", "720p", "480p"]. Non-HLS streams and media playlists return nil.
func Variants(ctx context.Context, client *http.Client, src provider.Stream) ([]string, error) {
	if !isHLS(src.URL) {
		return nil, nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	variants, err := fetchVariants(ctx, client, src)
	if err != nil {
		return nil, err
	}
	var heights []int
	for _, v := range variants {
		if v.height > 0 {
			heights = append(heights, v.height)
		}
	}
	slices.Sort(heights)
	heights = slices.Compact(heights)
	slices.Reverse(heights)
	var names []string
	for _, h := range heights {
		names = append(names, fmt.Sprintf("%dp", h))
	}
	return names, nil
}

func isHLS(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}

func parsePreference(preference string) (height int, auto bool) {
	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "", "auto":
		return 0, true
	}
	value, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(preference)), "p"))
	if err != nil || value <= 0 {
		return 0, true
	}
	return value, false
}

// fetchVariants reads a master playlist and resolves each variant URI against
// the playlist URL. A media playlist yields no variants.
func fetchVariants(ctx context.Context, client *http.Client, src provider.Stream) ([]variant, error) {
	body, err := fetchPlaylist(ctx, client, src.URL, src.Referer)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(src.URL)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(body, "\n")
	var variants []variant
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		attrs := parseAttrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
		v := variant{name: attrs["NAME"]}
		if n, err := strconv.ParseInt(attrs["BANDWIDTH"], 10, 64); err == nil {
			v.bandwidth = n
		}
		if _, h, ok := strings.Cut(attrs["RESOLUTION"], "x"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil {
				v.height = n
			}
		}
		for j := i + 1; j < len(lines); j++ {
			uri := strings.TrimSpace(lines[j])
			if uri == "" || strings.HasPrefix(uri, "#") {
				continue
			}
			v.url = resolveURL(base, uri)
			break
		}
		if v.url != "" {
			variants = append(variants, v)
		}
	}
	return variants, nil
}

// parseAttrs splits KEY=VALUE pairs, honouring quoted values that may hold commas.
func parseAttrs(s string) map[string]string {
	attrs := make(map[string]string)
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ',' || s[i] == ' ') {
			i++
		}
		start := i
		for i < len(s) && s[i] != '=' {
			i++
		}
		if i >= len(s) {
			break
		}
		key := strings.ToUpper(strings.TrimSpace(s[start:i]))
		i++
		var value string
		if i < len(s) && s[i] == '"' {
			i++
			start = i
			for i < len(s) && s[i] != '"' {
				i++
			}
			value = s[start:i]
			i++
		} else {
			start = i
			for i < len(s) && s[i] != ',' {
				i++
			}
			value = strings.TrimSpace(s[start:i])
		}
		attrs[key] = value
	}
	return attrs
}

func resolveURL(base *url.URL, raw string) string {
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

// pickExplicit prefers an exact height/name, then the closest lower variant,
// then the lowest one.
func pickExplicit(variants []variant, preference string) variant {
	target, _ := strconv.Atoi(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(preference)), "p"))
	var best variant
	lower := false
	for _, v := range variants {
		if (target > 0 && v.height == target) || (v.name != "" && strings.EqualFold(v.name, preference)) {
			return v
		}
		if target > 0 && v.height > 0 && v.height < target && (!lower || v.height > best.height) {
			best, lower = v, true
		}
	}
	if lower {
		return best
	}
	return lowestVariant(variants)
}

func lowestVariant(variants []variant) variant {
	best := variants[0]
	for _, v := range variants[1:] {
		if v.bandwidth > 0 && (best.bandwidth == 0 || v.bandwidth < best.bandwidth) {
			best = v
		}
	}
	return best
}

// autoVariant estimates throughput from the lowest variant and picks the
// highest BANDWIDTH that fits, keeping 30% headroom for bursts.
func autoVariant(ctx context.Context, client *http.Client, src provider.Stream, variants []variant) variant {
	// shortcut: one 256 KiB segment sample with a 2s cap is a rough probe;
	// upgrade to a real throughput estimator if it picks badly on real sites.
	rate := probeRate(ctx, client, src, lowestVariant(variants).url)
	if rate <= 0 {
		return fallbackVariant(variants)
	}
	budget := rate * 8 * 0.7
	var best variant
	found := false
	for _, v := range variants {
		if v.bandwidth > 0 && float64(v.bandwidth) <= budget && (!found || v.bandwidth > best.bandwidth) {
			best, found = v, true
		}
	}
	if !found {
		return lowestVariant(variants)
	}
	return best
}

func fallbackVariant(variants []variant) variant {
	for _, v := range variants {
		if v.height == 720 {
			return v
		}
	}
	return lowestVariant(variants)
}

func probeRate(ctx context.Context, client *http.Client, src provider.Stream, playlist string) float64 {
	segment, err := firstSegment(ctx, client, src, playlist)
	if err != nil {
		return 0
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeWindow)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, segment, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", probeBytes-1))
	if src.Referer != "" {
		req.Header.Set("Referer", src.Referer)
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	read, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, probeBytes))
	elapsed := time.Since(start).Seconds()
	if read == 0 || elapsed <= 0 {
		return 0
	}
	return float64(read) / elapsed
}

func firstSegment(ctx context.Context, client *http.Client, src provider.Stream, playlist string) (string, error) {
	body, err := fetchPlaylist(ctx, client, playlist, src.Referer)
	if err != nil {
		return "", err
	}
	base, err := url.Parse(playlist)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return resolveURL(base, line), nil
		}
	}
	return "", fmt.Errorf("no segments in %s", playlist)
}

func fetchPlaylist(ctx context.Context, client *http.Client, target, referer string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, playlistTTL)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("playlist: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, playlistLimit))
	return string(body), err
}
