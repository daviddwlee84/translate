package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsTranslateGemma(t *testing.T) {
	for model, want := range map[string]bool{
		"translategemma":              true,
		"translategemma:12b":          true,
		"TranslateGemma:4B":           true,
		"hf.co/acme/translategemma-4": true,
		"gemma3:4b":                   false,
		"llama3.2:3b":                 false,
		"not-translategemma":          false,
	} {
		if got := isTranslateGemma(model); got != want {
			t.Errorf("isTranslateGemma(%q)=%v, want %v", model, got, want)
		}
	}
}

// The model expects this exact text, including two blank lines before the input.
func TestTranslateGemmaPromptMatchesOfficialFormat(t *testing.T) {
	got, warnings, err := buildTranslateGemmaPrompt(Request{Text: "Hello, how are you?", Source: "en", Target: "es"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "You are a professional English (en) to Spanish (es) translator. " +
		"Your goal is to accurately convey the meaning and nuances of the original English text " +
		"while adhering to Spanish grammar, vocabulary, and cultural sensitivities.\n" +
		"Produce only the Spanish translation, without any additional explanations or commentary. " +
		"Please translate the following English text into Spanish:\n\n\nHello, how are you?"
	if got != want {
		t.Errorf("prompt:\n%q\nwant:\n%q", got, want)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestTranslateGemmaLanguageCodes(t *testing.T) {
	for _, tc := range []struct{ code, name, promptCode string }{
		{"en", "English", "en"},
		{"zh", "Chinese", "zh"},
		{"zh-TW", "Chinese", "zh-TW"},
		{"zh-CN", "Chinese", "zh-Hans"},
		{"af", "af", "af"}, // not in our table
	} {
		name, promptCode := translateGemmaLang(tc.code)
		if name != tc.name || promptCode != tc.promptCode {
			t.Errorf("translateGemmaLang(%q)=(%q,%q), want (%q,%q)", tc.code, name, promptCode, tc.name, tc.promptCode)
		}
	}
}

func TestTranslateGemmaSource(t *testing.T) {
	for name, tc := range map[string]struct {
		req  Request
		want string
	}{
		"explicit wins over pair": {Request{Source: "fr", Pair: true, PairHome: "zh-TW", PairAway: "en", Target: "en"}, "fr"},
		"pair target is home":     {Request{Source: "auto", Pair: true, PairHome: "zh-TW", PairAway: "en", Target: "zh-TW"}, "en"},
		"pair target is away":     {Request{Pair: true, PairHome: "zh-TW", PairAway: "en", Target: "en"}, "zh-TW"},
		"detected":                {Request{Source: "auto", Text: "The quick brown fox jumps over the lazy dog near the river bank."}, "en"},
		"short text falls back":   {Request{Text: "hello"}, "en"},
		"undetectable":            {Request{Text: "12345"}, "en"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := translateGemmaSource(tc.req); got != tc.want {
				t.Errorf("source=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestTranslateGemmaIgnoredOptionsWarn(t *testing.T) {
	base := Request{Text: "Hello", Source: "en", Target: "ja"}
	for _, preset := range []string{"", PresetConcise, PresetContextual} {
		req := base
		req.Preset = preset
		if _, warnings, err := buildTranslateGemmaPrompt(req); err != nil || len(warnings) != 0 {
			t.Errorf("preset %q: warnings=%v err=%v, want neither", preset, warnings, err)
		}
	}
	req := base
	req.Preset = PresetDictionary
	req.Extra = "finance terms"
	got, warnings, err := buildTranslateGemmaPrompt(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 {
		t.Errorf("warnings=%v, want dictionary and instructions", warnings)
	}
	if strings.Contains(got, "finance terms") {
		t.Errorf("instructions leaked into the fixed prompt: %q", got)
	}
}

func TestEstimateTokensOverestimatesMeasuredInput(t *testing.T) {
	// 3221 chars of this English measured 833 tokens on translategemma:4b.
	english := strings.Repeat("Sentence number 1 says that the quick brown fox visited city 1 on a sunny day. ", 40)
	if n := estimateTokens(english); n < 833 {
		t.Errorf("estimate=%d is below the measured token count", n)
	}
	if n := estimateTokens("翻譯"); n != 2 {
		t.Errorf("CJK estimate=%d, want 2", n)
	}
}

func TestTranslateGemmaOutputWarning(t *testing.T) {
	for _, tc := range []struct {
		target, output string
		want           bool
	}{
		{"zh-TW", "公司计划下个月发布其移动应用程序的新版本。", true},
		{"zh-TW", "醫生建議每天飲用足夠的水，並進行規律的運動。", false},
		{"zh-CN", "公司计划下个月发布新版本。", false},
		{"ja", "学校に行きます。", false},
	} {
		if got := translateGemmaOutputWarning(tc.target, tc.output) != ""; got != tc.want {
			t.Errorf("target=%s output=%q warned=%v, want %v", tc.target, tc.output, got, tc.want)
		}
	}
}

func TestTranslateGemmaRequestOnTheWire(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			var wire map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"公司计划"}}]}`+"\n\n")
					_, _ = io.WriteString(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
					return
				}
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"公司计划"},"finish_reason":"stop"}]}`)
			}))
			defer srv.Close()

			e := NewLLM(LLMConfig{Name: "ollama", BaseURL: srv.URL, Model: "translategemma:4b"})
			ch, err := e.Translate(context.Background(), Request{
				Text: "The plan", Source: "en", Target: "zh-TW", Stream: stream,
				Preset: PresetContextual, Extra: "finance terms",
			})
			if err != nil {
				t.Fatal(err)
			}
			res, err := Drain(ch, nil)
			if err != nil {
				t.Fatal(err)
			}

			messages, _ := wire["messages"].([]any)
			if len(messages) != 1 {
				t.Fatalf("messages=%v, want exactly one", wire["messages"])
			}
			message := messages[0].(map[string]any)
			if message["role"] != "user" {
				t.Errorf("role=%v, want user", message["role"])
			}
			if prompt, _ := message["content"].(string); !strings.Contains(prompt, "English (en) to Chinese (zh-TW)") {
				t.Errorf("prompt=%q", prompt)
			}
			if temperature, ok := wire["temperature"].(float64); !ok || temperature != 0 {
				t.Errorf("temperature=%v, want 0", wire["temperature"])
			}
			joined := strings.Join(res.Warnings, "\n")
			if !strings.Contains(joined, "custom instructions") || !strings.Contains(joined, "Simplified Chinese") {
				t.Errorf("warnings=%v", res.Warnings)
			}
		})
	}
}

