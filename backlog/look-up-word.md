# Look up Word — a Define Word-style Raycast picker

**Status**: shipped (2026-07-26)
**Effort**: L
**Related**: `TODO.md` Done · `internal/engine/{dictsearch,ecdict,cedictdb}.go` · `cmd/{dictupdate,define,lang}.go` · `raycast/extension/src/look-up-word.tsx` · [`suggestion-ranking.md`](suggestion-ranking.md) · [../docs/raycast-extension.md](../docs/raycast-extension.md)

## Context

2026-07-26. Raycast's built-in **Define Word** has a shape our extension didn't:
empty search bar → recent lookups; type → a ranked list of candidate headwords each
with a definition preview as its subtitle; `⏎` → a full page for that word.

Our `define` command couldn't do that. It debounced 500 ms, called
`translate define <word> --json` once, and rendered **one** result inline via
`isShowingDetail`. No candidate list, no history, no full-page view, no language
choice. The user's stated motivation for a full page: long content (an LLM
fallback definition, or a past sentence translation) is much more readable as
markdown on a page than in a narrow list detail pane.

Two CLI gaps made the candidate list impossible:

1. **No "search headwords by prefix" capability at all.** `define` is an exact point
   lookup that returns bare suggestion *strings* on a miss — no previews. The data
   was already on disk (ECDICT is a 770k-row SQLite table with a frequency column);
   it was simply never queried that way.
2. **`define` never wrote history**, so "⏎ records the lookup" had nothing behind it.

## Options considered

| Decision | Options | Verdict |
|---|---|---|
| Where the search lives | (a) new `dict search` subcommand · (b) a flag on `define` · (c) reimplement in TypeScript | **(a)**. (b) would overload `define --json`'s contract (one `TranslateResult`) with a second shape. (c) violates "reuse the binary, don't reimplement" and would duplicate ranking in two languages. |
| Chinese speed | (a) build a `cedict.db` SQLite index · (b) keep the lazy in-RAM parse · (c) an on-disk preview cache | **(a)**. The plain `.u8` is re-parsed by every process (~1.7 s per Chinese lookup) — unusable for type-ahead, and the index also takes `define 貓` from 1.7 s → 0.03 s for *every* front-end. (b) degrades gracefully and is kept as the fallback with a `notes` hint. |
| Auto-build the index | (a) inside `dict update` / an explicit `dict reindex` · (b) lazily inside `dict search` | **(a)**. A keystroke-driven process that Raycast aborts mid-build would never finish. `dict reindex` exists so installs predating the index don't re-download 9.8 MB. |
| New command vs rewriting `define` | (a) add `look-up-word`, keep `define` · (b) rewrite `define` in place · (c) add and remove | **(a)**, at the user's request — the two UXes stay side by side for comparison. |
| Empty-state list | (a) all history · (b) only word lookups · (c) toggle | **(a)**, at the user's request. Long past translations benefit most from the full-page view, so filtering them out would remove the payoff. |
| Language choice | (a) command argument + in-view dropdown · (b) in-view only · (c) config only | **(a)**. The argument is pickable in the Raycast root bar *before* the view opens; the in-view dropdown changes it later and is populated at runtime from the new `lang list --json` (35 languages) rather than the hand-synced 10-item manifest subset. |
| LLM fallback timing | (a) on `⏎` only · (b) automatically when the local list is empty | **(a)**. (b) is a ~6 s call per keystroke. An explicit *Ask the LLM* row appears when nothing local matched. |
| History write point | (a) `define` records by default, list stage uses `dict search` (no store at all) · (b) a new `history add` command · (c) record from TypeScript | **(a)**. `dict search` opening no store makes "typing cannot record" structural rather than a discipline. |
| MCP tool | (a) add `dict_search` · (b) skip | **(b)**. An LLM host doesn't need a typeahead list; `define` already returns `suggestions[]` and the model can just call it. ~20 lines against the same `Service` method if that changes. |
| `/v1/dict/search` on `serve` | (a) add · (b) skip | **(a)** for transport parity — the core lives in `internal/engine`/`appcore`, so every adapter gets it. Not token-guarded: dictionary headwords are public reference data, unlike history. |

## Non-obvious findings (measured, 2026-07-26)

- **ECDICT `frq` is a rank, not a count**, and `LIKE 'x%'` does not use `idx_word_lc`
  (435 ms full scan vs 12 ms range scan). Both are written up in
  [`../pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md`](../pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md).
- Timings after the change: `dict search tes` 4 ms · `te` 20 ms · `a` (worst case)
  33 ms warm / 275 ms cold · Chinese 35 ms indexed (was 720 ms) · `define 貓` 32 ms
  (was 519 ms). `cedict.db` is 23 MB and builds in ~2 s from the existing download.
- `translate define --plain <miss> --json` exits **1** with a plain stderr line and
  **no JSON**. The extension tags that case (`isNoDictEntry`) and renders a friendly
  page instead of a red error. `dict search` deliberately does the opposite: an empty
  candidate list and exit 0, because it runs on every keystroke.
- The Raycast **Speak** action used to run a full root translation *and* write a
  history row every time. Look up Word uses `translate speak`, which neither
  translates nor records; `speak()` gained `--no-history`.
- 7 of 72 `engine:"dictionary"` rows in the live history file had `output:""` — a
  "did you mean" miss recorded as an answer. `appcore.Recordable` now filters those
  at every write site (CLI, Service, define).

## Follow-ups filed

See `TODO.md`: `history` field filters · opportunistic `translate serve` fast path ·
Simplified→Traditional conversion for ECDICT glosses when the target is `zh-TW` ·
a covering `(word_lc, frq)` index to kill the 275 ms cold single-letter case ·
lemma-frequency boost via `entries.exchange` so `tests`/`testifying` rank sensibly.

## References

- ECDICT schema (`frq`/`bnc`/collins/oxford): https://github.com/skywind3000/ECDICT
- SQLite LIKE optimization requirements: https://www.sqlite.org/optoverview.html#the_like_optimization
- Raycast built-in Define Word (the UX being mirrored)
