# TranslateGemma follow-ups

**Status**: P? — base support shipped, follow-ups deferred (2026-11)
**Effort**: M
**Related**: `TODO.md` · `internal/engine/translategemma.go` · `internal/hostinfo/` · [../docs/translategemma.md](../docs/translategemma.md)

## Context

2026-11-06. `translate` now uses the official prompt for any `translategemma*`
model and `init` suggests a size. Test data and user-facing limits are in
[../docs/translategemma.md](../docs/translategemma.md). This page records what was
deliberately left out and why.

## Decisions worth keeping

- **Dispatch on the model name, no config field.** Same approach as `claude*` in
  `inferTransport`. It needs no `SchemaVersion` bump and works however the model
  is selected.
- **`zh-TW`, never `zh-Hant`.** On `4b`, `zh-Hant` gave 0/20 Traditional and `zh-TW`
  15/20. Don't "correct" this to the more standard-looking script subtag.
- **An undetected source is labelled English instead of erroring.** `whatlanggo`
  is under 0.5 confidence on most short text ("hello" → `so`, 0.01), so a required
  source blocked ordinary use. 16 short texts gave the same translation under wrong
  labels, including a label equal to the target. This is untested on long text.
- **Warn on Simplified only when it's in the output.** A warning on every `zh-TW`
  request would be noise. `simplifiedOnlyRunes` matched by-script classification on
  all 20 test outputs. It can miss short output with none of the listed characters.
- **Document mode is rejected.** A Markdown test kept the structure but translated
  an `echo` string inside a code fence.

## Options not taken

### Simplified → Traditional post-processing

Would fix the 25% Simplified rate on `zh-TW`. The existing ECDICT TODO needs the
same converter, so they should share one.

- Character-level conversion can't fix vocabulary (`信息` → `資訊`); that needs
  phrase tables as in OpenCC `s2twp`.
- Streaming conflicts with phrase conversion, since a phrase can span chunks.
- It needs a dependency or embedded tables. No pure-Go option has been evaluated.

### Chunking long input

Would lift the 2,000-token limit. Split at paragraph boundaries and translate each
part. Sentence context would be lost across parts, and each part needs its own
truncation check. `--bilingual` per-block mode already does something similar.

### Other sizes

Only `4b` has been tested (the host had 17 GiB free). Run the same checks on `12b`
and `27b`, especially Traditional Chinese, and confirm the `init` thresholds in
`internal/hostinfo/hostinfo.go` against real memory use. `ollama ps` reported
949 MB for `4b`, well below its 3.3 GB download, so don't derive thresholds from
that figure without understanding it.

### Image translation

The model accepts images, but `chatMessage.Content` is a plain string and the CLI
has no image input.
