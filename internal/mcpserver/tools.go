package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
)

// translateInput is the `translate` tool's arguments. Descriptions come from the
// jsonschema tags (the SDK infers the input schema from this struct).
type translateInput struct {
	Text   string `json:"text" jsonschema:"the text to translate"`
	Target string `json:"target,omitempty" jsonschema:"target language code, e.g. en or zh-TW (optional; server default otherwise)"`
	Source string `json:"source,omitempty" jsonschema:"source language code, or auto to detect (optional)"`
}

// defineInput is the `define` tool's arguments.
type defineInput struct {
	Word string `json:"word" jsonschema:"the word or short term to look up"`
}

// historyInput is the `history` tool's arguments.
type historyInput struct {
	Query string `json:"query,omitempty" jsonschema:"fuzzy search query; omit for the most recent entries"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of records to return (default 20)"`
}

// historyOutput wraps the record list (MCP structured output must be an object).
type historyOutput struct {
	Records []store.Record `json:"records"`
}

// translateHandler translates text. The structured output is the full
// TranslateResult; the text content is a human-readable rendering.
func translateHandler(svc Service) mcp.ToolHandlerFor[translateInput, engine.TranslateResult] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in translateInput) (*mcp.CallToolResult, engine.TranslateResult, error) {
		res, err := svc.Translate(ctx, appcore.Params{Text: in.Text, Target: in.Target, Source: in.Source})
		if err != nil {
			return toolError(err), engine.TranslateResult{}, nil
		}
		return textResult(renderTranslation(res)), *res, nil
	}
}

// defineHandler looks up a word. Structured output is the full TranslateResult
// (dictionary payload or suggestions); text content is a readable rendering.
func defineHandler(svc Service) mcp.ToolHandlerFor[defineInput, engine.TranslateResult] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in defineInput) (*mcp.CallToolResult, engine.TranslateResult, error) {
		res, err := svc.Define(ctx, in.Word)
		if err != nil {
			return toolError(err), engine.TranslateResult{}, nil
		}
		return textResult(renderDefine(res)), *res, nil
	}
}

// historyHandler lists or searches translation history.
func historyHandler(svc Service) mcp.ToolHandlerFor[historyInput, historyOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in historyInput) (*mcp.CallToolResult, historyOutput, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		var (
			recs []store.Record
			err  error
		)
		if strings.TrimSpace(in.Query) != "" {
			recs, err = svc.HistorySearch(ctx, in.Query, limit)
		} else {
			recs, err = svc.HistoryRecent(ctx, limit)
		}
		if err != nil {
			return toolError(err), historyOutput{}, nil
		}
		return textResult(renderHistory(recs)), historyOutput{Records: recs}, nil
	}
}

// textResult builds a successful result carrying human-readable text. The SDK
// auto-populates StructuredContent from the typed Out value.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// toolError reports a tool-level failure as content (IsError), per the MCP spec —
// not as a protocol error — so the model can see what went wrong.
func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

func renderTranslation(res *engine.TranslateResult) string {
	var b strings.Builder
	b.WriteString(res.Translation)
	if len(res.Alternatives) > 0 {
		b.WriteString("\n\nAlternatives:")
		for _, a := range res.Alternatives {
			b.WriteString("\n- " + a)
		}
	}
	if s := strings.TrimSpace(res.Notes); s != "" {
		b.WriteString("\n\nNotes: " + s)
	}
	return strings.TrimSpace(b.String())
}

func renderDefine(res *engine.TranslateResult) string {
	d := res.Dictionary
	if d == nil {
		if len(res.Suggestions) > 0 {
			return "No exact match. Did you mean: " + strings.Join(res.Suggestions, ", ")
		}
		return res.Translation
	}
	var b strings.Builder
	b.WriteString(d.Word)
	if d.Phonetic != "" {
		b.WriteString("  " + d.Phonetic)
	}
	for _, m := range d.Meanings {
		b.WriteString("\n" + m.PartOfSpeech)
		for i, def := range m.Definitions {
			if i >= 4 {
				break
			}
			b.WriteString("\n  • " + def.Text)
			if def.Example != "" {
				b.WriteString("\n    \"" + def.Example + "\"")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func renderHistory(recs []store.Record) string {
	if len(recs) == 0 {
		return "(no history)"
	}
	var b strings.Builder
	for _, r := range recs {
		fmt.Fprintf(&b, "%s → %s  [%s→%s]\n", r.Input, r.Output, r.SourceLang, r.TargetLang)
	}
	return strings.TrimSpace(b.String())
}
