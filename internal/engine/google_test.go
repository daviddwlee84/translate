package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGooglePreserveFormatBestEffort(t *testing.T) {
	const input = "\n# Report\n\n    command  \n\n"
	const translated = "\n# 報告\n\n    command  \n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != input {
			t.Errorf("Google input = %q, want %q", got, input)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{translated, input}}, nil, "en"})
	}))
	defer srv.Close()

	for _, tc := range []struct {
		name     string
		preserve bool
		fallback bool
	}{
		{"ordinary translation still trims", false, false},
		{"direct Google retains engine and warns", true, false},
		{"chain fallback retains engine and warns", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var eng Engine = NewGoogle(GoogleConfig{Endpoint: srv.URL})
			if tc.fallback {
				failed := &fakeEngine{name: "llm", err: errors.New("llm: unavailable")}
				eng = NewChain([]Engine{failed, eng}, 0)
			}
			ch, err := eng.Translate(context.Background(), Request{
				Text: input, Source: "en", Target: "zh-TW", Mode: ModeTranslate,
				PreserveFormat: tc.preserve,
			})
			if err != nil {
				t.Fatal(err)
			}
			res, err := Drain(ch, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := translated
			if !tc.preserve {
				want = strings.TrimSpace(want)
			}
			if res.Translation != want || res.Engine != "google" {
				t.Errorf("translation=%q engine=%q, want %q from google", res.Translation, res.Engine, want)
			}
			formatWarnings := 0
			for _, warning := range res.Warnings {
				if strings.Contains(warning, "cannot honor format-preservation instructions") {
					formatWarnings++
				}
			}
			wantWarnings := 0
			if tc.preserve {
				wantWarnings = 1
			}
			if formatWarnings != wantWarnings {
				t.Errorf("format warning count=%d, want %d: %v", formatWarnings, wantWarnings, res.Warnings)
			}
			if tc.fallback && !strings.Contains(strings.Join(res.Warnings, "\n"), "llm: unavailable") {
				t.Errorf("fallback reason missing: %v", res.Warnings)
			}
		})
	}
}

func TestGooglePreserveFormatRejectsWhitespaceOnlyResponse(t *testing.T) {
	_, err := parseGoogle([]json.RawMessage{json.RawMessage(`[["  \n\n"]]`)}, Request{PreserveFormat: true})
	if err == nil {
		t.Fatal("whitespace-only translation must still be rejected")
	}
}
