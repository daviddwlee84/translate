package engine

import (
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/lang"
)

func zhEnLearnReq(text string) Request {
	return Request{Text: text, Source: "auto", PairHome: "zh-TW", PairAway: "en", Learn: true}
}

func TestLearnDirection(t *testing.T) {
	// CJK input (home) → translate into the away side → teach.
	if got := learnDirection(zhEnLearnReq("我想學英文")); got != "teach" {
		t.Errorf("Chinese input direction = %q, want teach", got)
	}
	// Latin input (away) → route back to the CJK home → correct.
	if got := learnDirection(zhEnLearnReq("I has a apple")); got != "correct" {
		t.Errorf("English input direction = %q, want correct", got)
	}
}

func TestBuildLearnPromptSelectsDirection(t *testing.T) {
	teach, _ := buildLearnPrompt(zhEnLearnReq("我想學英文"))
	if !strings.Contains(teach, `"direction": "teach"`) {
		t.Errorf("teach prompt should carry the teach schema:\n%s", teach)
	}
	correct, _ := buildLearnPrompt(zhEnLearnReq("I has a apple"))
	if !strings.Contains(correct, `"direction": "correct"`) {
		t.Errorf("correct prompt should carry the correct schema:\n%s", correct)
	}
	// Both prompts must name the two languages so the model knows the sides.
	for _, name := range []string{lang.Name("zh-TW"), lang.Name("en")} {
		if !strings.Contains(teach, name) {
			t.Errorf("teach prompt missing language %q", name)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`:                       `{"a":1}`,
		"```json\n{\"a\":1}\n```":       `{"a":1}`,
		"```\n{\"a\":1}\n```":           `{"a":1}`,
		"here you go: {\"a\":1} thanks": `{"a":1}`,
		"  {\"a\":1}  ":                 `{"a":1}`,
	}
	for in, want := range cases {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLearn(t *testing.T) {
	if _, err := parseLearn(`{"direction":"teach","translation":"hi"}`); err != nil {
		t.Errorf("valid learn JSON should parse: %v", err)
	}
	if _, err := parseLearn(`not json`); err == nil {
		t.Error("malformed JSON should error")
	}
	if _, err := parseLearn(`{}`); err == nil {
		t.Error("empty object (no translation/corrected) should error")
	}
}

func TestFinalizeLearnTeach(t *testing.T) {
	e := NewLLM(LLMConfig{Name: "test"})
	req := zhEnLearnReq("我想學英文")
	full := "```json\n" + `{"direction":"correct","translation":"I want to learn English",` +
		`"vocab":[{"term":"learn","pos":"v.","phonetic":"/lɜːrn/","meaning":"學習"}]}` + "\n```"
	res := e.finalizeLearn(full, "claude-haiku-4-5", req)

	if res.Learn == nil {
		t.Fatal("Learn payload should be set")
	}
	// Direction is trusted from offline detection, NOT the (wrong) model field.
	if res.Learn.Direction != "teach" {
		t.Errorf("direction = %q, want teach (from detection)", res.Learn.Direction)
	}
	if res.Translation != "I want to learn English" {
		t.Errorf("Translation = %q, want the foreign translation", res.Translation)
	}
	if res.Target != "en" {
		t.Errorf("Target = %q, want en (PairAway)", res.Target)
	}
	if len(res.Learn.Vocab) != 1 || res.Learn.Vocab[0].Term != "learn" {
		t.Errorf("Vocab not parsed: %+v", res.Learn.Vocab)
	}
}

func TestFinalizeLearnCorrect(t *testing.T) {
	e := NewLLM(LLMConfig{Name: "test"})
	req := zhEnLearnReq("I has a apple")
	full := `{"direction":"correct","corrected":"I have an apple.","translation":"我有一顆蘋果。",` +
		`"issues":[{"span":"has","fix":"have","explanation":"主詞 I 用 have"}]}`
	res := e.finalizeLearn(full, "claude-haiku-4-5", req)

	if res.Learn == nil {
		t.Fatal("Learn payload should be set")
	}
	if res.Learn.Direction != "correct" {
		t.Errorf("direction = %q, want correct", res.Learn.Direction)
	}
	// The main Translation is the corrected FOREIGN sentence (so copy/speak get it).
	if res.Translation != "I have an apple." {
		t.Errorf("Translation = %q, want the corrected sentence", res.Translation)
	}
	if res.Target != "en" {
		t.Errorf("Target = %q, want en (PairAway) so speak/history label the foreign side", res.Target)
	}
	if len(res.Learn.Issues) != 1 || res.Learn.Issues[0].Fix != "have" {
		t.Errorf("Issues not parsed: %+v", res.Learn.Issues)
	}
}

func TestFinalizeLearnFallback(t *testing.T) {
	e := NewLLM(LLMConfig{Name: "test"})
	req := zhEnLearnReq("我想學英文")
	res := e.finalizeLearn("the model rambled without any JSON", "claude-haiku-4-5", req)

	if res.Learn != nil {
		t.Error("malformed reply should leave Learn nil")
	}
	if res.Translation != "the model rambled without any JSON" {
		t.Errorf("fallback Translation = %q, want the raw text", res.Translation)
	}
	if len(res.Warnings) == 0 {
		t.Error("fallback should record a warning")
	}
}

func TestParseBilingual(t *testing.T) {
	// Tolerates leading reasoning prose before the JSON — the exact leak doc mode
	// is meant to survive ("Wait, need Traditional Chinese…").
	reply := "Wait, need Traditional Chinese, not Simplified.\n\n{\"1\": \"一\", \"2\": \"二\"}"
	m, err := parseBilingual(reply)
	if err != nil {
		t.Fatalf("parseBilingual error: %v", err)
	}
	if m[1] != "一" || m[2] != "二" {
		t.Errorf("parseBilingual = %#v, want {1:一, 2:二}", m)
	}
	// A reply with no JSON object is an error (caller falls back to per-block).
	if _, err := parseBilingual("no json here"); err == nil {
		t.Error("expected an error for a reply with no JSON")
	}
}
