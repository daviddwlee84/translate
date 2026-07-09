package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"translate/internal/lang"
)

// LLMConfig parameterizes an OpenAI-compatible backend (copilot-proxy, Ollama,
// OpenRouter, LiteLLM, or a generic base_url+key endpoint).
type LLMConfig struct {
	Name      string        // "copilot", "ollama", "openrouter", ...
	BaseURL   string        // e.g. "http://localhost:4141/v1"
	Model     string        // e.g. "claude-sonnet-5"
	APIKeyEnv string        // env var holding the key; "" => no Authorization header
	Timeout   time.Duration // per-request timeout (0 => 60s)
}

// LLMEngine talks to any OpenAI-compatible /chat/completions endpoint.
type LLMEngine struct {
	cfg  LLMConfig
	key  string
	http *http.Client
}

// NewLLM builds an LLM engine, resolving the API key once from the environment.
func NewLLM(cfg LLMConfig) *LLMEngine {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	key := ""
	if cfg.APIKeyEnv != "" {
		key = strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
	}
	return &LLMEngine{cfg: cfg, key: key, http: &http.Client{Timeout: cfg.Timeout}}
}

// Name returns the provider name (e.g. "copilot").
func (e *LLMEngine) Name() string { return e.cfg.Name }

// Supports reports that LLM engines translate. (Dictionary lookups route to the
// dedicated dictionary engine in v1, so the chain never sends dict mode here.)
func (e *LLMEngine) Supports(m Mode) bool { return m == ModeTranslate }

// auth adds the bearer token only when a key is configured. copilot-proxy needs
// no Authorization header, so an empty APIKeyEnv yields no header at all.
func (e *LLMEngine) auth(r *http.Request) {
	if e.key != "" {
		r.Header.Set("Authorization", "Bearer "+e.key)
	}
}

// endpoint joins the base URL with a path segment.
func (e *LLMEngine) endpoint(path string) string {
	return strings.TrimRight(e.cfg.BaseURL, "/") + path
}

// Available is a cheap health probe: GET {BaseURL}/models with a short timeout.
func (e *LLMEngine) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint("/models"), nil)
	if err != nil {
		return false
	}
	e.auth(req)
	resp, err := e.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// Detect is a no-op for the LLM engine in this slice; offline detection is added
// with the free-API slice. Callers fall back to lang.Detect when this returns "".
func (e *LLMEngine) Detect(ctx context.Context, text string) (string, error) {
	return "", nil
}

// --- OpenAI chat wire types ---

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// streamDelta is one OpenAI SSE chunk (stream:true).
type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// --- Anthropic Messages API wire types (/v1/messages) ---

const anthropicMaxTokens = 4096

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// isAnthropicModel reports whether a model id must use the Anthropic Messages API
// (/v1/messages) rather than /chat/completions. copilot-proxy serves Claude
// models ONLY via the messages endpoint — they return HTTP 400
// "model_not_supported" on /chat/completions despite being listed in /v1/models.
func isAnthropicModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "claude")
}

// Translate performs a translation, streaming tokens when req.Stream is set.
// It always returns a channel that closes after exactly one terminal chunk, and
// dispatches to the Anthropic Messages API for Claude models.
func (e *LLMEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, ErrEmptyInput
	}
	model := NormalizeModelID(e.cfg.Model)
	if isAnthropicModel(model) {
		return e.translateAnthropic(ctx, req, model)
	}
	return e.translateOpenAI(ctx, req, model)
}

// finalize builds the terminal result from the accumulated translation text.
func (e *LLMEngine) finalize(full, model string, req Request) *TranslateResult {
	res := &TranslateResult{
		Translation: strings.TrimSpace(full),
		Target:      req.Target,
		Engine:      e.cfg.Name,
		Model:       model,
	}
	// The plain-text prompt returns only the translation, so fill the detected
	// source offline when the caller asked to auto-detect.
	if src := strings.TrimSpace(req.Source); src == "" || src == "auto" {
		res.DetectedSource = lang.Detect(req.Text)
	}
	return res
}

