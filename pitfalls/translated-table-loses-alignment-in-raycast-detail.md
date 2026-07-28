# A translated table renders as wrapped `────` runs in Raycast but looks fine in the terminal

**Symptoms** (grep this section): a translated table shows up in a Raycast `Detail` as loose `| dense-95 | 0.1210 |` lines instead of a table; long `─────` or `=====` runs reflow as if they were prose; a markdown table has no header row and no borders in Raycast; columns that lined up in the terminal are ragged after translation; a table looks right in `translate --json` / the TUI but wrong in the Raycast extension; CJK table cells push the columns out of alignment.
**First seen**: 2026-07-28
**Affects**: any surface that renders `translate` output as markdown in a proportional font — `raycast/extension/src/lib/translation-detail.tsx`, `stream-view.tsx`, `history-detail.tsx`. Not the CLI or TUI, which are monospace.

## Symptom

Translating a results table into zh-TW and opening it in **Translate Text**
produced a page of wrapped rule characters and orphaned pipe rows — no table:

```
────────────────────────  ← reflowed as prose, wrapped mid-run
| V1 (驗證 2025-05)   | V2 (驗證 2026-06 樣本外) |
| dense-95            | 0.1210 | 0.0679 |
```

The same text piped through the CLI looked correct in a terminal.

## Root cause

Two independent failures stack, and neither is visible from the CLI:

1. **Alignment padding cannot survive translation.** A terminal table aligns its
   columns with spaces measured in terminal cells, and a CJK glyph occupies
   **two** cells. Translating `Variant` → `變體` changes the cell width of every
   row, so no amount of faithful translation preserves the alignment. The model
   was doing the reasonable thing and reproducing the input's shape.

2. **`Detail` renders markdown in a proportional font.** Even alignment that
   *did* survive would be destroyed a second time, because a space is no longer
   1/1 of a character's advance width. Meanwhile GFM needs a `|---|---|`
   separator row directly under the header and a consistent cell count per row to
   recognise a table at all — a terminal table has neither, so its pipe rows
   render as literal text and its `─` borders reflow as prose.

Compounding it: `renderTranslation` dropped `r.translation` straight into the
`markdown` prop with no fencing and no normalisation, so there was nothing in the
path that could have caught this.

**The invariant worth remembering: structure travels, padding does not.** Pipes
and a separator row survive translation and re-rendering; column alignment is a
property of one specific font in one specific renderer.

## Workaround

Fixed at both ends, because the extension talks to whatever `translate` the user
has installed and cannot assume a current one:

- **CLI** (`internal/engine/prompt.go`) — `tableDirective` is appended to the
  active preset when `bitext.IsTabular(req.Text)` says the input is a table,
  using the same append-don't-replace mechanism as `pairDirective`. It asks for
  GFM table structure and explicitly forbids padding cells and emitting box
  rules.
- **Extension** (`src/lib/markdown.ts`) — `normalizeTables()` rebuilds any
  table-like run: inserts a missing separator, fits every row to the header's
  cell count, discards box rules. `fencePreformatted()` fences pipe-less aligned
  blocks so they at least stay monospace. Both are pure and asserted from
  `src/lib/dev-check.ts`.
- In `StreamView`, repair runs **once, in `onDone`** — a half-arrived table has
  no complete header row to normalise against, and repairing per chunk makes the
  view flicker between shapes.

## Prevention

- Never put model output directly into a `markdown` prop. It is untrusted text
  shaped for some other renderer.
- When a layout looks wrong in one front-end and right in another, check whether
  it depends on character width before assuming the content is wrong. Terminal
  and proportional renderers disagree about CJK by a factor of two.
- Ask a model for *structure* it can express, not for *appearance* it has to
  simulate.

## Related

- [`tui-viewport-clips-long-translation-no-softwrap.md`](tui-viewport-clips-long-translation-no-softwrap.md) — the other "rendering differs from the data" trap, in the TUI.
- [`raycast-extension-uses-installed-binary-not-working-tree.md`](raycast-extension-uses-installed-binary-not-working-tree.md) — why the extension-side repair has to exist even after the CLI is fixed.
- [`raycast-search-bar-refuses-long-paste.md`](raycast-search-bar-refuses-long-paste.md) — why a table gets pasted into **Translate Text** rather than the search bar.
