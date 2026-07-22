package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
)

// fakeService implements Service with canned responses and captured inputs.
type fakeService struct {
	result     *engine.TranslateResult
	defineRes  *engine.TranslateResult
	err        error
	recent     []store.Record
	lastParams appcore.Params
	lastSearch string
	streamTok  []string
}

func (f *fakeService) Translate(_ context.Context, p appcore.Params) (*engine.TranslateResult, error) {
	f.lastParams = p
	return f.result, f.err
}

func (f *fakeService) TranslateStream(_ context.Context, p appcore.Params, onToken func(string)) (*engine.TranslateResult, error) {
	f.lastParams = p
	for _, t := range f.streamTok {
		if onToken != nil {
			onToken(t)
		}
	}
	return f.result, f.err
}

func (f *fakeService) Define(_ context.Context, _ string) (*engine.TranslateResult, error) {
	return f.defineRes, f.err
}

func (f *fakeService) HistoryRecent(_ context.Context, _ int) ([]store.Record, error) {
	return f.recent, f.err
}

func (f *fakeService) HistorySearch(_ context.Context, query string, _ int) ([]store.Record, error) {
	f.lastSearch = query
	return f.recent, f.err
}

func do(t *testing.T, h *handlers, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	newMux(h).ServeHTTP(rec, req)
	return rec
}

func TestTranslateHappyPath(t *testing.T) {
	fake := &fakeService{result: &engine.TranslateResult{Translation: "你好", Target: "zh"}}
	rec := do(t, &handlers{svc: fake}, "POST", "/v1/translate", `{"text":"hello","target":"zh"}`, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	var got engine.TranslateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Translation != "你好" {
		t.Fatalf("translation = %q, want 你好", got.Translation)
	}
	if fake.lastParams.Text != "hello" || fake.lastParams.Target != "zh" {
		t.Fatalf("params = %+v, want text=hello target=zh", fake.lastParams)
	}
}

func TestTranslateEmptyText(t *testing.T) {
	rec := do(t, &handlers{svc: &fakeService{}}, "POST", "/v1/translate", `{"text":"   "}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), "empty_input")
}

func TestTranslateMalformedJSON(t *testing.T) {
	rec := do(t, &handlers{svc: &fakeService{}}, "POST", "/v1/translate", `{bad json`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), "bad_request")
}

func TestTranslateWrongMethod(t *testing.T) {
	rec := do(t, &handlers{svc: &fakeService{}}, "GET", "/v1/translate", "", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestDefineHappyAndEmpty(t *testing.T) {
	fake := &fakeService{defineRes: &engine.TranslateResult{Dictionary: &engine.DictEntry{Word: "hello"}}}
	rec := do(t, &handlers{svc: fake}, "POST", "/v1/define", `{"word":"hello"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	rec = do(t, &handlers{svc: fake}, "POST", "/v1/define", `{"word":""}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestHistoryRecentAndSearch(t *testing.T) {
	fake := &fakeService{recent: []store.Record{{Input: "a", Output: "b"}}}
	h := &handlers{svc: fake}

	rec := do(t, h, "GET", "/v1/history?limit=5", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var recs []store.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &recs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 1 || recs[0].Input != "a" {
		t.Fatalf("records = %+v, want one record a→b", recs)
	}

	do(t, h, "GET", "/v1/history?q=foo", "", nil)
	if fake.lastSearch != "foo" {
		t.Fatalf("search query = %q, want foo", fake.lastSearch)
	}
}

func TestHistoryEmptyEncodesArray(t *testing.T) {
	rec := do(t, &handlers{svc: &fakeService{recent: nil}}, "GET", "/v1/history", "", nil)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want [] (not null)", got)
	}
}

func TestHistoryTokenGuard(t *testing.T) {
	h := &handlers{svc: &fakeService{}, token: "secret"}

	if rec := do(t, h, "GET", "/v1/history", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}
	if rec := do(t, h, "GET", "/v1/history", "", map[string]string{"Authorization": "Bearer wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", rec.Code)
	}
	if rec := do(t, h, "GET", "/v1/history", "", map[string]string{"Authorization": "Bearer secret"}); rec.Code != http.StatusOK {
		t.Fatalf("good token: status = %d, want 200", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	rec := do(t, &handlers{svc: &fakeService{}}, "GET", "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Fatalf("body = %v, want status ok", body)
	}
}

func TestNewLoopbackEnforcement(t *testing.T) {
	fake := &fakeService{}
	if _, err := New(fake, Options{Bind: "0.0.0.0", Port: 4155}); err == nil {
		t.Fatal("expected error binding 0.0.0.0 without a token")
	}
	if _, err := New(fake, Options{Bind: "0.0.0.0", Port: 4155, Token: "t"}); err != nil {
		t.Fatalf("0.0.0.0 with token should be allowed: %v", err)
	}
	srv, err := New(fake, Options{Bind: "127.0.0.1", Port: 4155})
	if err != nil {
		t.Fatalf("127.0.0.1 should be allowed: %v", err)
	}
	if srv.Addr() != "127.0.0.1:4155" {
		t.Fatalf("addr = %q, want 127.0.0.1:4155", srv.Addr())
	}
}

// TestServeEndToEnd exercises the real HTTP stack (listener + client).
func TestServeEndToEnd(t *testing.T) {
	fake := &fakeService{result: &engine.TranslateResult{Translation: "bonjour"}}
	ts := httptest.NewServer(newMux(&handlers{svc: fake}))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/translate", "application/json", strings.NewReader(`{"text":"hi","target":"fr"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got engine.TranslateResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Translation != "bonjour" {
		t.Fatalf("translation = %q, want bonjour", got.Translation)
	}
}

func assertErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, body)
	}
	if env.Error.Code != want {
		t.Fatalf("error code = %q, want %q", env.Error.Code, want)
	}
}
