# Plan: `--bilingual` immersive pipe mode + ANSI-strip-on-input

## Context

Today, `tldr rg | translate --to zh.TW` loses all color: the pipe branch reads
stdin, `TrimSpace`es it, and sends the whole blob — **ANSI escape codes and all** —
to the LLM (`cmd/root.go:205-214`), which returns one plain paragraph printed with
`fmt.Println` (`cmd/root.go:468-469`). The user wants the Immersive-Translate
experience: keep the original (with its styling) and show the translation
alongside it.

Investigation (see the companion `backlog/` doc) established two things:

1. **Per-word color transfer (re-coloring `grep` red in the translation) is
   over-engineering** and is *not* what Immersive Translate does — it preserves
   **block-level** style and renders the translation in one uniform (dimmed)
   theme. Chasing per-span color reproduces the XLIFF/DeepL inline-tag
   reordering problem (dropped/duplicated/misplaced tags), defeats streaming, and
   needs cross-lingual word alignment. Rejected.
2. The right altitude is a **bilingual, block-interleaved reading mode**: original
   line kept verbatim (color intact) + translation beneath in a uniform dim style.
   And separately, a **one-call hygiene fix**: strip ANSI from piped input before
   it reaches the LLM.

`github.com/charmbracelet/x/ansi` (already an indirect dep) provides `ansi.Strip`
and a CJK-aware tokenizer, so no new third-party dep is needed.

This plan ships **two independent changes + docs**:

- **Fix (patch):** strip ANSI from stdin before translating (helps every piped run).
- **Feat (minor):** opt-in `--bilingual` / `-2` mode for the pipe path.

## Decisions (confirmed with user)

- Flag: **`--bilingual`** with shorthand **`-2`**.
- Mixed prose+code input: **skip code/command blocks** (echo verbatim, untranslated), translate only prose.
- **Flag-only** (standalone, Pattern B) — never a config/pipe default. Protects the Unix-filter contract documented at `cmd/root.go:411-413`.

---

## Part 1 — Strip ANSI from piped input (fix)

**File:** `cmd/root.go`, pipe branch (`case !term.IsTerminal(...Stdin...)`, ~line 204-209).

- Keep the raw bytes (needed by Part 2), but the text sent to the engine becomes
  ANSI-free: `text := strings.TrimSpace(bitext.Strip(string(b)))`.
- `bitext.Strip` is a thin wrapper over `ansi.Strip` (new package below), so the
  import lives in one testable place.
- No behavior change for output; strictly cleaner/cheaper LLM input. Safe for all
  piped runs regardless of `--bilingual`.

---

## Part 2 — `--bilingual` / `-2` mode (feat)

### New package `internal/bitext` (pure, testable)

Per repo convention, logic that needs coverage lives under `internal/` (there are
no `cmd/` tests). New file `internal/bitext/bitext.go`:

```go
package bitext

type Kind int
const ( Blank Kind = iota; Prose; Code )

// Block is one blank-line-delimited unit of piped input.
type Block struct {
    Raw   string // original lines joined, ANSI intact (for display)
    Plain string // ANSI-stripped text (for the LLM + classification)
    Kind  Kind
}

func Strip(s string) string          // wraps ansi.Strip
func Split(raw string) []Block       // split on blank lines, classify each
func Render(blocks []Block, translations map[int]string, dim func(string) string) string
```

- **`Split`**: break input into blocks separated by blank line(s). Consecutive
  non-blank lines coalesce into one block (this also groups soft-wrapped prose so
  it translates with sentence context).
- **Classification heuristic** (`Code`): a block is `Code` when every non-blank
  physical line begins with ≥2 columns of indentation (matches tldr/man example
  blocks like `    rg pattern`). Everything else with letters is `Prose`. This is
  the "skip code, translate prose" decision. Documented as a heuristic with known
  misfires (fully-indented man pages, tables — see Limitations).
- **`Render`**: emit each block's `Raw` verbatim; for `Prose` blocks whose index
  has a translation, append the translation beneath, each line prefixed `  ↳ `
  (matching the house glyph in `renderLearnCLI`, `cmd/root.go:496-538`) and passed
  through `dim()`. `Code`/`Blank` blocks pass through untouched. Keeping styling in
  a caller-supplied `dim` closure keeps `Render` pure (test with identity func).

### `cmd/root.go` wiring

1. **Flag (Pattern B, standalone):** add `flagBilingual bool` to the var block
   (~line 30-47) and register in `NewRootCmd` (~line 82):
   `f.BoolVarP(&flagBilingual, "bilingual", "2", false, "pipe mode: keep original (with color) + translation beneath")`.
   Read directly at the call site — **no** Overrides/Resolve/Config wiring
   (mirrors `flagJSON`/`flagNoHistory`).

2. **Dispatch:** in the stdin pipe branch, before the normal `oneShot` call:
   `if flagBilingual && !flagJSON && !res.Learn { return runBilingual(ctx, oneShotEng, string(b), src, effTgt, res, stdoutTTY) }`.
   `--bilingual` is honored **only on the pipe path**; with positional args it
   falls through to normal `oneShot` (documented). `--json`/`--learn` keep their
   structured output and take precedence.

3. **`translateOnce` helper (reuse seam):** extract the non-printing core
   (`eng.Translate(ctx, req)` → `engine.Drain(ch, nil)`) into
   `func translateOnce(ctx, eng engine.Engine, req engine.Request) (*engine.TranslateResult, error)`.
   Used by `runBilingual`; `oneShot` left as-is to limit blast radius.

