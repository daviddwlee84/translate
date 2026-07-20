# Bilingual / "immersive" pipe mode + ANSI-strip-on-input

**Status**: shipped (2026-07-20)
**Effort**: M
**Related**: `TODO.md` Done · `cmd/root.go` (`runBilingual`, `translateOnce`, `dimFunc`, pipe branch) · `internal/bitext/` · `.claude/plans/plan-c-strip-enchanted-hummingbird.md`

## Context

2026-07, surfaced from piping colored docs through the tool:
`tldr rg | translate --to zh-TW` produced a clean but **colorless** blob. Two
problems hid behind that:

1. The piped ANSI escape codes reached the LLM verbatim (`io.ReadAll` →
   `TrimSpace` → `Request.Text`; `TrimSpace` only trims edges), wasting tokens and
   risking mis-segmentation.
2. There was no way to see the translation **alongside** the original — the ask,
   inspired by [Immersive Translate](https://immersivetranslate.com/en/), whose
   philosophy is "translate in place, keep the original, preserve the element's
   own style".

## What shipped

- **Strip fix (patch):** `bitext.Strip` (wraps `ansi.Strip`) is applied to piped
  stdin before it becomes `Request.Text`. Cleaner/cheaper prompt for every piped
  run; output unchanged.
- **`--bilingual` / `-2` (feat):** opt-in, stdin-only. Splits input into
  blank-line-delimited blocks, echoes each block verbatim (ANSI/color intact), and
  prints a translation beneath each **prose** block (prefixed `  ↳ `, dimmed grey
  on a TTY). Indented **command/code** blocks are echoed untranslated. Prose blocks
  translate concurrently (bounded to 5), forced concise so each block maps to
  exactly one translation. Echoed translations (proper nouns the model returns
  unchanged) are suppressed. `--json`/`--learn` take precedence; with positional
  args it falls through to the normal one-shot.

## Key finding — what "the Immersive Translate philosophy" actually is

Reverse-engineered docs (DeepWiki) + the injected DOM classes show Immersive
Translate preserves **block/element-level** style and renders the translation in a
**single uniform theme** (grey/underline/dimmed). It inserts the translation into
the *same* block element, so a heading stays heading-sized via **CSS inheritance** —
it does **not** re-bold or re-color the specific words that were styled in the
source. Source inline styling survives only in the retained original copy.

Conclusion: "preserve per-word color in the translation" is **not** part of the
referenced philosophy. The faithful terminal analog is: keep the original block
(with its color) + one uniformly-dimmed translation beneath. That is exactly what
shipped.

## Options considered

| Option | What | Verdict |
|---|---|---|
| **A. Dominant-color per line** | detect each line's most-common SGR, recolor the translation in it | **Rejected.** tldr/`--help` color is *per-word* (command vs placeholder); "most frequent SGR" is a semantically meaningless color. |
| **B. Per-span color transfer** | parse ANSI → styled runs → placeholder tokens → LLM translates keeping markers → re-substitute | **Rejected — over-engineering.** Same problem class as XLIFF inline codes; unreliable across word reordering; defeats streaming. |
| **C. Block-interleaved, uniform dim** (shipped) | keep original block verbatim, translation beneath in one dim style; skip code blocks | **Chosen.** Matches the real Immersive Translate philosophy, robust, no alignment problem. |

### Why B is a trap (prior art)

Preserving inline markup across a translation that **reorders words** is a
well-studied hard problem, and the localization industry does **not** trust the
engine to reposition the tags:

- **XLIFF 2.0** adds `canReorder`/`canCopy`/`canDelete` on inline codes precisely
  because reorder/duplication/deletion are the recognized hazards. The split
  `<bx/>`/`<ex/>` placeholder design is itself a source of unpairable output.
- **DeepL** `tag_handling` **duplicates** tags by design when word order changes;
  **Google** `format=HTML` returns tags "to the extent possible" (documented drops,
  e.g. vanishing `notranslate` spans).
- **MediaWiki Content Translation** *abandoned* delegating tag placement to MT:
  they strip markup, translate plain text, then reattach via subsentence alignment
  + n-gram fuzzy matching — and still hit inflection/reorder/split failures.
- CAT tools (OmegaT, memoQ) rely on **detection** (tag-count validation that blocks
  export), not prevention.

A single ANSI-colored source word can become many target words or vanish via
inflection — there is no reliable "which target span wears the color". And the
reattachment step needs the **full** output buffered, defeating the token-by-token
streaming hot path (`internal/engine/prompt.go` asks for "ONLY the translated
text"; `engine.go` streams `ChunkToken`). Effort ≈ a mini-XLIFF engine for a
cosmetic terminal nicety.

## Implementation notes

- **`internal/bitext`** (pure, tested): `Strip`, `Split` (blank-line blocks +
  Prose/Code/Blank classification by ≥2-column indentation), `Render` (interleave;
  `dim` supplied as a closure so the package stays presentation-free and testable).
- **`github.com/charmbracelet/x/ansi`** (was already an indirect dep, now direct)
  provides `ansi.Strip` and a CJK/wcwidth-aware tokenizer — no new third-party dep.
  Its API also offers `DecodeSequence`/`StringWidth`/`Truncate` if a future
  refinement needs per-span parsing.
- **`cmd/root.go`**: `--bilingual` is a standalone flag (Pattern B — read directly,
  no config/resolve wiring), dispatched only on the stdin pipe branch;
  `runBilingual` fans out `translateOnce` over prose blocks with a
  `chan struct{}`/`WaitGroup` semaphore; `dimFunc` styles only when
  `stdoutTTY && NO_COLOR unset`.

## Known limitations (not fixed here)

- **Tables/columns** (`ls -l`, `kubectl get`, `git status`): interleaving a
  translation beneath each row breaks alignment. Bilingual targets prose docs
  (tldr, man, `--help`), not tabular output.
- **Fully-indented man pages** may be misclassified as all-`Code` and skipped.
- **Hard-wrapped prose** split by blank lines loses cross-block context.
- Pair/learn routing and `--speak`/history are intentionally out of scope for this
  reading view.

→ tracked as a `P3 [M]` follow-up in `TODO.md` (table/wrapped-prose-aware bilingual).

## References

- Immersive Translate (DeepWiki, reverse-engineered): content-script DOM insertion
  + theme styling system.
- XLIFF 1.2 / 2.0 inline codes (OASIS); DeepL `tag_handling` v2; Google Cloud
  Translation `format=HTML`; MediaWiki Content Translation markup handling.
- `charmbracelet/x/ansi` v0.11.7 (`ansi.Strip`, `DecodeSequence`, `StringWidth`).
