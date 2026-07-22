package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/daviddwlee84/translate/internal/appcore"
)

// translateStream serves GET /v1/translate/stream as Server-Sent Events. It is a
// GET (browser EventSource is GET-only) taking query params, and emits:
//
//	event: token    data: "<json-encoded token>"   (zero or more)
//	event: warning  data: "<message>"              (only if the result was truncated)
//	event: done     data: <TranslateResult JSON>   (terminal, success)
//	event: error    data: "<message>"              (terminal, failure)
//
// Payloads are always JSON so multi-line tokens survive SSE's line framing.
//
// Note: visible token cadence depends on the upstream provider — copilot-proxy
// buffers Claude /v1/messages, so Claude models still arrive in a burst regardless
// of this transport. SSE's win is dict/google/OpenAI-style providers and fan-out.
func (h *handlers) translateStream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text := strings.TrimSpace(q.Get("text"))
	if text == "" {
		writeError(w, http.StatusUnprocessableEntity, "empty_input", `query param "text" is required`)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_flush", "streaming is not supported by this server")
		return
	}

	p := appcore.Params{
		Text:         text,
		Source:       q.Get("source"),
		Target:       q.Get("target"),
		Preset:       q.Get("preset"),
		Instructions: q.Get("instructions"),
		Model:        q.Get("model"),
		Pair:         parseBoolPtr(q.Get("pair")),
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
	w.WriteHeader(http.StatusOK)
	flusher.Flush() // commit headers so the client opens the stream immediately

	onToken := func(tok string) {
		writeSSEString(w, "token", tok)
		flusher.Flush()
	}
	// r.Context() cancels on client disconnect and is threaded through the engine,
	// aborting the upstream LLM request — no extra plumbing needed.
	res, err := h.svc.TranslateStream(r.Context(), p, onToken)
	if err != nil {
		writeSSEString(w, "error", err.Error())
		flusher.Flush()
		return
	}
	// Truncated is transient (json:"-"), so it would vanish from the done payload —
	// surface it explicitly as a warning before the terminal event.
	if res.Truncated {
		writeSSEString(w, "warning", "result was truncated (max_tokens); output is partial")
		flusher.Flush()
	}
	writeSSEJSON(w, "done", res)
	flusher.Flush()
}

// writeSSE writes one framed SSE event with an already-JSON data payload.
func writeSSE(w io.Writer, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// writeSSEString frames a string payload (JSON-encoded so it stays single-line).
func writeSSEString(w io.Writer, event, s string) {
	b, _ := json.Marshal(s)
	writeSSE(w, event, b)
}

// writeSSEJSON frames an arbitrary value as compact JSON.
func writeSSEJSON(w io.Writer, event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeSSEString(w, "error", "failed to encode result: "+err.Error())
		return
	}
	writeSSE(w, event, b)
}

// parseBoolPtr maps a query flag to a tri-state *bool (absent => nil).
func parseBoolPtr(s string) *bool {
	if s == "" {
		return nil
	}
	v := s == "1" || strings.EqualFold(s, "true") || strings.EqualFold(s, "yes") || strings.EqualFold(s, "on")
	return &v
}
