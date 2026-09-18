package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Exercise the transport boundary as well as finalization: whitespace-only
// deltas, leading code indentation, and Markdown hard breaks must survive both
// token streaming and the final result returned to JSON/history consumers.
func TestLLMDocumentWhitespaceAcrossTransports(t *testing.T) {
	transports := []struct {
		name       string
		path       string
		translate  func(*LLMEngine, context.Context, Request, context.CancelFunc, string) (<-chan Chunk, error)
		completion string
		delta      string
		end        string
	}{
		{
			name: "chat", path: "/chat/completions", translate: (*LLMEngine).translateOpenAI,
			completion: `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`,
			delta:      `{"choices":[{"delta":{"content":%s}}]}`,
			end:        `{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		},
		{
			name: "anthropic", path: "/messages", translate: (*LLMEngine).translateAnthropic,
			completion: `{"content":[{"type":"text","text":%s}],"stop_reason":"end_turn"}`,
			delta:      `{"type":"content_block_delta","delta":{"type":"text_delta","text":%s}}`,
			end:        `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		},
		{
			name: "responses", path: "/responses", translate: (*LLMEngine).translateResponses,
			completion: `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":%s}]}]}`,
			delta:      `{"type":"response.output_text.delta","delta":%s}`,
			end:        `{"type":"response.completed","response":{"status":"completed"}}`,
		},
	}
	const input = "\n    printf 'hello'  \n\n# Result\n\n- Complete  \n\n"
	tokens := []string{"\n    ", "printf 'hello'  \n\n# 結果\n\n- 已完成", "  \n\n"}
	full := strings.Join(tokens, "")
	for _, transport := range transports {
		for _, stream := range []bool{false, true} {
			for _, preserve := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%v/preserve=%v", transport.name, stream, preserve), func(t *testing.T) {
					var requests atomic.Int32
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						if r.URL.Path != transport.path {
							t.Errorf("request path=%q, want %q", r.URL.Path, transport.path)
						}
						var wire struct {
							Instructions string        `json:"instructions"`
							Input        string        `json:"input"`
							System       string        `json:"system"`
							Messages     []chatMessage `json:"messages"`
							Stream       bool          `json:"stream"`
						}
						if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
							t.Errorf("decode request: %v", err)
							http.Error(w, "invalid request", http.StatusBadRequest)
							return
						}
						if wire.Stream != stream {
							t.Errorf("request streaming=%v, want %v", wire.Stream, stream)
						}
						system, user := wire.System, wire.Input
						if wire.Instructions != "" {
							system = wire.Instructions
						}
						for _, message := range wire.Messages {
							switch message.Role {
							case "system":
								system = message.Content
							case "user":
								user = message.Content
							}
						}
						if !strings.HasSuffix(user, input) {
							t.Errorf("request lost document whitespace: %q", user)
						}
						if got := strings.Contains(system, "professional document translation engine"); got != preserve {
							t.Errorf("document prompt selected=%v, want %v", got, preserve)
						}
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							for _, token := range tokens {
								encoded, _ := json.Marshal(token)
								_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(transport.delta, encoded))
							}
							_, _ = fmt.Fprintf(w, "data: %s\n\n", transport.end)
						} else {
							w.Header().Set("Content-Type", "application/json")
							encoded, _ := json.Marshal(full)
							_, _ = fmt.Fprintf(w, transport.completion, encoded)
						}
					}))
					defer srv.Close()
					e := NewLLM(LLMConfig{Name: "test", BaseURL: srv.URL})
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					ch, err := transport.translate(e, ctx, Request{
						Text: input, Source: "en", Target: "zh-TW", Mode: ModeTranslate,
						Stream: stream, PreserveFormat: preserve, Preset: PresetContextual,
					}, cancel, "test-model")
					if err != nil {
						t.Fatal(err)
					}
					var streamed strings.Builder
					res, err := Drain(ch, func(token string) { streamed.WriteString(token) })
					if err != nil {
						t.Fatal(err)
					}
					want := full
					if !preserve {
						want = strings.TrimSpace(want)
					}
					if res.Translation != want {
						t.Errorf("translation=%q, want %q", res.Translation, want)
					}
					if stream && streamed.String() != full {
						t.Errorf("streamed=%q, want %q", streamed.String(), full)
					}
					if !stream && streamed.Len() != 0 {
						t.Errorf("non-streaming response emitted tokens: %q", streamed.String())
					}
					if res.Truncated {
						t.Error("complete document response marked truncated")
					}
					if got := requests.Load(); got != 1 {
						t.Errorf("document translation made %d requests, want 1", got)
					}
				})
			}
		}
	}
}
