package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer returns a test server that replies to any request with body, as an
// event stream. The whole body is written at once — the SSE reader scans it line
// by line regardless, so completeness detection is exercised identically.
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runStream drives a streaming translation against a canned SSE body and returns
// the terminal result (via Drain).
func runStream(t *testing.T, model, body string) (*TranslateResult, error) {
	t.Helper()
	srv := sseServer(t, body)
	e := NewLLM(LLMConfig{Name: "test", BaseURL: srv.URL, Model: model})
	ch, err := e.Translate(context.Background(), Request{
		Text:   "hello",
		Source: "auto",
		Target: "zh",
		Mode:   ModeTranslate,
		Stream: true,
	})
	if err != nil {
		t.Fatalf("Translate setup error: %v", err)
	}
	return Drain(ch, nil)
}

// --- Anthropic (/v1/messages) completeness ---

const anthropicComplete = `event: message_start
data: {"type":"message_start"}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"世界"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

event: message_stop
data: {"type":"message_stop"}
`

// A dropped connection: text deltas arrive, then the stream just ends — no
// message_delta, no message_stop. This is the reported bug's fingerprint.
const anthropicTruncated = `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"世"}}
`

// A cap hit: the stream terminates cleanly (message_stop present) but the model
// was cut off, so stop_reason is "max_tokens".
const anthropicMaxTokensBody = `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"}}

event: message_stop
data: {"type":"message_stop"}
`

func TestAnthropicStreamCompleteness(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantText  string
		wantTrunc bool
	}{
		{"complete", anthropicComplete, "你好世界", false},
		{"dropped", anthropicTruncated, "你好世", true},
		{"max_tokens", anthropicMaxTokensBody, "你好", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runStream(t, "claude-sonnet-5", tc.body)
			if err != nil {
				t.Fatalf("Drain error: %v", err)
			}
			if res.Translation != tc.wantText {
				t.Errorf("Translation = %q, want %q (partial text must be preserved)", res.Translation, tc.wantText)
			}
			if res.Truncated != tc.wantTrunc {
				t.Errorf("Truncated = %v, want %v", res.Truncated, tc.wantTrunc)
			}
			if tc.wantTrunc && len(res.Warnings) == 0 {
				t.Errorf("truncated result must carry a warning, got none")
			}
			if !tc.wantTrunc && len(res.Warnings) != 0 {
				t.Errorf("complete result must carry no warnings, got %v", res.Warnings)
			}
		})
	}
}

// --- OpenAI (/chat/completions) completeness ---

const openaiComplete = `data: {"choices":[{"index":0,"delta":{"content":"你好"}}]}

data: {"choices":[{"index":0,"delta":{"content":"世界"}}]}

data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}

data: [DONE]
`

// Dropped: content chunks then the stream ends — no finish_reason, no [DONE].
const openaiTruncated = `data: {"choices":[{"index":0,"delta":{"content":"你好"}}]}

data: {"choices":[{"index":0,"delta":{"content":"世"}}]}
`

// Cap hit: [DONE] arrives, but finish_reason is "length".
const openaiLength = `data: {"choices":[{"index":0,"delta":{"content":"你好"}}]}

data: {"choices":[{"index":0,"finish_reason":"length","delta":{}}]}

data: [DONE]
`

func TestOpenAIStreamCompleteness(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantText  string
		wantTrunc bool
	}{
		{"complete", openaiComplete, "你好世界", false},
		{"dropped", openaiTruncated, "你好世", true},
		{"length", openaiLength, "你好", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runStream(t, "gemini-3.5-flash", tc.body)
			if err != nil {
				t.Fatalf("Drain error: %v", err)
			}
			if res.Translation != tc.wantText {
				t.Errorf("Translation = %q, want %q (partial text must be preserved)", res.Translation, tc.wantText)
			}
			if res.Truncated != tc.wantTrunc {
				t.Errorf("Truncated = %v, want %v", res.Truncated, tc.wantTrunc)
			}
			if tc.wantTrunc && len(res.Warnings) == 0 {
				t.Errorf("truncated result must carry a warning, got none")
			}
		})
	}
}

