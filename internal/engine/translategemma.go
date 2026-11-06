package engine

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/daviddwlee84/translate/internal/lang"
)

// TranslateGemma accepts one user message in a fixed format with an explicit
// source language; it has no system role. Its Ollama template renders a system
// message as a separate user turn, so the general prompts do not apply.
// See docs/translategemma.md.
const translateGemmaPrompt = "You are a professional %[1]s (%[2]s) to %[3]s (%[4]s) translator. " +
	"Your goal is to accurately convey the meaning and nuances of the original %[1]s text " +
	"while adhering to %[3]s grammar, vocabulary, and cultural sensitivities.\n" +
	"Produce only the %[3]s translation, without any additional explanations or commentary. " +
	"Please translate the following %[1]s text into %[3]s:\n\n\n%[5]s"

// translateGemmaMaxInputTokens is the model card's 2K input limit. Ollama drops
// the start of an over-long prompt (including the instructions) and still reports
// finish_reason=stop, so the limit is enforced before sending.
const translateGemmaMaxInputTokens = 2000

// isTranslateGemma reports whether a model id names TranslateGemma, with or
// without a registry namespace or tag ("translategemma:12b", "hf.co/x/translategemma-4b").
func isTranslateGemma(model string) bool {
	id := strings.ToLower(model)
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	return strings.HasPrefix(id, "translategemma")
}

// buildTranslateGemmaPrompt returns the single user message for req plus warnings
// for options the model cannot honour. An error means the request must not be sent.
func buildTranslateGemmaPrompt(req Request) (string, []string, error) {
	switch {
	case req.Learn:
		return "", nil, errors.New("translategemma does not support learn mode: it cannot produce structured output")
	case req.Bilingual:
		return "", nil, errors.New("translategemma does not support bilingual doc mode: it cannot produce structured output (try --bilingual-mode blocks)")
	case req.PreserveFormat:
		return "", nil, errors.New("translategemma does not support document mode: it can translate code blocks (for multiline input, pass --preserve-format=false)")
	}
	if n := estimateTokens(req.Text); n > translateGemmaMaxInputTokens {
		return "", nil, fmt.Errorf("translategemma input is too long: about %d tokens, limit %d; split the text or use another engine", n, translateGemmaMaxInputTokens)
	}

	var warnings []string
	if req.Preset == PresetDictionary {
		warnings = append(warnings, "translategemma ignores the dictionary preset: it returns the translation only")
	}
	if strings.TrimSpace(req.Extra) != "" {
		warnings = append(warnings, "translategemma ignores custom instructions: its prompt format is fixed")
	}

	sourceName, sourceCode := translateGemmaLang(translateGemmaSource(req))
	targetName, targetCode := translateGemmaLang(req.Target)
	return fmt.Sprintf(translateGemmaPrompt, sourceName, sourceCode, targetName, targetCode, req.Text), warnings, nil
}

// translateGemmaSource resolves the source language, which the prompt requires:
// an explicit source, else the other side of a pair, else offline detection.
//
// Detection is unreliable on short text ("hello" scores 0.01), but refusing would
// block most short input. In testing a wrong label did not change the translation,
// even when it equalled the target, so an undetected source is labelled English.
func translateGemmaSource(req Request) string {
	if source := strings.TrimSpace(req.Source); source != "" && source != "auto" {
		return source
	}
	if req.Pair {
		switch {
		case strings.EqualFold(req.Target, req.PairHome):
			return req.PairAway
		case strings.EqualFold(req.Target, req.PairAway):
			return req.PairHome
		}
	}
	if detected := lang.Detect(req.Text); detected != "" {
		return detected
	}
	return "en"
}

// translateGemmaLang maps a language code to the name and code the prompt uses.
// zh-TW is sent as-is: in testing zh-Hant produced almost only Simplified output,
// while zh-CN is not a code the model lists, so it becomes zh-Hans.
func translateGemmaLang(code string) (name, promptCode string) {
	code = strings.TrimSpace(code)
	if lang.Base(code) == "zh" {
		if strings.EqualFold(code, "zh-CN") {
			return "Chinese", "zh-Hans"
		}
		return "Chinese", code
	}
	name = lang.Name(code)
	if name == code {
		return code, code // not in our table: pass through rather than guess a name
	}
	return strings.ToUpper(name[:1]) + name[1:], code
}

// estimateTokens deliberately overestimates: one token per CJK rune, and one per
// three other runes (English measured close to four).
func estimateTokens(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+2)/3
}

// simplifiedOnlyRunes are common characters that do not occur in standard
// Traditional Chinese, so one in the output is reliable evidence of Simplified.
const simplifiedOnlyRunes = "这们时说对会过经发学国为问题开关实现还进让给间门见业车电话书语读写认识网络软数图视听买卖钱专务总统选举亲爱热闹难双类运动护医药师环职员办单欢迎传历华请谁计"

// translateGemmaOutputWarning flags Simplified output for a Traditional target,
// which the model produces for a sizeable share of zh-TW requests.
func translateGemmaOutputWarning(target, output string) string {
	if !strings.EqualFold(target, "zh-TW") || !strings.ContainsAny(output, simplifiedOnlyRunes) {
		return ""
	}
	return "translategemma returned Simplified Chinese for zh-TW: this model does so for some inputs; use another engine for reliable Traditional output"
}
