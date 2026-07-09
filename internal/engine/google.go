package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/daviddwlee84/translate/internal/lang"
)

// GoogleConfig configures the free, unofficial Google translate_a/single endpoint.
type GoogleConfig struct {
	Endpoint  string
	ExtraDT   []string // extra dt slots, e.g. "bd" (bilingual dict), "at" (alternates)
	UserAgent string
	Timeout   time.Duration
}

// GoogleEngine is a keyless translation engine using Google's web endpoint.
// It is unofficial and best-effort: it can rate-limit (HTTP 429) or change shape
// without notice, so it lives at the end of the fallback chain.
type GoogleEngine struct {
	cfg  GoogleConfig
	http *http.Client
}

// NewGoogle builds a Google engine with sensible defaults.
func NewGoogle(cfg GoogleConfig) *GoogleEngine {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://translate.googleapis.com/translate_a/single"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 4 * time.Second
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 translate-cli"
	}
	return &GoogleEngine{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

// Name returns "google".
func (e *GoogleEngine) Name() string { return "google" }

// Supports reports that Google handles translation only (not dictionary).
func (e *GoogleEngine) Supports(m Mode) bool { return m == ModeTranslate }

func (e *GoogleEngine) buildURL(req Request) string {
	sl := req.Source
	if sl == "" {
		sl = "auto"
	}
	q := url.Values{}
	q.Set("client", "gtx")
	q.Set("sl", sl)
	q.Set("tl", req.Target)
	q.Set("dt", "t")
	for _, dt := range e.cfg.ExtraDT {
		q.Add("dt", dt)
	}
	q.Set("q", req.Text)
	return e.cfg.Endpoint + "?" + q.Encode()
}

// Translate performs one keyless translation (non-streaming).
func (e *GoogleEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, ErrEmptyInput
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, e.buildURL(req), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", e.cfg.UserAgent)

	resp, err := e.http.Do(httpReq)
	if err != nil {
		return single(nil, fmt.Errorf("google: %w", err)), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return single(nil, fmt.Errorf("google: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))), nil
	}

	var data []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return single(nil, fmt.Errorf("google: decode: %w", err)), nil
	}
	res, err := parseGoogle(data, req)
	if err != nil {
		return single(nil, err), nil
	}
	res.Engine = e.Name()
	return single(res, nil), nil
}

// parseGoogle defensively walks the nested-array response. Positions: data[0] is
// an array of [translated, original, ...] segments (long text splits into many);
// data[2] is the detected source language.
func parseGoogle(data []json.RawMessage, req Request) (*TranslateResult, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("google: empty payload")
	}
	var segs []json.RawMessage
	if err := json.Unmarshal(data[0], &segs); err != nil {
		return nil, fmt.Errorf("google: bad segments: %w", err)
	}
	var b strings.Builder
	for _, s := range segs {
		var parts []json.RawMessage
		if json.Unmarshal(s, &parts) != nil || len(parts) == 0 {
			continue
		}
		var txt string
		if json.Unmarshal(parts[0], &txt) == nil {
			b.WriteString(txt)
		}
	}
	res := &TranslateResult{Translation: strings.TrimSpace(b.String()), Target: req.Target}
	if res.Translation == "" {
		return nil, fmt.Errorf("google: no translation in response")
	}
	if len(data) > 2 {
		var det string
		if json.Unmarshal(data[2], &det) == nil {
			res.DetectedSource = det
		}
	}
	return res, nil
}

// Detect uses the offline detector (cheap, no network round-trip).
func (e *GoogleEngine) Detect(ctx context.Context, text string) (string, error) {
	return lang.Detect(text), nil
}

// Available probes the endpoint with a tiny translation request.
func (e *GoogleEngine) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	u := e.buildURL(Request{Text: "a", Source: "auto", Target: "en"})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", e.cfg.UserAgent)
	resp, err := e.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}