func TestResponsesStreamCompleteness(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantText  string
		wantClean bool
	}{
		{
			name: "complete",
			body: `event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"你好"}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"世界"}

event: response.completed
data: {"type":"response.completed","response":{"status":"completed"}}
`,
			wantText:  "你好世界",
			wantClean: true,
		},
		{
			name: "dropped",
			body: `event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"你好"}
`,
			wantText:  "你好",
			wantClean: false,
		},
		{
			name: "incomplete",
			body: `event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"你好"}

event: response.incomplete
data: {"type":"response.incomplete","response":{"status":"incomplete"}}
`,
			wantText:  "你好",
			wantClean: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewLLM(LLMConfig{Name: "copilot", BaseURL: "http://127.0.0.1:1", Model: "gpt-5.6-terra"})
			chunks := make(chan Chunk, 8)
			var full strings.Builder
			clean, err := e.readResponsesSSE(context.Background(), strings.NewReader(tc.body), chunks, &full)
			if err != nil {
				t.Fatalf("readResponsesSSE: %v", err)
			}
			if clean != tc.wantClean {
				t.Fatalf("complete = %v, want %v", clean, tc.wantClean)
			}
			if full.String() != tc.wantText {
				t.Fatalf("text = %q, want %q", full.String(), tc.wantText)
			}
		})
	}
}

// A mid-stream Anthropic error event must surface as a terminal error, not a
// silently-truncated success.
func TestAnthropicStreamErrorEvent(t *testing.T) {
	body := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: error
data: {"type":"error","error":{"message":"overloaded"}}
`
	res, err := runStream(t, "claude-sonnet-5", body)
	if err == nil {
		t.Fatalf("expected a terminal error, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "overloaded") {
		t.Errorf("error = %v, want it to mention the upstream message", err)
	}
}

// --- Non-streaming completeness (piped CLI / --json) ---

// runOneShot drives a non-streaming translation (Stream:false) against a canned
// JSON body and returns the terminal result.
func runOneShot(t *testing.T, model, body string) (*TranslateResult, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	e := NewLLM(LLMConfig{Name: "test", BaseURL: srv.URL, Model: model})
	ch, err := e.Translate(context.Background(), Request{
		Text: "hello", Source: "auto", Target: "zh", Mode: ModeTranslate, Stream: false,
	})
	if err != nil {
		t.Fatalf("Translate setup error: %v", err)
	}
	return Drain(ch, nil)
}

func TestNonStreamCompleteness(t *testing.T) {
	cases := []struct {
		name      string
		model     string
		body      string
		wantTrunc bool
	}{
		{"anthropic ok", "claude-sonnet-5",
			`{"content":[{"type":"text","text":"你好世界"}],"stop_reason":"end_turn"}`, false},
		{"anthropic max_tokens", "claude-sonnet-5",
			`{"content":[{"type":"text","text":"你好"}],"stop_reason":"max_tokens"}`, true},
		{"openai ok", "gemini-3.5-flash",
			`{"choices":[{"message":{"role":"assistant","content":"你好世界"},"finish_reason":"stop"}]}`, false},
		{"openai length", "gemini-3.5-flash",
			`{"choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"length"}]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runOneShot(t, tc.model, tc.body)
			if err != nil {
				t.Fatalf("Drain error: %v", err)
			}
			if res.Truncated != tc.wantTrunc {
				t.Errorf("Truncated = %v, want %v", res.Truncated, tc.wantTrunc)
			}
		})
	}
}

