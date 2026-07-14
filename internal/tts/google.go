package tts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultGoogleTTSURL = "https://translate.google.com/translate_tts"

// googleChunkLimit is the max query length (runes) per translate_tts request; the
// endpoint 400s on long q, so longer text is split and the MP3 parts concatenated.
const googleChunkLimit = 200

// googleBackend synthesizes speech via Google's free, unofficial translate_tts
// endpoint (MP3), caches the audio, and plays it with a discovered player. It is
// keyless but best-effort: it can rate-limit (429) or change without notice.
type googleBackend struct {
	url       string
	userAgent string
	cacheDir  string
	http      *http.Client
	player    player
}

func newGoogle(rawURL, ua, cacheDir string, timeout time.Duration, pl player) *googleBackend {
	if rawURL == "" {
		rawURL = defaultGoogleTTSURL
	}
	if ua == "" {
		ua = "Mozilla/5.0 translate-cli"
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &googleBackend{
		url:       rawURL,
		userAgent: ua,
		cacheDir:  cacheDir,
		http:      &http.Client{Timeout: timeout},
		player:    pl,
	}
}

func (g *googleBackend) Name() string { return "google" }

// Available reports whether an audio player exists. The network is probed lazily
// at Speak time, so google can sit as a fallback without a round-trip.
func (g *googleBackend) Available() bool { return g.player.available() }

// Speak fetches (or reuses cached) MP3 audio for text and plays it.
func (g *googleBackend) Speak(ctx context.Context, text, langCode string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return ErrEmptyText
	}
	tl := googleTL(langCode)
	file := cacheFile(g.cacheDir, tl, text)
	if !cached(file) {
		mp3, err := g.fetch(ctx, text, tl)
		if err != nil {
			return err
		}
		if err := writeCache(file, mp3); err != nil {
			return err
		}
	}
	return g.player.play(ctx, file)
}

// fetch downloads and concatenates the MP3 for text (chunked to ≤googleChunkLimit).
func (g *googleBackend) fetch(ctx context.Context, text, tl string) ([]byte, error) {
	var out []byte
	for _, chunk := range chunkText(text, googleChunkLimit) {
		q := url.Values{}
		q.Set("ie", "UTF-8")
		q.Set("client", "tw-ob")
		q.Set("tl", tl)
		q.Set("q", chunk)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", g.userAgent)
		resp, err := g.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("google tts: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("google tts: http %d", resp.StatusCode)
		}
		if readErr != nil {
			return nil, fmt.Errorf("google tts: %w", readErr)
		}
		out = append(out, body...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("google tts: empty audio")
	}
	return out, nil
}

// chunkText splits s into pieces of at most n runes, preferring to break on a
// space so words are not cut mid-syllable.
func chunkText(s string, n int) []string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return []string{string(r)}
	}
	var out []string
	for len(r) > n {
		cut := n
		for i := n; i > n/2; i-- { // back up to the last space in the window
			if r[i] == ' ' {
				cut = i
				break
			}
		}
		if piece := strings.TrimSpace(string(r[:cut])); piece != "" {
			out = append(out, piece)
		}
		r = r[cut:]
	}
	if piece := strings.TrimSpace(string(r)); piece != "" {
		out = append(out, piece)
	}
	return out
}
