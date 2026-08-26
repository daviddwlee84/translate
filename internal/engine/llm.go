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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daviddwlee84/translate/internal/debug"
	"github.com/daviddwlee84/translate/internal/lang"
)

// LLMConfig parameterizes an OpenAI-compatible backend (copilot-proxy, Ollama,
// OpenRouter, LiteLLM, or a generic base_url+key endpoint).
type LLMConfig struct {
	Name      string // "copilot", "ollama", "openrouter", ...
	BaseURL   string // e.g. "http://localhost:4141/v1"
	Model     string // e.g. "claude-sonnet-5"
	APIKeyEnv string // env var holding the key; "" => no Authorization header
	// AutoModel consults the live model catalog before a request. When Model is
	// no longer served (common for copilot-proxy entitlement/catalog changes),
	// the strongest model in Tier usable by this engine is selected automatically.
	AutoModel bool
	Tier      string // default | fast | max; used only by AutoModel fallback
	// Timeout is the whole-request ceiling (0 => 10m). It exists to stop a wedged
	// connection hanging a front-end, NOT to bound generation: a document-sized
	// translation legitimately runs for minutes.
	Timeout time.Duration
}

// LLMEngine talks to any OpenAI-compatible /chat/completions endpoint.
type LLMEngine struct {
	cfg  LLMConfig
	key  string
	http *http.Client
}

// defaultRequestTimeout is the whole-request ceiling when the config sets none.
// Generous on purpose: it is a backstop for a wedged connection, and a
// document-sized translation legitimately takes minutes.
const defaultRequestTimeout = 10 * time.Minute

// NewLLM builds an LLM engine, resolving the API key once from the environment.
func NewLLM(cfg LLMConfig) *LLMEngine {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultRequestTimeout
	}
	key := ""
	if cfg.APIKeyEnv != "" {
		key = strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
	}
	// No http.Client.Timeout, and no Transport.ResponseHeaderTimeout either.
	//
	// Client.Timeout covers the *entire* request including reading the body, so a
	// streamed translation was cut off at exactly that deadline mid-sentence (see
	// pitfalls/llm-stream-truncation-silently-rendered-as-complete.md).
	// ResponseHeaderTimeout looks like the right replacement and is not:
	// copilot-proxy buffers Claude /v1/messages responses and sends no headers
	// until generation finishes, so a header deadline fails exactly the long
	// documents this is meant to fix.
	//
	// The bound is the per-request context in Translate instead. Probes set their
	// own short deadlines (Available 800ms, Models 4s) and are unaffected.
	return &LLMEngine{cfg: cfg, key: key, http: &http.Client{}}
}

// Name returns the provider name (e.g. "copilot").
func (e *LLMEngine) Name() string { return e.cfg.Name }

// Model returns the model id this engine was built with (e.g. "claude-sonnet-5").
func (e *LLMEngine) Model() string { return e.cfg.Model }

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

// --- OpenAI Responses API wire types (/v1/responses) ---

