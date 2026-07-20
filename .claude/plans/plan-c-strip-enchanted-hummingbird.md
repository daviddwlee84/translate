# Plan: context-aware "doc" bilingual mode alongside the per-block mode (+ compare)

## Context

`--bilingual` currently translates each blank-line block in **isolation** (N concurrent LLM calls). Real output showed three failures from that isolation, all in one `tldr rg` run:
- `rg` (the command header) → mistranslated as an abbreviation ("角速度陀螺仪 / 巴西航空") because the model never sees it's Ripgrep's command.
- **Simplified/Traditional drift** — some blocks came back in 简体 among the 繁體.
- **Reasoning leak** — `Wait, need Traditional Chinese…` bled into a block.

The user's insight: Immersive Translate's LLM advantage is **context-awareness**. A single whole-document call fixes all three at once, and is actually **cheaper** (system prompt once + shared context vs repeated per block). We'll **keep both** strategies behind a flag and compare them.

`--learn` already establishes the structured-JSON call pattern to reuse: `promptFor` branches to `buildLearnPrompt`, output is parsed by `finalizeLearn`/`parseLearn`/`extractJSON` (which tolerates surrounding reasoning — directly defusing the leak), forced non-streaming, larger token cap (`internal/engine/llm.go`, `prompt.go`).

Outcome: `--bilingual` defaults to the new context-aware **doc** mode; `--bilingual-mode blocks` keeps the old per-block behavior for comparison/fallback.

## Design

### 1. New prompt — `internal/engine/prompt.go` `buildBilingualPrompt(req)`
- **System**: "Translate terminal/CLI documentation into `<target>`. You are given the full document as a numbered list of segments; some are prose to translate, others are command/code shown ONLY as context. Use the whole document — a bare token like `rg` is the command being documented, NOT an abbreviation; never expand it. Keep command names, flags, paths, URLs, and code verbatim. Translate ONLY prose segments. Reply with ONE JSON object `{"<n>": "<translation>"}` and nothing else — no reasoning, no commentary." (target expanded via `lang.Name`, like `buildTranslatePrompt`).
- **User**: the numbered segments — prose shown plainly, code lines tagged `[code — context only]`.
- Reuse `extractJSON` (`llm.go:342`) for robust parsing.

### 2. Engine wiring — mirror learn mode (`internal/engine/`)
- `engine.go`: add `Segment{ Text string; Code bool }`; `Request.Bilingual bool` + `Request.Segments []Segment`; `TranslateResult.Bilingual map[int]string` (prose-segment number → translation), transient (`json:"-"`), alongside `Learn`.
- `llm.go`: in `promptFor` (~L277) `if req.Bilingual { return buildBilingualPrompt(req) }`; extend the stream gates (`stream := req.Stream && !req.Learn && !req.Bilingual`, L369/L444); use a large cap when `req.Bilingual` (reuse `learnMaxTokens`); after drain (~L287) `if req.Bilingual { return e.finalizeBilingual(full, model, req) }` → new `finalizeBilingual` mirroring `finalizeLearn` (L299): `extractJSON` → `map[string]string` → convert keys to int → `res.Bilingual`.
- google/dict ignore Bilingual (LLM-only); doc mode requires an LLM provider.

### 3. cmd wiring — `cmd/root.go`
- New standalone flag (Pattern B): `f.StringVar(&flagBilingualMode, "bilingual-mode", "doc", "bilingual strategy: doc (context-aware, one call) | blocks (per-block)")`.
- `runBilingual(...)` gains a `mode` param and splits:
  - **`blocks`**: the existing per-block fan-out (unchanged — `translateOnce` × N, echo-suppression, bounded concurrency).
  - **`doc`**: build `[]engine.Segment` from `bitext.Split` (prose→`{Text,Code:false}`, code→`{Text,Code:true}`, blanks skipped); one `translateOnce` with `Request{Bilingual:true, Segments:…, Source, Target, Extra}`; map the returned `Bilingual[n]` back to the prose blocks' original indices; render via `bitext.Render`. Keep echo-suppression.
  - **Fallback**: if doc mode has no LLM provider, or the call errors / returns unparseable JSON / wrong segment count → fall back to `blocks` with a one-line stderr note (never silent). This also covers `--engine google` + doc (google can't emit JSON).
- Dispatch in the pipe branch passes `flagBilingualMode`.

### 4. Tests
- `internal/engine/prompt_test.go`: `buildBilingualPrompt` includes the CLI-doc directive ("not an abbreviation"), the JSON-only instruction, and the expanded target name.
- New parse test: `finalizeBilingual`/`extractJSON` recovers the JSON from a reply with leading reasoning prose (guards the leak fix).
- `internal/bitext` unchanged.

## Comparison (the "先比一下" step — after implementing)

Run both on the same input and show side-by-side:
```
tldr rg | translate --to zh-TW --bilingual --bilingual-mode doc     # new
tldr rg | translate --to zh-TW --bilingual --bilingual-mode blocks  # old
```
Report: `rg` header handling, Simplified/Traditional consistency, reasoning-leak presence, and rough token/latency (doc = 1 call, blocks = N). Use the user's default engine (LLM) — that's where the differences show.

## Verification
1. `just check` + `go test ./...` green; `gofmt` clean.
2. Doc mode e2e on real `tldr rg` (default engine): `rg` no longer becomes an abbreviation; all 繁體; no leaked reasoning; command examples still untranslated.
3. Fallback e2e: `--bilingual-mode doc --engine google` → falls back to blocks with a stderr note (still produces output).
4. `blocks` mode unchanged from today.
5. Ship: new flag → **minor** bump (`v0.4.0` per AGENTS.md); bump the tap `Formula/translate.rb` (url + sha256) and `brew upgrade` to verify on this Mac.

## Notes / trade-offs
- doc mode is non-streaming (fine for a reading view) and needs an LLM provider (blocks mode remains the any-engine path).
- Very large piped docs could exceed the token cap → future chunking-with-overlap; tldr/`--help`/man are small. Not handled in v1 (fall back or truncate-warn).
- Default flips `--bilingual` to `doc` (better + cheaper); `blocks` stays one flag away.
