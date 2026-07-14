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
	"sort"
	"strings"
	"time"

	"github.com/daviddwlee84/translate/internal/lang"
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
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// streamDelta is one OpenAI SSE chunk (stream:true). FinishReason is non-nil
// only on the terminal chunk ("stop" on a clean finish, "length" on a cap hit).
type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// --- Anthropic Messages API wire types (/v1/messages) ---

const anthropicMaxTokens = 4096

// learnMaxTokens is a larger cap for learn mode: a gloss-rich structured JSON reply
// can exceed the terse-translation budget, and a truncated JSON body fails to parse.
const learnMaxTokens = 8192

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
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
		// StopReason is carried on the terminal "message_delta" event
		// ("end_turn"/"stop_sequence" on a clean finish, "max_tokens" on a cap
		// hit); empty on "text_delta" events.
		StopReason string `json:"stop_reason"`
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
	// Apply a per-request model override only when it targets this provider, so a
	// copilot model id never reaches an Ollama fallback (which would 404).
	if req.Model != "" && (req.ModelProvider == "" || req.ModelProvider == e.cfg.Name) {
		model = NormalizeModelID(req.Model)
	}
	if isAnthropicModel(model) {
		return e.translateAnthropic(ctx, req, model)
	}
	return e.translateOpenAI(ctx, req, model)
}

// ModelLister is implemented by engines that can enumerate their models.
type ModelLister interface {
	Models(ctx context.Context) ([]string, error)
}

type modelsResponse struct {
	Data []struct {
		ID                 string   `json:"id"`
		SupportedEndpoints []string `json:"supported_endpoints"`
	} `json:"data"`
}

