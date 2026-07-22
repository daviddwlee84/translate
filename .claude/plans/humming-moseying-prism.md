# Plan: Collapse excess blank lines in the TUI result pane

## Context

When translating multi-paragraph text, the result pane shows large vertical gaps
— several blank lines between paragraphs (see the user's screenshot: 2–3 blank
rows between each translated paragraph).

**Root cause (confirmed by tracing the code):** the excess blank lines live in the
translation *text*, not in styling or layout.
- The `concise` prompt tells the model "Output ONLY the translated text"
  (`internal/engine/prompt.go:21`), so the model faithfully reproduces the source's
  paragraph structure — and occasionally emits runs of 2+ blank lines.
- `finalize` does only `strings.TrimSpace(full)` (`internal/engine/llm.go:264`),
  which trims the ends but leaves *internal* blank-line runs verbatim.
- The `trans` style is just a color + bold, no padding/margins
  (`internal/tui/styles.go:41`), and the result viewport uses `SoftWrap`
  (`internal/tui/model.go:199–200`) which preserves blank lines exactly.

So nothing in the pipeline collapses the whitespace. The fix is a small
display-layer normalizer.

**Decision (confirmed with user):** apply in the result pane (streaming + final)
**and** in `^y` copy, so what you see equals what you copy. Leave engine output,
saved history, and TTS verbatim. Collapse runs of 2+ blank lines down to a single
blank line (paragraph breaks are kept; only the excess is removed).

## Changes

### 1. New helper — `internal/tui/text.go`
Add a small pure function (with doc comment matching the file's dense-comment style):

```go
// collapseBlankLines trims trailing whitespace from each line and collapses any
// run of 2+ blank (whitespace-only) lines down to a single blank line, so a
// multi-paragraph translation keeps its paragraph breaks without the model's
// occasional extra vertical whitespace piling up in the result pane.
func collapseBlankLines(s string) string
```
Implementation: split on `"\n"`, `strings.TrimRight(line, " \t")` each line, keep at
most one consecutive blank line, `strings.Join` back with `"\n"`. No regexp needed.
(Leading/trailing blank lines are already handled by the callers' `TrimSpace`, but
the helper is safe on them regardless.)

### 2. Apply at the three free-text render/copy sites
These are the only places the plain `Translation`/stream buffer becomes visible text
(the learn/dictionary/suggestions renderers build their own structure and are not
affected):

- **Streaming render** — `internal/tui/update.go:98`
  `m.vp.SetContent(m.st.trans.Render(collapseBlankLines(m.streamBuf)))`
- **Final render** — `internal/tui/view.go:199` (in `renderResult`)
  `b.WriteString(m.st.trans.Render(collapseBlankLines(res.Translation)))`
- **Copy** — `internal/tui/update.go:502` (in `copyText`)
  `return collapseBlankLines(strings.TrimSpace(m.result.Translation))`

Applying the same helper to both the streaming buffer and the final result means the
pane does not visually "jump" when the completed result replaces the live stream.

### 3. Test — `internal/tui/text_test.go`
Table-driven unit test for `collapseBlankLines` covering: 3+ blank lines → 1,
single blank line preserved, whitespace-only lines treated as blank, trailing
spaces stripped, no-blank text unchanged, empty string.

## Verification

- `go test ./internal/tui/...` — new helper test passes.
- `go build ./...` — compiles.
- Manual smoke (needs a configured provider): run the TUI, paste multi-paragraph
  text that has double/triple blank lines between paragraphs, and confirm the result
  pane now shows a single blank line between paragraphs; press `^y` and paste
  elsewhere to confirm the copied text matches what's shown.

## Out of scope
- Engine/history/TTS normalization (user chose display + copy only).
- Learn / dictionary / suggestions renderers (they own their spacing; no change).
