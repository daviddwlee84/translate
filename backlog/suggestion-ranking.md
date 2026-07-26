# Frequency-aware "did you mean" suggestion ranking

**Status**: P? — needs a spike
**Effort**: S–M
**Related**: `TODO.md` P? · `internal/engine/{smartdict,ecdict}.go` · `raycast/extension/src/lib/did-you-mean.tsx` · [../docs/raycast-extension.md](../docs/raycast-extension.md)

## Context

2026-07, surfaced while adding the Raycast "did you mean?" UX. The dictionary engine
returns fuzzy `suggestions[]` for single-word typos (consumed by Raycast
Translate/Define, and the CLI `define`). Ranking is **edit-distance only**, so the
obvious correction doesn't always surface first:

- `recieve` → `[relieve, believe, reachieve, recarve, recede, receive, recidive]`
  (`receive` is 6th)
- `cononical` → `[canonical, acanonical, canonicals, …]` (`canonical` happens to be 1st)

Google ranks by frequency ("Showing results for **canonical**"). We want the common
word on top.

## Investigation

- **ECDICT has frequency data** — `frq`, `bnc` columns (+ collins/oxford flags). A
  re-rank by `(editDistance asc, frequency desc)` would float common words up.
- Suggestions are produced in `internal/engine` (smart-dict / ecdict fuzzy lookup), so
  re-ranking there benefits **all** frontends at once — CLI `define`, Raycast, and the
  new `serve` / `mcp` adapters.
- `TranslateResult.SuggestDistance` exists but is `json:"-"` (not serialized). Exposing
  it (e.g. `suggest_distance`) would let a frontend auto-apply a confident correction
  ("Showing results for X · search instead for Y", Google-style) instead of only listing.

## Options considered

| Option | What | Verdict |
|---|---|---|
| A. Edit-distance only (current) | fuzzy by Levenshtein | simple; the right word is often not on top |
| B. Frequency-weighted re-rank | sort by (distance, then ECDICT `frq`/`bnc`) | best value; small change in the dict engine; helps every frontend |
| C. Expose `SuggestDistance` in `--json` | frontend decides auto-correct vs. list | pairs with B for a Google-style "showing results for X" |
| D. Keyboard-adjacency (qwerty) weighting | typo model | marginal over B; more complex |

## Current blocker / open questions

- ~~Does the bundled ECDICT actually populate `frq`/`bnc` for enough headwords?~~
  **Answered 2026-07-26.** `frq` yes — 42,231 of 770,611 rows carry one — but it is a
  **rank**, not a count (`the`=1, `test`=575, `testosterone`=10400, `0` = unranked), so
  the sort is *ascending with zeros last*; `frq DESC` is backwards and looks plausible
  (see [`../pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md`](../pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md)).
  `bnc` is **not imported at all** — `internal/engine/dictdata.go` reads only `rec[9]`.
- Inflected forms (`tests`, `testifying`) are unranked and therefore sort late. Their
  lemma is in the `exchange` column (`test` → `s:tests/d:tested/i:testing`), so a
  lemma-frequency boost is possible — filed separately in `TODO.md`.
- Chinese suggestions are prefix matches with no distance/frequency — leave as-is, or
  pinyin/stroke rank?
- Threshold/cap: only show suggestions within distance N; how many to return.

## Decision (if any)

`2026-07 deferred` — the extension already surfaces the full list (usable, just not
ideally ordered). Do **B** (and optionally **C**) when polishing the did-you-mean UX.
Frontends need no change for B; C would let Translate/Define auto-apply the top
correction.

`2026-07-26 partially shipped` — the **Look up Word** work
([`look-up-word.md`](look-up-word.md)) implemented **B** inside the new
`translate dict search` path: `engine.sortByDistanceThenRank` orders typo candidates
by `(distance asc, frq asc with 0 last, alpha)`, and the ECDICT lookup that supplies
the frequency exists (`ecdictDB.lookupMany`). `recieve` now puts `receive` 3rd instead
of 6th (`relieve` legitimately wins at distance 1, `believe` at rank 212 beats
`receive` at 497).

What remains — and why this entry is **not** Done:

- The comparator is not yet wired into `wordIndex.nearestN`'s *other* consumers
  (`internal/engine/dict.go:91`, `localdict.go:115`), so `define` / `translate` / TUI /
  MCP `suggestions[]` are still distance-only. That is a small change now that both the
  comparator and the frequency lookup exist.
- **C** is partly superseded: `dict search`'s `candidates[].distance` already gives a
  frontend the auto-correct signal without touching `TranslateResult`. Keep C open only
  for the `define` / `translate` JSON paths.

## References

- ECDICT schema (`frq`/`bnc`/collins/oxford): https://github.com/skywind3000/ECDICT
- Raycast did-you-mean component: `raycast/extension/src/lib/did-you-mean.tsx`