// outputTokenBudget sizes max_tokens for the input instead of using one fixed
// cap. A flat 4096 silently truncated long documents: 10 KB of English prose
// produced 5368 Chinese characters and was cut off mid-document.
func TestOutputTokenBudget(t *testing.T) {
	const floor = anthropicMaxTokens

	if got := outputTokenBudget("", floor); got != floor {
		t.Fatalf("empty input should get the floor, got %d", got)
	}
	if got := outputTokenBudget("hello", floor); got != floor {
		t.Fatalf("short input should get the floor, got %d", got)
	}

	// The case that used to truncate: 10k runes needs more than the old 4096.
	long := strings.Repeat("a", 10_000)
	if got := outputTokenBudget(long, floor); got <= floor {
		t.Fatalf("10k runes should scale above the floor, got %d", got)
	}

	// Never above the ceiling, however big the input.
	huge := strings.Repeat("a", 10_000_000)
	if got := outputTokenBudget(huge, floor); got != maxOutputTokens {
		t.Fatalf("want the ceiling %d, got %d", maxOutputTokens, got)
	}

	// Runes, not bytes: CJK is 3 bytes per character and is the direction that
	// needs the most output tokens, so counting bytes would over-scale wildly
	// while counting runes tracks the real cost.
	cjk := strings.Repeat("貓", 10_000)
	if got, want := outputTokenBudget(cjk, floor), outputTokenBudget(long, floor); got != want {
		t.Fatalf("10k CJK runes = %d, 10k ASCII runes = %d; should match", got, want)
	}

	// A larger floor (learn/bilingual) is respected.
	if got := outputTokenBudget("hi", learnMaxTokens); got != learnMaxTokens {
		t.Fatalf("learn floor not honored, got %d", got)
	}
}

// The HTTP client must carry no whole-request deadline: Go's Client.Timeout also
// caps reading a streamed body, which cut long translations off mid-sentence.
func TestNewLLMHasNoClientTimeout(t *testing.T) {
	e := NewLLM(LLMConfig{Name: "x", BaseURL: "http://127.0.0.1:1", Model: "claude-sonnet-5"})
	if e.http.Timeout != 0 {
		t.Fatalf("http.Client.Timeout = %v, want 0 (it would truncate streams)", e.http.Timeout)
	}
	if e.cfg.Timeout != defaultRequestTimeout {
		t.Fatalf("cfg.Timeout = %v, want the %v default", e.cfg.Timeout, defaultRequestTimeout)
	}
}

func TestCopilotAutoModelReplacesUnavailablePreference(t *testing.T) {
	var requestedModel, requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"id":"gpt-5.6-sol","supported_endpoints":["/responses"]},
				{"id":"gpt-5.6-terra","supported_endpoints":["/responses"]},
				{"id":"gpt-5.4","supported_endpoints":["/responses","/chat/completions"]},
				{"id":"gemini-3.1-pro-preview","supported_endpoints":["/chat/completions"]},
				{"id":"text-embedding-3-small","supported_endpoints":[]}
			]}`))
		case "/responses":
			requestedPath = r.URL.Path
			var body responsesRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode responses request: %v", err)
			}
			requestedModel = body.Model
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"恐龍"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	e := NewLLM(LLMConfig{
		Name: "copilot", BaseURL: srv.URL, Model: "claude-sonnet-5", AutoModel: true, Tier: "default",
	})
	ch, err := e.Translate(context.Background(), Request{
		Text: "dinosaur", Source: "auto", Target: "zh-TW", Mode: ModeTranslate,
	})
	if err != nil {
		t.Fatalf("Translate setup: %v", err)
	}
	res, err := Drain(ch, nil)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if requestedPath != "/responses" {
		t.Fatalf("request path = %q, want /responses", requestedPath)
	}
	if requestedModel != "gpt-5.6-terra" {
		t.Fatalf("request model = %q, want gpt-5.6-terra", requestedModel)
	}
	if res.Model != "gpt-5.6-terra" {
		t.Fatalf("result model = %q, want gpt-5.6-terra", res.Model)
	}
}

func TestCopilotAutoModelKeepsLivePreference(t *testing.T) {
	var requestedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"id":"gpt-5.4","supported_endpoints":["/chat/completions"]},
				{"id":"gpt-5-mini","supported_endpoints":["/chat/completions"]}
			]}`))
		case "/chat/completions":
			var body chatRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode chat request: %v", err)
			}
			requestedModel = body.Model
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	e := NewLLM(LLMConfig{
		Name: "copilot", BaseURL: srv.URL, Model: "gpt-5-mini", AutoModel: true,
	})
	ch, err := e.Translate(context.Background(), Request{
		Text: "hello", Source: "auto", Target: "zh-TW", Mode: ModeTranslate,
	})
	if err != nil {
		t.Fatalf("Translate setup: %v", err)
	}
	if _, err := Drain(ch, nil); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if requestedModel != "gpt-5-mini" {
		t.Fatalf("request model = %q, want configured live model gpt-5-mini", requestedModel)
	}
}
