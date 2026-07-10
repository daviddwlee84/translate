# Fix: silent LLM stream truncation + record pitfall

## Context

A live translation rendered only half its output — cut mid-word at 「英文翻」
(should be 「英文翻譯」) — yet was presented as a finished result (with the normal
`detected: english` line, no error). A language model never stops mid-word; a
natural stop lands on a punctuation/sentence boundary. So the stream was
**truncated** (copilot-proxy / Copilot upstream dropped the SSE connection
mid-generation, an intermittent behaviour), not a model decision. Not a
`max_tokens` hit either: the cap is 4096, this output was ~150 tokens.

Root cause is in the code, not just the proxy: both streaming readers in
`internal/engine/llm.go` treat **any clean EOF as success**. `readAnthropicSSE`
ignores every event except `content_block_delta` (so `message_delta` /
`message_stop` are dropped) and returns `sc.Err()`, which is `nil` on a clean
EOF; `readSSE` `break`s on `[DONE]` but records no flag, so a stream that ends
*without* `[DONE]` is indistinguishable from one that finished. The caller then
emits a normal `ChunkDone` with whatever partial text accumulated.

Goal: detect incomplete streams and make them **visible** — keep the partial
text but attach a `⚠` truncation warning (user's chosen UX: non-blocking,
manual "press Enter to retry"), don't cache or persist the partial result, and
record the trap in `pitfalls/`.

## Changes

### 1. `internal/engine/engine.go` — transient truncation flag
Add one field to `TranslateResult` (next to `Warnings`):
```go
// Truncated is set when a streamed result ended before the model finished
// (no terminal marker / stop_reason=="max_tokens"). Transient: not persisted.
Truncated bool `json:"-"`
```

### 2. `internal/engine/llm.go` — assert stream completeness
- **Parse the terminal signals** the readers currently ignore:
  - `streamDelta.Choices[]`: add `FinishReason *string \`json:"finish_reason"\``.
  - `anthropicStreamEvent.Delta`: add `StopReason string \`json:"stop_reason"\``
    (populated on `message_delta`, empty on `text_delta` — one struct covers both).
- **Change both reader signatures** to `(complete bool, err error)`:
  - `readSSE`: track `sawDone` (set at `[DONE]`) and `finish` (last non-nil
    `finish_reason`). Rule: `truncated` if `finish=="length"`; `complete` if
    `sawDone || finish=="stop"`; otherwise (no terminal marker) truncated.
  - `readAnthropicSSE`: track `sawStop` (`ev.Type=="message_stop"`) and
    `stopReason` (from `message_delta`). Rule: `truncated` if
    `stopReason=="max_tokens"`; `complete` if
    `sawStop || stopReason=="end_turn" || stopReason=="stop_sequence"`;
    otherwise truncated. Keep the existing `ev.Error` passthrough (returns an error).
  - This rule accepts **either** a `stop_reason` **or** a `message_stop`, so a
    proxy that forwards only one of the two terminal events still reads as
    complete (guards against false positives — see Verification).
- **Both streaming callers** (`translateOpenAI`, `translateAnthropic`):
  ```go
  complete, err := e.readAnthropicSSE(ctx, resp.Body, ch, &full)
  if err != nil { ch <- Chunk{Kind: ChunkError, Err: err}; return }
  res := e.finalize(full.String(), model, req)
  if !complete { markTruncated(res) }
  ch <- Chunk{Kind: ChunkDone, Result: res}
  ```
- Add a small helper:
  ```go
  func markTruncated(res *TranslateResult) {
      res.Truncated = true
      res.Warnings = append(res.Warnings,
          "output was cut off before completion (stream truncated) — press Enter to retry")
  }
  ```
- No new sentinel error needed: truncation is a non-fatal `(false, nil)`, carried
  on the result. The CLI `Drain` path returns the result with `Truncated`/`Warnings`
  set, so it can surface it too.

### 3. `internal/tui/update.go` — don't cache or persist a truncated result
In `handleStream` (`ChunkDone` branch), when `msg.chunk.Result.Truncated`:
- skip the write-through cache (`m.cache[m.pendingKey] = …`) so Enter/live re-fetches
  instead of replaying the stale partial;
- skip `saveHistoryCmd` — change the save guard to
  `if !r.Truncated && (r.Dictionary != nil || r.Translation != "")`.
Still render it and set `statusDone` (the `⚠` reads as a settled, retryable state).

### 4. `internal/tui/view.go` — already handled
`renderResult` (lines 181–186) already renders each `res.Warnings` with `⚠` and a
follow-up hint line, styled via the existing `m.st.warn`. No structural change.
Optional polish: when `res.Truncated`, swap the generic
`(^e switch engine · check the model/provider)` hint for `(按 Enter 重試)`.

### 5. `internal/engine/llm_test.go` — first test in the repo
Add table tests driving `e.Translate` against an `httptest.Server` that returns
canned SSE, draining the channel via `engine.Drain`:
- Anthropic full stream (`content_block_delta`* + `message_delta` end_turn +
  `message_stop`) → `Truncated==false`, full text.
- Anthropic dropped stream (deltas then abrupt close, no `message_stop`) →
  `Truncated==true`, `Warnings` non-empty, **partial text preserved**.
- Anthropic `stop_reason:"max_tokens"` → `Truncated==true`.
- OpenAI `[DONE]` vs no-`[DONE]` and `finish_reason:"length"` → mirror the above.

### 6. Docs — record the pitfall
- New `pitfalls/llm-stream-truncation-silently-rendered-as-complete.md` in house
  format (bold-label header: **Symptoms** / **First seen**: 2026-07 / **Affects**:
  translate + copilot-proxy claude-sonnet-5 streaming / **Status**: fixed; then
  `## Symptom` / `## Root cause` / `## Workaround` / `## Prevention` / `## Related`).
  Symptom section quotes the grep-able fingerprint (output cut mid-word, no error).
  Cross-link the `copilot-proxy-model-availability` memory and the 60s-timeout note below.
- Add an alphabetical row to the `## Index` table in `pitfalls/README.md`.
- Add a `TODO.md` **P3** follow-up: the streaming path uses `http.Client{Timeout:
  60s}`, which caps the *entire* streamed read — a long translation is cut at 60s
  via the same path (now visible as `⚠ truncated`, but still worth fixing by
  relying on `ctx` deadlines instead of a whole-request timeout).

## Verification

1. `go build ./... && go vet ./...`
2. `go test ./internal/engine/` — new truncation tests pass.
3. **False-positive gate (do this first, against the live proxy):** confirm
   copilot-proxy actually forwards a terminal marker for a *successful* stream,
   so the completeness rule doesn't flag every translation. Capture the raw SSE:
   `curl -N -s http://localhost:4141/v1/messages -H 'content-type: application/json'
   -H 'anthropic-version: 2023-06-01' -d '{"model":"claude-sonnet-5","max_tokens":1024,
   "stream":true,"messages":[{"role":"user","content":"say hi"}]}'` and verify the
   tail contains `message_delta` (with `stop_reason`) and/or `message_stop`. If it
   emits *neither*, the plan's rule must fall back to a different signal — revisit
   before shipping.
4. Live TUI check: run the binary, `live` on, engine copilot / claude-sonnet-5,
   translate a full paragraph. Confirm a normal completion shows **no** `⚠`, and a
   truncated one (reproduce by pointing at a flaky run, or a test stub) shows the
   partial text + `⚠`, is retryable with Enter, and does **not** appear in `^r` history.