4. **`runBilingual`:**
   - `blocks := bitext.Split(raw)`.
   - For each `Prose` block, translate `block.Plain` via `translateOnce` with a
     Request forcing `Stream:false` and `Preset:""` (**concise** — contextual/
     dictionary presets reshape output and would break the 1-block↔1-translation
     mapping). Reuse `src`/`effTgt`/`res.Instructions`; ignore pair/learn here.
   - **Bounded concurrency** (no new dep): `sem := make(chan struct{}, 5)` +
     `sync.WaitGroup`; each goroutine writes its own index into a
     `map[int]string` guarded by a mutex (or a pre-sized slice). Derive per-call
     work from `ctx` so cancellation propagates. Per-block calls are short, so the
     60s `http.Client` timeout footgun (TODO.md P2) is not triggered the way one
     giant concatenated call would be.
   - On a block's translation error: skip its translation (still print the
     original) and collect a one-line `stderr` warning at the end — never silent,
     matching the existing warning idiom (`cmd/root.go:450-455`).
   - Print `bitext.Render(blocks, translations, dim)`.

5. **`dim` styling (pipe-safe):** a small local helper. Style only when
   `stdoutTTY && os.Getenv("NO_COLOR") == ""`, using
   `lg.NewStyle().Foreground(lg.Color("#6C6C6C"))` (the TUI's `colDim`,
   `internal/tui/styles.go:8,46`; the primitive is a one-liner — no shared helper
   exists to import). Otherwise identity (plain `  ↳ text`). The **original block's
   own ANSI always passes through** (that's the whole point); only our *added*
   translation lines are gated.

6. **History/speak in bilingual:** v1 skips per-block history and `--speak`
   (bilingual is a multi-block reading view). Documented.

### Known limitations (documented, not fixed in v1)

- Tabular/aligned output (`ls -l`, `kubectl get`, `git status`) — interleaving
  breaks columns; bilingual is meant for prose/docs (tldr, man, `--help`).
- Fully-indented man pages may be misclassified as all-`Code` (skipped).
- Hard-wrapped prose split by blank lines loses cross-block context.

---

## Docs (the "补文档 / 竞品分析" ask)

Per `AGENTS.md:39-96` conventions (no new `docs/` dir; `TODO.md` is the single index):

1. **`backlog/bilingual-immersive-mode.md`** (new) — the design + competitor
   analysis. Sections: the problem; **Immersive Translate philosophy** (block-level
   style + bilingual + uniform dim, *not* per-word color); **rejected alternatives**
   (A dominant-color = meaningless for per-word tldr color; B per-span placeholder
   tokens = XLIFF/DeepL/MediaWiki inline-tag failure modes, streaming-defeating);
   the `x/ansi` API notes; heuristics + limitations. Mark `Status: shipped`.
2. **`backlog/README.md`** — add the alphabetical index row for the new doc.
3. **`README.md`** — new `### Bilingual mode (--bilingual)` subsection (parallel to
   `### Pair mode`, ~line 107) + a flag row in the usage/flags list (~line 39).
   Note both the `--bilingual` behavior and the ANSI-strip-on-input hygiene fix.
4. **`TODO.md`** — record the shipped feature in `## Done` (dated) via
   `scripts/promote-todo.sh`; add a `P3 [M]` follow-up for table/wrapped-prose-aware
   bilingual (the documented limitation) via `scripts/add-todo.sh`. Re-run
   `scripts/todo-kanban.sh --validate-only TODO.md`.

Version implication: new flag → **minor** bump per `AGENTS.md:12` (tagging/release
is a separate manual step, out of scope unless requested).

---

## Tests

New `internal/bitext/bitext_test.go` (table-driven, per `prompt_test.go`/`llm_test.go` style):

- `TestStrip`: colored + CJK input → correct plain text.
- `TestSplit`: blank/prose/code classification; indentation heuristic; ANSI kept in
  `Raw`, stripped in `Plain`; soft-wrap coalescing; a realistic tldr-shaped fixture.
- `TestRender`: prose block gets `  ↳ ` translation lines (identity `dim`); code &
  blank blocks pass through verbatim; multi-line translation indentation.

No `cmd/` tests (repo has none by convention); `runBilingual` is kept thin, with all
logic in the tested `bitext` package.

---

## Verification

1. **Static + unit:** `just check` (gofmt + `go vet` + build) and `just test` green.
   `go mod tidy` to promote `x/ansi` from indirect → direct.
2. **Strip fix (e2e):** `printf '\033[31mhola\033[0m mundo' | go run . --to en` →
   clean translation; with `--debug` confirm the prompt text carries no escape bytes.
3. **Bilingual (e2e):** `tldr rg | go run . --to zh-TW --bilingual` (or
   `ls --color=always / | go run . --to zh-TW -2`) → original colored lines intact,
   dim `↳` translations beneath prose, indented command examples untranslated.
4. **Pipe-clean:** `printf 'Hello\n\n  code --flag\n' | go run . --to zh-TW -2 | cat -v`
   → our translation lines carry NO dim escapes (non-TTY stdout); code line
   untranslated; original passes through.
5. **Fallback:** point at an unreachable provider → per-block error surfaces as a
   `stderr` warning, originals still print.

## Out of scope / follow-ups

- Per-word/per-span color transfer (rejected — documented in backlog).
- Table/column-aware and hard-wrap-aware bilingual (P3 follow-up).
- Config/`[cli]` default + env var for bilingual (flag-only for now).
- `--speak` / history integration for bilingual view.
