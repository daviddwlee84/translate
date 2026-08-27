package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runComplete drives cobra's hidden completion driver, the same entry point the
// generated shell scripts call. It returns the candidate lines with the trailing
// ":<directive>" line stripped.
func runComplete(t *testing.T, args ...string) []string {
	t.Helper()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"__complete"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("__complete %v: %v (output: %s)", args, err, out.String())
	}

	var lines []string
	for _, l := range strings.Split(out.String(), "\n") {
		if l == "" || strings.HasPrefix(l, ":") || strings.HasPrefix(l, "Completion ended") {
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

// values strips the "\tdescription" half off each candidate.
func values(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, completionValue(l))
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// writeTestConfig points TRANSLATE_CONFIG at a fixture with known providers, so
// provider/model completion does not depend on the developer's own config.
func writeTestConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	body := `schema = 1

[[provider]]
name = "copilot"
type = "openai"
base_url = "http://127.0.0.1:4141/v1"
model = "gpt-5.4"
model_fast = "gpt-5-mini"

[[provider]]
name = "ollama"
type = "ollama"
base_url = "http://127.0.0.1:11434/v1"
model = "qwen3:8b"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRANSLATE_CONFIG", p)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
}

// Every flag registerCompletions covers must return candidates. Without the
// registration cobra falls back to filename completion and emits nothing, which
// is exactly the regression this guards.
func TestFlagCompletionsAreRegistered(t *testing.T) {
	writeTestConfig(t)
	for _, flag := range completedFlagNames() {
		got := runComplete(t, "--"+flag, "")
		if len(got) == 0 {
			t.Errorf("--%s: no completions; the RegisterFlagCompletionFunc call is missing or names a nonexistent flag", flag)
		}
	}
}

func TestLanguageCompletion(t *testing.T) {
	writeTestConfig(t)

	to := values(runComplete(t, "--to", ""))
	for _, want := range []string{"en", "zh-TW", "ja"} {
		if !contains(to, want) {
			t.Errorf("--to completions missing %q (got %d candidates)", want, len(to))
		}
	}
	if contains(to, "auto") {
		t.Error(`--to should not offer "auto": there is no auto target language`)
	}

	// The description half is what shells render next to the code.
	raw := runComplete(t, "--to", "")
	if !strings.Contains(strings.Join(raw, "\n"), "en\tenglish") {
		t.Errorf("--to candidates should carry tab-separated descriptions, got %q", raw[0])
	}

	if from := values(runComplete(t, "--from", "")); !contains(from, "auto") {
		t.Error(`--from should offer "auto" for source detection`)
	}
}

func TestProviderAndModelCompletion(t *testing.T) {
	writeTestConfig(t)

	providers := values(runComplete(t, "--provider", ""))
	for _, want := range []string{"copilot", "ollama"} {
		if !contains(providers, want) {
			t.Errorf("--provider completions missing %q, got %v", want, providers)
		}
	}

	// With no --provider given, every configured provider's models are offered.
	all := values(runComplete(t, "--model", ""))
	for _, want := range []string{"gpt-5.4", "gpt-5-mini", "qwen3:8b"} {
		if !contains(all, want) {
			t.Errorf("--model completions missing %q, got %v", want, all)
		}
	}

	// With --provider set, only that provider's models are offered.
	scoped := values(runComplete(t, "--provider", "ollama", "--model", ""))
	if !contains(scoped, "qwen3:8b") {
		t.Errorf("--provider ollama --model should offer qwen3:8b, got %v", scoped)
	}
	if contains(scoped, "gpt-5.4") {
		t.Errorf("--provider ollama --model should not offer copilot's models, got %v", scoped)
	}
}

// "dict" is only valid inside chain.order, never as an --engine value:
// appcore.BuildEngine has no case for it and errors out on the default branch.
func TestEngineCompletionOmitsDict(t *testing.T) {
	writeTestConfig(t)

	engines := values(runComplete(t, "--engine", ""))
	for _, want := range []string{"smartauto", "auto", "google", "copilot"} {
		if !contains(engines, want) {
			t.Errorf("--engine completions missing %q, got %v", want, engines)
		}
	}
	if contains(engines, "dict") {
		t.Error(`--engine should not offer "dict": BuildEngine rejects it`)
	}
}

// At the first word cobra offers subcommands, which is what we want. Beyond it
// the arguments are free text to translate, so filename completion would be
// noise — cobra.NoFileCompletions suppresses it.
func TestPositionalArgsDoNotCompleteFiles(t *testing.T) {
	writeTestConfig(t)

	if first := values(runComplete(t, "")); !contains(first, "define") {
		t.Errorf("the first word should still offer subcommands, got %v", first)
	}

	if got := runComplete(t, "hola", ""); len(got) != 0 {
		t.Errorf("completion after free text should be empty, not filenames; got %v", got)
	}
}

// Completion must never write a config file: a TAB press is not a first run.
func TestCompletionDoesNotCreateConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	t.Setenv("TRANSLATE_CONFIG", p)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)

	runComplete(t, "--provider", "")

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("completion created %s; it must use config.LoadForRead, not config.Load", p)
	}
}
