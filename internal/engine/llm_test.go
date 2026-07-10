package engine

import (
	"context"
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