// translateOpenAI uses the OpenAI /chat/completions endpoint.
func (e *LLMEngine) translateOpenAI(ctx context.Context, req Request, model string) (<-chan Chunk, error) {
	system, user := buildTranslatePrompt(req)
	body := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: req.Stream,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint("/chat/completions"), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	e.auth(httpReq)

	ch := make(chan Chunk, 32) // buffered so a fast stream doesn't block on renders
	go func() {
		defer close(ch)
		resp, err := e.http.Do(httpReq)
		if err != nil {
			ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: %w", e.cfg.Name, err)}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			ch <- Chunk{Kind: ChunkError, Err: e.httpError(resp)}
			return
		}

		var full strings.Builder
		if req.Stream {
			if err := e.readSSE(ctx, resp.Body, ch, &full); err != nil {
				ch <- Chunk{Kind: ChunkError, Err: err}
				return
			}
		} else {
			var cr chatResponse
			if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
				ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: decode response: %w", e.cfg.Name, err)}
				return
			}
			if cr.Error != nil {
				ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: %s", e.cfg.Name, cr.Error.Message)}
				return
			}
			if len(cr.Choices) == 0 {
				ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: empty response", e.cfg.Name)}
				return
			}
			full.WriteString(cr.Choices[0].Message.Content)
		}
		ch <- Chunk{Kind: ChunkDone, Result: e.finalize(full.String(), model, req)}
	}()
	return ch, nil
}

// translateAnthropic uses the Anthropic Messages API (/v1/messages).
func (e *LLMEngine) translateAnthropic(ctx context.Context, req Request, model string) (<-chan Chunk, error) {
	system, user := buildTranslatePrompt(req)
	body := anthropicRequest{
		Model:     model,
		MaxTokens: anthropicMaxTokens,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
		Stream:    req.Stream,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint("/messages"), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	e.anthropicAuth(httpReq)

	ch := make(chan Chunk, 32)
	go func() {
		defer close(ch)
		resp, err := e.http.Do(httpReq)
		if err != nil {
			ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: %w", e.cfg.Name, err)}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			ch <- Chunk{Kind: ChunkError, Err: e.httpError(resp)}
			return
		}

		var full strings.Builder
		if req.Stream {
			if err := e.readAnthropicSSE(ctx, resp.Body, ch, &full); err != nil {
				ch <- Chunk{Kind: ChunkError, Err: err}
				return
			}
		} else {
			var ar anthropicResponse
			if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
				ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: decode response: %w", e.cfg.Name, err)}
				return
			}
			if ar.Error != nil {
				ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: %s", e.cfg.Name, ar.Error.Message)}
				return
			}
			for _, c := range ar.Content {
				if c.Type == "text" {
					full.WriteString(c.Text)
				}
			}
		}
		ch <- Chunk{Kind: ChunkDone, Result: e.finalize(full.String(), model, req)}
	}()
	return ch, nil
}

// anthropicAuth sets the Anthropic auth header (x-api-key, not Bearer). The
// copilot-proxy needs none, so an empty key yields no header.
func (e *LLMEngine) anthropicAuth(r *http.Request) {
	if e.key != "" {
		r.Header.Set("x-api-key", e.key)
	}
}

// readSSE parses an OpenAI-style event stream, emitting a ChunkToken per content
// delta and accumulating the full text into full.
func (e *LLMEngine) readSSE(ctx context.Context, r io.Reader, ch chan<- Chunk, full *strings.Builder) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20) // tolerate long SSE lines
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "[DONE]" {
			break
		}
		var d streamDelta
		if json.Unmarshal([]byte(payload), &d) != nil || len(d.Choices) == 0 {
			continue
		}
		tok := d.Choices[0].Delta.Content
		if tok == "" {
			continue
		}
		full.WriteString(tok)
		select {
		case ch <- Chunk{Kind: ChunkToken, Text: tok}:
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", e.cfg.Name, ctx.Err())
		}
	}
	return sc.Err()
}

// readAnthropicSSE parses an Anthropic Messages event stream, emitting a
// ChunkToken per text_delta and accumulating the full text into full.
func (e *LLMEngine) readAnthropicSSE(ctx context.Context, r io.Reader, ch chan<- Chunk, full *strings.Builder) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // ignore "event:" and blank lines
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "" {
			continue
		}
		var ev anthropicStreamEvent
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		if ev.Error != nil {
			return fmt.Errorf("%s: %s", e.cfg.Name, ev.Error.Message)
		}
		if ev.Type != "content_block_delta" || ev.Delta.Type != "text_delta" || ev.Delta.Text == "" {
			continue
		}
		full.WriteString(ev.Delta.Text)
		select {
		case ch <- Chunk{Kind: ChunkToken, Text: ev.Delta.Text}:
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", e.cfg.Name, ctx.Err())
		}
	}
	return sc.Err()
}

// httpError reads an error body (best effort) into a descriptive error.
func (e *LLMEngine) httpError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("%s: http %d: %s", e.cfg.Name, resp.StatusCode, msg)
}
