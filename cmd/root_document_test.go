package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
)

func TestPreserveFormatSelection(t *testing.T) {
	for _, tc := range []struct {
		name, input, env string
		args             []string
		stdin, learn     bool
		mode             config.Mode
		want, wantError  bool
	}{
		{name: "multiline stdin", input: "hello\nworld\n", stdin: true, want: true},
		{name: "single word newline", input: "hello\n", stdin: true},
		{name: "heading", input: "# Hello\n", stdin: true, want: true},
		{name: "argv default", input: "hello\nworld"},
		{name: "force argv", input: "hello", args: []string{"--preserve-format"}, want: true},
		{name: "disable auto", input: "# Hello", stdin: true, args: []string{"--preserve-format=false"}},
		{name: "explicit preset", input: "# Hello", stdin: true, args: []string{"--preset", "contextual"}},
		{name: "environment preset", input: "# Hello", stdin: true, env: "dictionary"},
		{name: "empty environment", input: "# Hello", stdin: true, env: "  ", want: true},
		{name: "force over preset", input: "hello", stdin: true, env: "dictionary", args: []string{"--preset", "contextual", "--preserve-format"}, want: true},
		{name: "json supports auto", input: "# Hello", stdin: true, args: []string{"--json"}, want: true},
		{name: "learn no auto", input: "# Hello", stdin: true, learn: true},
		{name: "bilingual no auto", input: "# Hello", stdin: true, args: []string{"--bilingual"}},
		{name: "learn conflict", learn: true, args: []string{"--preserve-format"}, wantError: true},
		{name: "bilingual conflict", args: []string{"--preserve-format", "--bilingual"}, wantError: true},
		{name: "tui conflict", mode: config.ModeTUI, args: []string{"--preserve-format"}, wantError: true},
		{name: "tui false also one-shot only", mode: config.ModeTUI, args: []string{"--preserve-format=false"}, wantError: true},
		{name: "learn disabled preservation", learn: true, args: []string{"--preserve-format=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRANSLATE_PRESET", tc.env)
			cmd := NewRootCmd()
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			err := validatePreserveFormat(cmd, tc.mode, tc.learn)
			if (err != nil) != tc.wantError {
				t.Fatalf("validation error = %v, wantError %v", err, tc.wantError)
			}
			if !tc.wantError {
				if got := preserveFormatForInput(cmd, tc.input, tc.stdin, tc.learn); got != tc.want {
					t.Fatalf("preserve = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// Root still owns terminal detection through os.Stdin/Stdout. Temporary regular
// files exercise that real pipe path without depending on the test runner's TTY.
func captureDocumentCLI(t *testing.T, input string, run func() error) (stdout, stderr string, err error) {
	t.Helper()
	dir := t.TempDir()
	open := func(name, body string) *os.File {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(path, os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	in, out, diag := open("stdin", input), open("stdout", ""), open("stderr", "")
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = in, out, diag
	defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
	err = run()
	outBytes, readErr := os.ReadFile(out.Name())
	if readErr != nil {
		t.Fatal(readErr)
	}
	errBytes, readErr := os.ReadFile(diag.Name())
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(outBytes), string(errBytes), err
}

func documentTestConfig(t *testing.T, endpoint string) {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"TRANSLATE_ENGINE", "TRANSLATE_PROVIDER", "TRANSLATE_MODEL", "TRANSLATE_TIER", "TRANSLATE_PRESET", "TRANSLATE_PAIR", "TRANSLATE_PAIR_WITH", "TRANSLATE_LEARN", "TRANSLATE_DEBUG", "TRANSLATE_INSTRUCTIONS", "TRANSLATE_SOURCE", "TRANSLATE_TARGET"} {
		t.Setenv(k, "")
	}
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(k, dir)
	}
	t.Setenv("TRANSLATE_CONFIG", filepath.Join(dir, "config.toml"))
	cfg := config.Default()
	cfg.General.Engine = "fixture"
	cfg.General.Preset = "contextual"
	cfg.General.DefaultTarget = "zh-TW"
	cfg.General.RememberLastPair = false
	cfg.History.Enabled = false
	cfg.TTS.Enabled = false
	cfg.Providers = []config.Provider{{Name: "fixture", Type: "openai", BaseURL: endpoint, Model: "fixture-model"}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentPipeEndToEnd(t *testing.T) {
	input := "\x1b[31m    keep indent\x1b[0m\n\n# Heading\n\n- prose with `--flag`  \n"
	clean := "    keep indent\n\n# Heading\n\n- prose with `--flag`  \n"
	for _, tc := range []struct {
		name, translation string
		args              []string
	}{
		{"plain newline", "    keep indent\n\n# 標題\n\n- 文字 `--flag`  \n", nil},
		{"plain missing newline", "    keep indent\n\n# 標題  ", nil},
		{"stream newline", "    keep indent\n\n# 標題\n\n", []string{"--stream"}},
		{"stream missing newline", "    keep indent\n\n# 標題  ", []string{"--stream"}},
		{"json", "    keep indent\n\n# 標題  \n", []string{"--json", "--stream"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type message struct{ Role, Content string }
			type request struct {
				Messages []message
				Stream   bool
			}
			requests := make(chan request, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				var req request
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				requests <- req
				if req.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					for _, token := range []string{"    ", tc.translation[4:]} {
						b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": token}}}})
						fmt.Fprintf(w, "data: %s\n\n", b)
					}
					fmt.Fprint(w, "data: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": tc.translation}, "finish_reason": "stop"}}})
				}
			}))
			defer srv.Close()
			documentTestConfig(t, srv.URL)
			out, diag, err := captureDocumentCLI(t, input, func() error {
				cmd := NewRootCmd()
				cmd.SetArgs(append([]string{"--no-history"}, tc.args...))
				return cmd.ExecuteContext(context.Background())
			})
			if err != nil {
				t.Fatalf("command failed: %v (%s)", err, diag)
			}
			if diag != "" {
				t.Fatalf("unexpected diagnostics: %q", diag)
			}
			if len(requests) != 1 {
				t.Fatalf("translation requests = %d, want 1", len(requests))
			}
			req := <-requests
			if len(req.Messages) != 2 || !strings.Contains(req.Messages[0].Content, "document translation engine") || strings.Contains(req.Messages[0].Content, "SHORT list") {
				t.Fatalf("document prompt missing or contextual prompt leaked: %+v", req.Messages)
			}
			if !strings.HasSuffix(req.Messages[1].Content, "Text:\n"+clean) {
				t.Fatalf("input whitespace lost: %q", req.Messages[1].Content)
			}
			if tc.name == "json" {
				var result engine.TranslateResult
				if err := json.Unmarshal([]byte(out), &result); err != nil || result.Translation != tc.translation {
					t.Fatalf("JSON output = %q, err = %v", out, err)
				}
				if req.Stream {
					t.Fatal("JSON request must not stream")
				}
			} else {
				want := tc.translation
				if !strings.HasSuffix(want, "\n") {
					want += "\n"
				}
				if out != want {
					t.Fatalf("stdout = %q, want %q", out, want)
				}
			}
		})
	}
}

type cliResultEngine struct{ result *engine.TranslateResult }

func (e cliResultEngine) Name() string                                   { return "fixture" }
func (e cliResultEngine) Supports(engine.Mode) bool                      { return true }
func (e cliResultEngine) Available(context.Context) bool                 { return true }
func (e cliResultEngine) Detect(context.Context, string) (string, error) { return "en", nil }
func (e cliResultEngine) Translate(context.Context, engine.Request) (<-chan engine.Chunk, error) {
	ch := make(chan engine.Chunk, 1)
	ch <- engine.Chunk{Kind: engine.ChunkDone, Result: e.result}
	close(ch)
	return ch, nil
}

func TestOneShotNonStreamingResultAndDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name           string
		result         engine.TranslateResult
		json, preserve bool
		want           string
	}{
		{name: "dictionary stream fallback", result: engine.TranslateResult{Translation: "測試", Engine: "dictionary"}, want: "測試\n"},
		{name: "suggestions", result: engine.TranslateResult{Suggestions: []string{"hello"}}, want: "no exact match — did you mean: hello\n"},
		{name: "setup diagnostic", result: engine.TranslateResult{Notes: "CC-CEDICT not installed"}, want: "\n"},
		{name: "document nonstreaming engine", result: engine.TranslateResult{Translation: "    文\n"}, preserve: true, want: "    文\n"},
		{name: "JSON diagnostic", result: engine.TranslateResult{Notes: "ECDICT not installed", Warnings: []string{"online fallback"}}, json: true},
		{name: "fallback provenance", result: engine.TranslateResult{Translation: "文字", Engine: "google", Warnings: []string{"provider unavailable"}}, want: "文字\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			NewRootCmd() // reset package flag values as a real invocation does
			flagStream, flagJSON = true, tc.json
			out, diag, err := captureDocumentCLI(t, "", func() error {
				_, err := oneShot(context.Background(), cliResultEngine{&tc.result}, "hello", "auto", "zh-TW", false, "contextual", "", false, "", "", false, "", tc.preserve)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.json {
				var result engine.TranslateResult
				if err := json.Unmarshal([]byte(out), &result); err != nil || result.Notes != tc.result.Notes {
					t.Fatalf("bad JSON: %q (%v)", out, err)
				}
			} else if out != tc.want {
				t.Fatalf("stdout = %q, want %q", out, tc.want)
			}
			if tc.result.Notes != "" && !strings.Contains(diag, tc.result.Notes) {
				t.Fatalf("missing diagnostic: %q", diag)
			}
			if strings.Contains(diag, "check the model/provider") {
				t.Fatalf("misleading setup advice: %q", diag)
			}
			if len(tc.result.Warnings) > 0 && tc.result.Engine != "" && !strings.Contains(diag, fmt.Sprintf("used %q", tc.result.Engine)) {
				t.Fatalf("fallback engine missing from diagnostics: %q", diag)
			}
		})
	}
}

func TestDocumentGoogleWithoutLLMProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "# Hello\n\nText\n" {
			t.Errorf("Google input = %q", got)
		}
		fmt.Fprint(w, `[[["# 你好\n\n文字\n","# Hello\n\nText\n",null,null]],null,"en"]`)
	}))
	defer srv.Close()
	documentTestConfig(t, srv.URL)
	// Explicit empty array prevents omitted-provider defaults from materializing.
	body := fmt.Sprintf(`schema = 2
provider = []
[general]
engine = "google"
default_target = "zh-TW"
remember_last_pair = false
[google]
enabled = true
endpoint = %q
[history]
enabled = false
[tts]
enabled = false
`, srv.URL)
	if err := os.WriteFile(config.Path(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, diag, err := captureDocumentCLI(t, "# Hello\n\nText\n", func() error {
		cmd := NewRootCmd()
		cmd.SetArgs([]string{"--no-history"})
		return cmd.ExecuteContext(context.Background())
	})
	if err != nil || out != "# 你好\n\n文字\n" || !strings.Contains(diag, "google cannot honor format-preservation") {
		t.Fatalf("Google document: stdout=%q stderr=%q error=%v", out, diag, err)
	}
}
