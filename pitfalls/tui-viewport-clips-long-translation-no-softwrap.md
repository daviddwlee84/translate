# TUI translation cut off mid-sentence, but the CLI / same request shows the full text

**Symptoms** (grep this section): interactive TUI shows a translation that ends
mid-sentence / mid-word with no error and the normal `detected: …` line; the *same*
input via the one-shot CLI (`translate "…"`) or a raw `curl` returns the **complete**
text; longer / multi-line outputs are the ones that get cut; no `⚠` warning shown.
**First seen**: 2026-07
**Affects**: `translate` TUI result pane (Bubble Tea `charm.land/bubbles/v2` viewport);
any single-line result wider than the viewport.
**Status**: fixed — enabled `viewport.SoftWrap` (was default `false`).

## Symptom

A paragraph translation rendered in the TUI as e.g. `…要我做一次英文翻譯處` and stopped,
with `detected: english` right after and empty space below. It *looked* like a truncated
LLM stream. But the one-shot CLI and a raw `curl` of the identical request returned the
full text every time (`…然後發布 0.8.0？`). Instrumenting the engine confirmed the stream
was complete: `deltas=7 fullRunes=131 complete=true`, and the TUI's own `ChunkDone` carried
the whole 131-rune `translation` string — yet only ~2 rows displayed.

## Root cause

`renderResult` emits the translation as **one long unwrapped line** (`lipgloss` `Render`
with no `Width` set does not wrap). The result viewport (`bubbles/v2/viewport`) has
`SoftWrap bool` defaulting to **`false`**, so a line wider than the viewport is **clipped**,
not wrapped — the tail past the viewport width is simply not shown. Short results fit in one
row and looked fine, which is why it read as "intermittent" and "truncated". The engine,
channel, and `ChunkDone.Result.Translation` all had the complete text the whole time.

The tell that it's a *display* bug, not a stream bug: **the CLI (which prints
`res.Translation` directly) shows the full text, but the TUI (which routes it through the
viewport) does not.** Same engine code, different presentation → look at the view layer.

## Workaround

Fixed in code:

```go
// internal/tui/model.go, New():
vp := viewport.New()
vp.SoftWrap = true   // wrap long paragraph lines instead of clipping their tail
```

Also `GotoTop()` after `SetContent` on `ChunkDone` (`internal/tui/update.go`) so a completed
result shows from its start rather than inheriting the streaming `GotoBottom` scroll.

## Prevention

- When putting model/LLM text into a `viewport`, set `SoftWrap = true` (or pre-wrap the
  content to the viewport width) — prose is long single lines, and the default clips.
- **Cross-check surfaces before blaming the source.** If one frontend (CLI) shows full data
  and another (TUI) shows partial, the bug is in the differing layer (rendering), not the
  shared engine/network. This one was first mis-diagnosed as a stream drop — see the sibling
  [`llm-stream-truncation-silently-rendered-as-complete`](llm-stream-truncation-silently-rendered-as-complete.md)
  (a real but *separate* robustness gap that shares this symptom).
- A cheap `TRANSLATE_DEBUG` log of `fullRunes`/`complete` at the engine boundary vs the
  rendered line count settles "stream vs display" in one run.

## Related

- Sibling (same symptom, different cause): `llm-stream-truncation-silently-rendered-as-complete.md`.
- `bubbles/v2` viewport `SoftWrap`: `charm.land/bubbles/v2/viewport` `Model.SoftWrap`.