type responsesRequest struct {
	Model           string `json:"model"`
	Instructions    string `json:"instructions,omitempty"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	Stream          bool   `json:"stream"`
}

type responsesResponse struct {
	Status            string `json:"status"`
	IncompleteDetails any    `json:"incomplete_details"`
	Output            []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type responsesStreamEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Response *responsesResponse `json:"response"`
}

// --- Anthropic Messages API wire types (/v1/messages) ---

// anthropicMaxTokens is the *floor* for the output cap, enough for any short
// translation. Long input scales above it — see outputTokenBudget.
const anthropicMaxTokens = 4096

// learnMaxTokens is a larger cap for learn mode: a gloss-rich structured JSON reply
// can exceed the terse-translation budget, and a truncated JSON body fails to parse.
const learnMaxTokens = 8192

// maxOutputTokens caps the scaled budget. Well inside any current Claude model's
// output limit, while still covering a document-sized translation.
const maxOutputTokens = 32768

// outputTokenBudget sizes max_tokens for the input rather than using one fixed
// cap. A translation is roughly as long as its source, so a fixed 4096 silently
// truncated anything past a few thousand characters — measured: 10 KB of English
// prose produced 5368 Chinese characters and was cut off mid-document.
//
// The estimate is deliberately generous: max_tokens is only a ceiling, so
// over-estimating costs nothing, while under-estimating loses the tail of the
// answer. 1.5 tokens per input rune covers en→CJK, the expensive direction (CJK
// output runs close to one token per character).
func outputTokenBudget(text string, floor int) int {
	want := utf8.RuneCountInString(text) * 3 / 2
	if want < floor {
		return floor
	}
	if want > maxOutputTokens {
		return maxOutputTokens
	}
	return want
}

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

type modelTransport int

const (
	transportChat modelTransport = iota
	transportAnthropic
	transportResponses
)

func inferTransport(model string) modelTransport {
	if isAnthropicModel(model) {
		return transportAnthropic
	}
	return transportChat
}

// Translate performs a translation, streaming tokens when req.Stream is set.
// It always returns a channel that closes after exactly one terminal chunk, and
// dispatches to Anthropic Messages, OpenAI Responses, or Chat Completions based
// on the selected model's live endpoint metadata.
func (e *LLMEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	// A bilingual doc request carries its content in Segments, not Text.
	if strings.TrimSpace(req.Text) == "" && !(req.Bilingual && len(req.Segments) > 0) {
		return nil, ErrEmptyInput
	}
	model := NormalizeModelID(e.cfg.Model)
	// Apply a per-request model override only when it targets this provider, so a
	// copilot model id never reaches an Ollama fallback (which would 404).
	if req.Model != "" && (req.ModelProvider == "" || req.ModelProvider == e.cfg.Name) {
		model = NormalizeModelID(req.Model)
	}
	transport := inferTransport(model)
	if e.cfg.AutoModel {
		model, transport = e.resolveModel(ctx, model)
	}
	// A backstop for a wedged connection. The goroutines below own the cancel:
	// they run past this function's return, so cancelling here would kill the
	// stream immediately.
	ctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	switch transport {
	case transportAnthropic:
		return e.translateAnthropic(ctx, req, cancel, model)
	case transportResponses:
		return e.translateResponses(ctx, req, cancel, model)
	default:
		return e.translateOpenAI(ctx, req, cancel, model)
	}
}

// resolveModel keeps the requested model when it is live and transport-usable;
// otherwise it picks the best usable model from the provider's current catalog.
// Catalog failures are non-fatal: retaining the configured id preserves the
// existing request/chain fallback behavior when the proxy itself is down.
func (e *LLMEngine) resolveModel(ctx context.Context, requested string) (string, modelTransport) {
	probeCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	catalog, err := e.modelCatalog(probeCtx)
	if err != nil || len(catalog) == 0 {
		return requested, inferTransport(requested)
	}
	models := make([]string, 0, len(catalog))
	for _, model := range catalog {
		if !usableModel(model.ID, model.SupportedEndpoints) {
			continue
		}
		models = append(models, model.ID)
		if strings.EqualFold(NormalizeModelID(model.ID), requested) {
			return NormalizeModelID(model.ID), transportForModel(model.ID, model.SupportedEndpoints)
		}
	}
	selected := pickBestModel(models, e.cfg.Tier)
	if selected == "" {
		return requested, inferTransport(requested)
	}
	for _, model := range catalog {
		if strings.EqualFold(NormalizeModelID(model.ID), selected) {
			debug.Logf("%s: model %q is not in the live usable catalog; auto-selected %q for tier %q", e.cfg.Name, requested, selected, e.cfg.Tier)
			return selected, transportForModel(model.ID, model.SupportedEndpoints)
		}
	}
	return selected, inferTransport(selected)
}

// ModelLister is implemented by engines that can enumerate their models.
type ModelLister interface {
	Models(ctx context.Context) ([]string, error)
}

type modelInfo struct {
	ID                 string   `json:"id"`
	SupportedEndpoints []string `json:"supported_endpoints"`
}

type modelsResponse struct {
	Data []modelInfo `json:"data"`
}

// Models fetches the provider's model ids, keeping only those usable through the
// transports this engine speaks: OpenAI /chat/completions, OpenAI /responses, or
// (for claude-*) the Anthropic /v1/messages endpoint.
func (e *LLMEngine) Models(ctx context.Context) ([]string, error) {
	catalog, err := e.modelCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, model := range catalog {
		if usableModel(model.ID, model.SupportedEndpoints) {
			out = append(out, model.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (e *LLMEngine) modelCatalog(ctx context.Context) ([]modelInfo, error) {
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
	return mr.Data, nil
}

// usableModel reports whether a model id can be driven by this engine.
func usableModel(id string, endpoints []string) bool {
	if strings.Contains(strings.ToLower(id), "embedding") {
		return false
	}
	if isAnthropicModel(id) {
		return true // routed via /v1/messages
	}
	for _, ep := range endpoints {
		if ep == "/chat/completions" || ep == "/responses" {
			return true
		}
	}
	// No endpoint metadata (e.g. Ollama) => assume chat-usable.
	return len(endpoints) == 0
}

func transportForModel(id string, endpoints []string) modelTransport {
	if isAnthropicModel(id) {
		return transportAnthropic
	}
	hasChat, hasResponses := false, false
	for _, endpoint := range endpoints {
		switch endpoint {
		case "/chat/completions":
			hasChat = true
		case "/responses":
			hasResponses = true
		}
	}
	if hasChat {
		return transportChat
	}
	if hasResponses {
		return transportResponses
	}
	return transportChat
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
	switch {
	case req.Bilingual:
		return buildBilingualPrompt(req)
	case req.Learn:
		return buildLearnPrompt(req)
	default:
		return buildTranslatePrompt(req)
	}
}

// finalizeResult builds the terminal result, parsing structured learn output when
// the request asked for it.
func (e *LLMEngine) finalizeResult(full, model string, req Request) *TranslateResult {
	switch {
	case req.Bilingual:
		return e.finalizeBilingual(full, model, req)
	case req.Learn:
		return e.finalizeLearn(full, model, req)
	default:
		return e.finalize(full, model, req)
	}
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

// finalizeBilingual parses the model's JSON reply (prose-segment number → translation)
// into TranslateResult.Bilingual. Defensive like finalizeLearn: on any parse failure
// it returns an empty result + a warning so the caller can fall back to per-block mode.
func (e *LLMEngine) finalizeBilingual(full, model string, req Request) *TranslateResult {
	res := &TranslateResult{
		Target: req.Target,
		Engine: e.cfg.Name,
		Model:  model,
	}
	m, err := parseBilingual(full)
	if err != nil {
		res.Warnings = append(res.Warnings, "bilingual: could not parse structured output")
		return res
	}
	res.Bilingual = m
	return res
}

// parseBilingual extracts a {"<n>": "<translation>"} JSON object from a model reply
// and converts it to a map keyed by the integer segment number, ignoring non-numeric
// keys defensively.
func parseBilingual(full string) (map[int]string, error) {
	var raw map[string]string
	if err := json.Unmarshal([]byte(extractJSON(full)), &raw); err != nil {
		return nil, err
	}
	out := make(map[int]string, len(raw))
	for k, v := range raw {
		n, err := strconv.Atoi(strings.TrimSpace(k))
		if err != nil {
			continue
		}
		out[n] = v
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bilingual: no numeric segment keys")
	}
	return out, nil
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

func responseOutputText(response *responsesResponse) string {
	if response == nil {
		return ""
	}
	var full strings.Builder
	for _, output := range response.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "output_text" {
				full.WriteString(content.Text)
			}
		}
	}
	return full.String()
}

// translateResponses uses the OpenAI Responses API. copilot-proxy exposes its
// newest GPT tiers (including gpt-5.6-sol/terra/luna) only on this endpoint.
func (e *LLMEngine) translateResponses(ctx context.Context, req Request, cancel context.CancelFunc, model string) (<-chan Chunk, error) {
	ok := false
	defer func() {
		if !ok {
			cancel()
		}
	}()
	system, user := promptFor(req)
	stream := req.Stream && !req.Learn && !req.Bilingual
	floor := anthropicMaxTokens
	if req.Learn || req.Bilingual {
		floor = learnMaxTokens
	}
	body := responsesRequest{
		Model:           model,
		Instructions:    system,
		Input:           user,
		MaxOutputTokens: outputTokenBudget(user, floor),
		Stream:          stream,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint("/responses"), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	e.auth(httpReq)

	ch := make(chan Chunk, 32)
	ok = true
	go func() {
		defer cancel()
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
			complete, err = e.readResponsesSSE(ctx, resp.Body, ch, &full)
			if err != nil {
				ch <- Chunk{Kind: ChunkError, Err: err}
				return
			}
		} else {
			var response responsesResponse
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: decode response: %w", e.cfg.Name, err)}
				return
			}
			if response.Error != nil {
				ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: %s", e.cfg.Name, response.Error.Message)}
				return
			}
			full.WriteString(responseOutputText(&response))
			complete = response.Status != "incomplete"
		}
		if full.Len() == 0 {
			ch <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%s: empty response", e.cfg.Name)}
			return
		}
		res := e.finalizeResult(full.String(), model, req)
		if !complete {
			markTruncated(res)
		}
		ch <- Chunk{Kind: ChunkDone, Result: res}
	}()
	return ch, nil
}

func (e *LLMEngine) readResponsesSSE(ctx context.Context, r io.Reader, ch chan<- Chunk, full *strings.Builder) (bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	completed := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		switch event.Type {
		case "response.output_text.delta":
			if event.Delta == "" {
				continue
			}
			full.WriteString(event.Delta)
			select {
			case ch <- Chunk{Kind: ChunkToken, Text: event.Delta}:
			case <-ctx.Done():
				return false, fmt.Errorf("%s: %w", e.cfg.Name, ctx.Err())
			}
		case "response.completed":
			completed = true
			if full.Len() == 0 {
				full.WriteString(responseOutputText(event.Response))
			}
		case "response.incomplete":
			if full.Len() == 0 {
				full.WriteString(responseOutputText(event.Response))
			}
			return false, nil
		case "response.failed", "error":
			message := "responses request failed"
			if event.Error != nil && event.Error.Message != "" {
				message = event.Error.Message
			} else if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
				message = event.Response.Error.Message
			}
			return false, fmt.Errorf("%s: %s", e.cfg.Name, message)
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("%s: read stream: %w", e.cfg.Name, err)
	}
	return completed, nil
}

// translateOpenAI uses the OpenAI /chat/completions endpoint.
func (e *LLMEngine) translateOpenAI(ctx context.Context, req Request, cancel context.CancelFunc, model string) (<-chan Chunk, error) {
	// Any early return here abandons the context, so release it now; the
	// success path hands ownership to the goroutine below.
	ok := false
	defer func() {
		if !ok {
			cancel()
		}
	}()
	system, user := promptFor(req)
	stream := req.Stream && !req.Learn && !req.Bilingual // structured JSON output: parse at done
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
	ok = true
	go func() {
		defer cancel()
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
func (e *LLMEngine) translateAnthropic(ctx context.Context, req Request, cancel context.CancelFunc, model string) (<-chan Chunk, error) {
	// Any early return here abandons the context, so release it now; the
	// success path hands ownership to the goroutine below.
	ok := false
	defer func() {
		if !ok {
			cancel()
		}
	}()
	system, user := promptFor(req)
	stream := req.Stream && !req.Learn && !req.Bilingual // structured JSON output: parse at done
	floor := anthropicMaxTokens
	if req.Learn || req.Bilingual {
		floor = learnMaxTokens
	}
	body := anthropicRequest{
		Model:     model,
		MaxTokens: outputTokenBudget(user, floor),
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
	ok = true
	go func() {
		defer cancel()
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