// Models fetches the provider's model ids, keeping only those usable through the
// transports this engine speaks: OpenAI /chat/completions, or (for claude-*) the
// Anthropic /v1/messages endpoint. Ids needing only /responses are dropped.
func (e *LLMEngine) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint("/models"), nil)
	if err != nil {
		return nil, err
	}
	e.auth(req)
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, e.httpError(resp)
	}
	var mr modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range mr.Data {
		if usableModel(m.ID, m.SupportedEndpoints) {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}

// usableModel reports whether a model id can be driven by this engine.
func usableModel(id string, endpoints []string) bool {
	if isAnthropicModel(id) {
		return true // routed via /v1/messages
	}
	for _, ep := range endpoints {
		if ep == "/chat/completions" {
			return true
		}
	}
	// No endpoint metadata (e.g. Ollama) => assume chat-usable.
	return len(endpoints) == 0
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

// promptFor picks the (system, user) prompt pair for a request: the structured
// tutor prompt for learn mode, else the plain translate prompt.
func promptFor(req Request) (system, user string) {
	if req.Learn {
		return buildLearnPrompt(req)
	}
	return buildTranslatePrompt(req)
}

// finalizeResult builds the terminal result, parsing structured learn output when
// the request asked for it.
func (e *LLMEngine) finalizeResult(full, model string, req Request) *TranslateResult {
	if req.Learn {
		return e.finalizeLearn(full, model, req)
	}
	return e.finalize(full, model, req)
}

// finalizeLearn parses the model's structured JSON reply into a LearnResult. It is
// defensive — it strips a markdown fence and slices to the outermost object before
// unmarshalling — and on any failure falls back to the raw text as the translation
// (with a warning), so a malformed reply degrades gracefully instead of erroring.
// res.Target is set to the FOREIGN (away) language because res.Translation always
// holds the foreign sentence (the corrected sentence, or the translation).
func (e *LLMEngine) finalizeLearn(full, model string, req Request) *TranslateResult {
	res := &TranslateResult{
		Target: req.PairAway,
		Engine: e.cfg.Name,
		Model:  model,
	}
	if src := strings.TrimSpace(req.Source); src == "" || src == "auto" {
		res.DetectedSource = lang.Detect(req.Text)
	}
	lr, err := parseLearn(full)
	if err != nil {
		res.Translation = strings.TrimSpace(full)
		res.Warnings = append(res.Warnings, "learn: could not parse structured output — showing the raw reply")
		return res
	}
	lr.Direction = learnDirection(req) // trust offline detection over the model's guess
	if strings.TrimSpace(lr.Original) == "" {
		lr.Original = req.Text
	}
	res.Learn = lr
	if lr.Direction == "correct" && strings.TrimSpace(lr.Corrected) != "" {
		res.Translation = strings.TrimSpace(lr.Corrected)
	} else {
		res.Translation = strings.TrimSpace(lr.Translation)
	}
	return res
}

// parseLearn extracts and unmarshals a LearnResult from a model reply, returning an
// error when the JSON is absent/invalid or carries no main sentence.
func parseLearn(full string) (*LearnResult, error) {
	var lr LearnResult
	if err := json.Unmarshal([]byte(extractJSON(full)), &lr); err != nil {
		return nil, err
	}
	if strings.TrimSpace(lr.Translation) == "" && strings.TrimSpace(lr.Corrected) == "" {
		return nil, fmt.Errorf("learn: empty structured result")
	}
	return &lr, nil
}

// extractJSON returns the outermost JSON object in s, tolerating a surrounding
// markdown code fence or stray prose around it.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") { // strip ```json … ``` fence
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// markTruncated flags a result whose stream ended before the model finished, so
// the caller keeps the partial text but never treats it as a complete answer.
func markTruncated(res *TranslateResult) {
	res.Truncated = true
	res.Warnings = append(res.Warnings,
		"output was cut off before completion (stream truncated) — press Enter to retry")
}

// translateOpenAI uses the OpenAI /chat/completions endpoint.
func (e *LLMEngine) translateOpenAI(ctx context.Context, req Request, model string) (<-chan Chunk, error) {
	system, user := promptFor(req)
	stream := req.Stream && !req.Learn // learn output is structured JSON: parse at done
	body := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: stream,
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
	if stream {
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
		complete := true
		if stream {
			var err error
			complete, err = e.readSSE(ctx, resp.Body, ch, &full)
			if err != nil {
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
			complete = cr.Choices[0].FinishReason != "length" // "length" => hit the cap
		}
		res := e.finalizeResult(full.String(), model, req)
		if !complete {
			markTruncated(res)
		}
		ch <- Chunk{Kind: ChunkDone, Result: res}
	}()
	return ch, nil
}

// translateAnthropic uses the Anthropic Messages API (/v1/messages).
func (e *LLMEngine) translateAnthropic(ctx context.Context, req Request, model string) (<-chan Chunk, error) {
	system, user := promptFor(req)
	stream := req.Stream && !req.Learn // learn output is structured JSON: parse at done
	maxTokens := anthropicMaxTokens
	if req.Learn {
		maxTokens = learnMaxTokens
	}
	body := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
		Stream:    stream,
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
	if stream {
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
		complete := true
		if stream {
			var err error
			complete, err = e.readAnthropicSSE(ctx, resp.Body, ch, &full)
			if err != nil {
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
			complete = ar.StopReason != "max_tokens" // "max_tokens" => hit the cap
		}
		res := e.finalizeResult(full.String(), model, req)
		if !complete {
			markTruncated(res)
		}
		ch <- Chunk{Kind: ChunkDone, Result: res}
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
// delta and accumulating the full text into full. It reports complete=true only
// when the stream ended on a terminal marker ([DONE] or finish_reason=="stop");
// a stream that closes with no marker, or on finish_reason=="length", is treated
// as truncated so the caller can flag it rather than silently accept partial text.
func (e *LLMEngine) readSSE(ctx context.Context, r io.Reader, ch chan<- Chunk, full *strings.Builder) (bool, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20) // tolerate long SSE lines
	sawDone := false
	finish := ""
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var d streamDelta
		if json.Unmarshal([]byte(payload), &d) != nil || len(d.Choices) == 0 {
			continue
		}
		if fr := d.Choices[0].FinishReason; fr != nil && *fr != "" {
			finish = *fr
		}
		tok := d.Choices[0].Delta.Content
		if tok == "" {
			continue
		}
		full.WriteString(tok)
		select {
		case ch <- Chunk{Kind: ChunkToken, Text: tok}:
		case <-ctx.Done():
			return false, fmt.Errorf("%s: %w", e.cfg.Name, ctx.Err())
		}
	}
	if err := sc.Err(); err != nil {
		return false, fmt.Errorf("%s: %w", e.cfg.Name, err)
	}
	complete := finish != "length" && (sawDone || finish == "stop")
	return complete, nil
}

// readAnthropicSSE parses an Anthropic Messages event stream, emitting a
// ChunkToken per text_delta and accumulating the full text into full. It reports
// complete=true only when a terminal marker arrived (a message_stop event, or a
// message_delta with a natural stop_reason); a stream that closes with neither,
// or on stop_reason=="max_tokens", is treated as truncated.
func (e *LLMEngine) readAnthropicSSE(ctx context.Context, r io.Reader, ch chan<- Chunk, full *strings.Builder) (bool, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	sawStop := false
	stopReason := ""
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
			return false, fmt.Errorf("%s: %s", e.cfg.Name, ev.Error.Message)
		}
		switch ev.Type {
		case "message_stop":
			sawStop = true
			continue
		case "message_delta":
			if ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			continue
		}
		if ev.Type != "content_block_delta" || ev.Delta.Type != "text_delta" || ev.Delta.Text == "" {
			continue
		}
		full.WriteString(ev.Delta.Text)
		select {
		case ch <- Chunk{Kind: ChunkToken, Text: ev.Delta.Text}:
		case <-ctx.Done():
			return false, fmt.Errorf("%s: %w", e.cfg.Name, ctx.Err())
		}
	}
	if err := sc.Err(); err != nil {
		return false, fmt.Errorf("%s: %w", e.cfg.Name, err)
	}
	complete := stopReason != "max_tokens" &&
		(sawStop || stopReason == "end_turn" || stopReason == "stop_sequence")
	return complete, nil
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
