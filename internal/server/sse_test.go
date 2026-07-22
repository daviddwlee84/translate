package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daviddwlee84/translate/internal/engine"
)

func TestSSEStreamsTokens(t *testing.T) {
	fake := &fakeService{
		streamTok: []string{"你", "好"},
		result:    &engine.TranslateResult{Translation: "你好", Target: "zh"},
	}
	rec := do(t, &handlers{svc: fake}, "GET", "/v1/translate/stream?text=hi&target=zh", "", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if strings.Count(body, "event: token") != 2 {
		t.Fatalf("expected 2 token events, body:\n%s", body)
	}
	for _, want := range []string{`data: "你"`, `data: "好"`, "event: done", `"translation":"你好"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if fake.lastParams.Text != "hi" || fake.lastParams.Target != "zh" {
		t.Fatalf("params = %+v, want text=hi target=zh", fake.lastParams)
	}
}

func TestSSEEmptyText(t *testing.T) {
	rec := do(t, &handlers{svc: &fakeService{}}, "GET", "/v1/translate/stream", "", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), "empty_input")
}

func TestSSETruncatedWarning(t *testing.T) {
	fake := &fakeService{
		streamTok: []string{"par"},
		result:    &engine.TranslateResult{Translation: "par", Truncated: true},
	}
	rec := do(t, &handlers{svc: fake}, "GET", "/v1/translate/stream?text=hi", "", nil)
	body := rec.Body.String()
	if !strings.Contains(body, "event: warning") {
		t.Fatalf("expected a warning event for a truncated result:\n%s", body)
	}
	// The warning must precede the terminal done event.
	if strings.Index(body, "event: warning") > strings.Index(body, "event: done") {
		t.Fatalf("warning must come before done:\n%s", body)
	}
}

func TestSSEError(t *testing.T) {
	fake := &fakeService{err: engine.ErrNoResult}
	rec := do(t, &handlers{svc: fake}, "GET", "/v1/translate/stream?text=hi", "", nil)
	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected an error event:\n%s", body)
	}
	if strings.Contains(body, "event: done") {
		t.Fatalf("must not emit done on error:\n%s", body)
	}
}

// TestSSEContextCancel confirms a client disconnect cancels the handler's context,
// which is threaded to the engine (aborting the upstream request).
func TestSSEContextCancel(t *testing.T) {
	started := make(chan struct{})
	fake := &fakeService{streamFn: func(ctx context.Context, _ func(string)) (*engine.TranslateResult, error) {
		close(started)
		<-ctx.Done() // block until the client disconnects
		return nil, ctx.Err()
	}}
	ts := httptest.NewServer(newMux(&handlers{svc: fake}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/translate/stream?text=hi", nil)

	done := make(chan struct{})
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		close(done)
	}()

	<-started // handler is running, blocked on its request context
	cancel()  // simulate client disconnect

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not return after client cancel — ctx not propagated")
	}
}