// Every other model must keep its system prompt and leave sampling to the server.
func TestOtherModelsRequestUnchanged(t *testing.T) {
	var wire map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"hola"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	e := NewLLM(LLMConfig{Name: "ollama", BaseURL: srv.URL, Model: "llama3.2:3b"})
	ch, err := e.Translate(context.Background(), Request{Text: "hello", Target: "es", Preset: PresetConcise})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Drain(ch, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["temperature"]; ok {
		t.Errorf("temperature sent for another model: %v", wire["temperature"])
	}
	messages, _ := wire["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages=%v, want system + user", wire["messages"])
	}
}

func TestTranslateGemmaRejectsBeforeSending(t *testing.T) {
	long := strings.Repeat("字", translateGemmaMaxInputTokens+1)
	for name, tc := range map[string]struct {
		req  Request
		want string
	}{
		"learn":     {Request{Text: "hi", Source: "en", Target: "ja", Learn: true}, "learn mode"},
		"bilingual": {Request{Text: "hi", Source: "en", Target: "ja", Bilingual: true}, "bilingual"},
		"document":  {Request{Text: "hi", Source: "en", Target: "ja", PreserveFormat: true}, "document mode"},
		"too long":  {Request{Text: long, Source: "zh", Target: "en"}, "too long"},
	} {
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
			defer srv.Close()
			e := NewLLM(LLMConfig{Name: "ollama", BaseURL: srv.URL, Model: "translategemma:4b"})
			_, err := e.Translate(context.Background(), tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "ollama: ") {
				t.Errorf("err=%v, want it to name the engine and contain %q", err, tc.want)
			}
			if requests.Load() != 0 {
				t.Errorf("sent %d requests, want none", requests.Load())
			}
		})
	}
}

// A rejection happens before the first token, so a chain still reaches the next engine.
func TestTranslateGemmaRejectionFailsOverInChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	chain := NewChain([]Engine{
		NewLLM(LLMConfig{Name: "ollama", BaseURL: srv.URL, Model: "translategemma:4b"}),
		NewLLM(LLMConfig{Name: "other", BaseURL: srv.URL, Model: "other-model"}),
	}, time.Minute)
	ch, err := chain.Translate(context.Background(), Request{Text: "hi", Source: "en", Target: "ja", PreserveFormat: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Drain(ch, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Engine != "other" {
		t.Errorf("engine=%q, want failover to other", res.Engine)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "document mode") {
		t.Errorf("warnings=%v, want the rejection reason", res.Warnings)
	}
}
