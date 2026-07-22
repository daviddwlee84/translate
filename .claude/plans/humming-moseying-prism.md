# Plan: Fix pair-mode routing for mixed-script (mostly-Latin + a few CJK) input

## Context

In bidirectional **pair mode** (`zh-TW⇄en`), pasting a mostly-English passage that
contains a few Chinese proper nouns (e.g. 李榮浩, 李白) came back **unchanged as
English** — an "English→English" no-op — and `^t` (re-pick target) didn't help.
The footer showed `detected: english` (correct), which disagreed with the actual
routing (to English), and that mismatch is the tell.

**Root cause:** `lang.PairTarget` (`internal/lang/detect.go`) routes a CJK⇄Latin
pair by `containsCJK(text)` — "does the text contain **any** CJK rune?". A single
Han character flips the whole routing to the Latin side, so mostly-English text
with a couple of Chinese names was treated as "CJK text" → target = English → the
model "translated" already-English text into English. `^t` can't change this
because the CJK-side decision ignores the target entirely; it's pure script
presence. The `detected: english` label (`lang.Detect`, whatlanggo) was correct
all along — the bug is in the **routing**, not detection.

## Change (already implemented in the working tree; awaiting approval)

`internal/lang/detect.go`
- Extract `isCJKRune(r rune) bool` (Han/Hiragana/Katakana/Hangul); `containsCJK`
  now calls it (kept — still a tested utility).
- Add `cjkDominant(text string) bool`: compares the CJK rune count against the
  number of **non-CJK words** (maximal runs of non-CJK letters, any script).
  Since one CJK rune ≈ a word, this measures which script *dominates* rather than
  mere presence — a few CJK proper nouns in a long Latin passage no longer flip
  routing, while genuinely CJK-heavy text still counts as CJK. Works for
  non-Latin "other" scripts too (e.g. zh⇄ru) because it counts any non-CJK letter.
- `PairTarget`: replace the `containsCJK(text)` branch with `cjkDominant(text)`
  (predominantly CJK → Latin side; else → CJK side). Update the doc comment.

`internal/lang/detect_test.go`
- Keep existing cases; the CJK-majority mixed case `"hello 世界" → en` still holds
  (2 Han > 1 Latin word). Add the reported-bug cases (`mostly-latin-few-han`,
  `latin-with-cjk-filenames` → `zh-TW`) and a CJK-sentence-with-loanword case
  (`"我今天在用 iPhone 打字" → en`). Add `TestCJKDominant`.

Callers unaffected in signature: `cmd/root.go:268` (`effectiveTarget`),
`internal/tui/update.go:433,461`, `internal/engine/prompt.go:178` (learn-mode
direction) all keep calling `PairTarget`; they just get correct routing now.

## Verification (already run this session)

- `go build ./...` — OK.
- `go test ./internal/lang -run 'PairTarget|CJK'` — all PASS (incl. the two
  reported-bug cases and `TestCJKDominant`).
- End-to-end CLI (`translate --pair --pair-with en --to zh-TW …`), real provider:
  - mostly-English + 李榮浩/李白 → **Chinese** output (was echoed English) ✓
  - pure English → Chinese ✓ · pure Chinese → English ✓ · Chinese+`iPhone` → English ✓
- Remaining (post-approval): run full `just check` + `go test ./...`, then decide
  on commit / whether this rides into a follow-up patch release.

## Out of scope
- `lang.Detect` / whatlanggo itself — it returned `english` correctly here; no change.
- Same-script pairs (en⇄es etc.) — untouched (still trigram `inLang`).
