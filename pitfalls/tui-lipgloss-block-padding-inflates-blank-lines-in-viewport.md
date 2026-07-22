# TUI translation shows big gaps / many blank lines between paragraphs — but `^y` copy is clean

**Symptoms** (grep this section): interactive TUI result pane shows 2–4 blank
rows between paragraphs; excess vertical whitespace / "looks very empty"; the gap
grows as paragraphs get longer or the terminal gets narrower; multi-paragraph or
CJK / full-width translations are the worst; **`^y` copy of the same result pastes
with normal single blank lines** (display and clipboard disagree); collapsing
blank lines in the source string does *not* fix the display; rebuilding /
reinstalling doesn't help.
**First seen**: 2026-07
**Affects**: `translate` TUI result pane — `charm.land/lipgloss/v2` `Style.Render`
of multi-line text piped into a `charm.land/bubbles/v2` viewport with
`SoftWrap = true`. Worse with long paragraphs, narrow width, and CJK input.
**Status**: fixed — style the translation **per line** (`styles.renderTranslation`)
instead of block-rendering; blank-run collapsing made Unicode-whitespace-aware.

## Symptom

A multi-paragraph translation (e.g. translating a terminal scrape of a plan-mode
screen) rendered in the TUI with ~3 blank rows between every paragraph — it read
as "the newlines are exploding". The decisive tell: **`^y` copy produced clean,
single-blank-line text.** Copy and display both start from the same
`res.Translation`, so a clean copy means the excess whitespace is added *after*
the shared text — in the view layer, not the data.

Two earlier "fixes" did **not** move the needle, which is instructive:
1. Collapsing runs of blank lines in `res.Translation` — correct, but defeated
   downstream (see root cause). Copy uses the collapsed string and *was* clean;
   the pane was not.
2. Making the blank-line detector Unicode-aware — also a real bug (scrape rows
   were full-width spaces `U+3000`, which an ASCII `TrimRight(" \t")` missed), but
   still not the main cause.

## Root cause

`lipgloss.Style.Render(s)` on a **multi-line** string uses block formatting: it
pads *every* line out to the width of the **widest** line in the block. A blank
line becomes a run of spaces:

```go
newStyles().trans.Render("AA\n\nBBBBBBBBBB")
// line 0: "AA        "   ← padded to 10
// line 1: "          "   ← blank line → 10 SPACES, not empty
// line 2: "BBBBBBBBBB"
```

That padded blank line is then handed to the viewport, whose `SoftWrap = true`
re-wraps the long run of spaces into `ceil(blockWidth / viewportWidth)` blank
rows. So one paragraph break becomes N blank rows, N scaling with the longest
paragraph and shrinking viewport width:

```
content through viewport RAW (no style):   raw = [1 1]
content through trans.Render THEN viewport: styled = [2 2] @width180
                                            styled = [4 4] @width60
```

Copy (`copyText`) is unaffected because it returns the collapsed string directly
and never calls `Style.Render`. Short paragraphs also hide it — if the block's
widest line is narrow, the padded blank line doesn't wrap, so it stays one row.
That's why isolated tests with short fixtures passed while the real (long,
wrapping) output failed.

Irony worth noting: `SoftWrap = true` was itself the fix for a *different* pane
bug (long lines clipped — see Related). The very setting that stopped clipping is
what re-wraps these padded blanks. The two interact.

## Workaround

Fixed in code — style the translation **per line** so blank lines stay empty and
short lines aren't padded to the block max:

```go
// internal/tui/text.go
func (s styles) renderTranslation(text string) string {
	lines := strings.Split(collapseBlankLines(text), "\n")
	for i, ln := range lines {
		if ln != "" {
			lines[i] = s.trans.Render(ln) // one line at a time — no block padding
		}
	}
	return strings.Join(lines, "\n")
}
```

Call it from both render sites (`view.go` `renderResult`, `update.go` streaming
`ChunkToken`) instead of `trans.Render(collapseBlankLines(...))`.

`collapseBlankLines` also detects blanks with `strings.TrimSpace` (Unicode-aware:
`U+3000` full-width space, NBSP, lone `\r` from CRLF) and normalizes them to empty.

## Prevention

- **Never `Style.Render` a multi-line prose block into a `SoftWrap` viewport.**
  Block padding turns blank lines into space runs the viewport re-wraps. Style
  per line, or `SetContent` the raw text and color via the viewport's own style,
  or pre-wrap yourself.
- **Display disagrees with clipboard ⇒ the bug is in the view layer**, not the
  engine/`res.Translation`. `^y` copy being clean localized this instantly — the
  same cross-surface reasoning as the sibling clip pitfall.
- **Detect blank lines with `strings.TrimSpace`**, not an ASCII `" \t"` cutset —
  scrape/CJK sources carry `U+3000` / NBSP / `\r` on "empty" rows.
- **End-to-end guard uses LONG wrapping paragraphs.** Drive the real Bubble Tea
  model (`New` → `WindowSizeMsg` → `ChunkDone` streamMsg → read `vp.View()`) and
  assert blank runs ≤ 1. Short fixtures keep the block narrow and hide the bug —
  the regression test (`internal/tui/render_test.go`
  `TestResultPaneCollapsesBlankLines`) was proven to FAIL on the old block-render
  (`runs=[2 2]`) before it passed, so it isn't vacuous.

## Related

- Sibling (same pane, and the setting this bug rides on):
  [`tui-viewport-clips-long-translation-no-softwrap.md`](tui-viewport-clips-long-translation-no-softwrap.md)
  — `SoftWrap = true` fixed clipping; here it's what re-wraps the padded blanks.
- lipgloss block formatting: `charm.land/lipgloss/v2` `Style.Render` pads to the
  widest line (needed for backgrounds/alignment; surprising for plain fg+bold).
- Code: `internal/tui/text.go` (`renderTranslation`, `collapseBlankLines`),
  `internal/tui/render_test.go`.
