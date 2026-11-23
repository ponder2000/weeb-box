// Package megaplay resolves MegaPlay iframe pages into HLS streams and captions.
package megaplay

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/ponder2000/weeb-box/internal/provider"
)

const (
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0 Safari/537.36"

	// Embedded in the MegaPlay player JS. If streams start failing, re-read
	// https://megaplay.buzz/lib/app.main.js and update these constants.
	sourcesKey = "i?LMTAx0Q6,:}50U"
	sourcesIV  = "W0;27ToaUpl_P%'c"
)

// Resolve takes a MegaPlay player page URL and the page that embeds it, then
// returns the HLS URL and caption tracks.
func Resolve(ctx context.Context, client *http.Client, streamPageURL, referer string) (provider.Stream, error) {
	streamPage, err := fetch(ctx, client, streamPageURL, referer, false)
	if err != nil {
		return provider.Stream{}, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(streamPage))
	if err != nil {
		return provider.Stream{}, err
	}
	id, ok := doc.Find("#megaplay-player").Attr("data-id")
	if !ok {
		id, ok = doc.Find("[data-id]").First().Attr("data-id")
	}
	if !ok || id == "" {
		return provider.Stream{}, fmt.Errorf("megaplay: no data-id in %s", streamPageURL)
	}

	origin := originOf(streamPageURL)
	apiURL := origin + "/stream/getSourcesNew?id=" + url.QueryEscape(id)
	sources, err := fetch(ctx, client, apiURL, streamPageURL, true)
	if err != nil {
		return provider.Stream{}, err
	}

	var payload struct {
		Enc    string `json:"enc"`
		Tracks []struct {
			File    string `json:"file"`
			Label   string `json:"label"`
			Kind    string `json:"kind"`
			Default bool   `json:"default"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(sources, &payload); err != nil {
		return provider.Stream{}, fmt.Errorf("megaplay: sources json: %w", err)
	}
	plain, err := decryptSources(payload.Enc)
	if err != nil {
		return provider.Stream{}, err
	}
	var media struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal([]byte(plain), &media); err != nil {
		return provider.Stream{}, fmt.Errorf("megaplay: decrypted sources: %w", err)
	}
	if media.File == "" {
		return provider.Stream{}, fmt.Errorf("megaplay: empty stream url")
	}

	var subtitles []provider.Subtitle
	for _, track := range payload.Tracks {
		if !strings.HasSuffix(strings.ToLower(track.File), ".vtt") {
			continue
		}
		if track.Kind != "" && track.Kind != "captions" {
			continue
		}
		subtitles = append(subtitles, provider.Subtitle{
			URL: track.File, Label: track.Label, Default: track.Default,
		})
	}
	return provider.Stream{URL: media.File, Referer: origin + "/", Subtitles: subtitles}, nil
}

func fetch(ctx context.Context, client *http.Client, rawURL, referer string, xhr bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if xhr {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func decryptSources(enc string) (string, error) {
	enc = strings.TrimRight(strings.TrimSpace(enc), "=")
	if enc == "" {
		return "", fmt.Errorf("megaplay: empty encrypted sources")
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("megaplay: decode sources: %w", err)
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", fmt.Errorf("megaplay: bad ciphertext length %d", len(raw))
	}
	block, err := aes.NewCipher(padKey([]byte(sourcesKey), 32))
	if err != nil {
		return "", err
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, padKey([]byte(sourcesIV), 16)).CryptBlocks(out, raw)

	n := int(out[len(out)-1])
	if n < 1 || n > aes.BlockSize || n > len(out) {
		return "", fmt.Errorf("megaplay: bad padding")
	}
	for _, b := range out[len(out)-n:] {
		if int(b) != n {
			return "", fmt.Errorf("megaplay: bad padding")
		}
	}
	return string(out[:len(out)-n]), nil
}

func padKey(key []byte, size int) []byte {
	padded := make([]byte, size)
	copy(padded, key)
	return padded
}
