# Live translation shows only half the output, cut mid-word, with no error

**Symptoms** (grep this section): streamed LLM translation ends mid-word / mid-sentence
(e.g. Chinese output stops at 「英文翻」 instead of 「英文翻譯」), yet renders as a
finished result with the normal `detected: …` line and **no error banner**; partial
answer gets saved to history and cached; intermittent, more likely on longer outputs;
copilot-proxy / GitHub Copilot upstream `claude-sonnet-5` streaming.
**First seen**: 2026-07
**Affects**: `translate` TUI live mode, any streaming LLM engine (copilot-proxy
`/v1/messages` Anthropic SSE and `/chat/completions` OpenAI SSE); provoked by the
proxy/upstream dropping the SSE connection mid-generation.
**Status**: fixed — `internal/engine/llm.go` now asserts stream completeness and flags
truncation (`TranslateResult.Truncated` + a `⚠` warning); the TUI auto-retries once (a
fresh request almost always completes) and only shows the partial text + `⚠` after a
second truncation. Truncated results are never cached/persisted.

## Symptom

A live translation of a multi-line paragraph rendered only ~80% of the text, cut
mid-word:

```
需注意兩件事
- ha_export.py / haexport.py 是中文的…因此違反了套件「src/ 中 0 CJK 字元」的規則。要我進行一次英文翻
detected: english
```

Expected tail was 「要我進行一次英文翻**譯**掃描…並發布 0.8.0 嗎？」. No error was shown, so it
looked like the model chose to stop — but a language model never stops mid-word; a
natural stop lands on a punctuation/sentence boundary.

> **Note (mis-diagnosis):** the *originally reported* case with this exact symptom turned
> out to be a **display** bug, not a stream drop — the TUI viewport clipped a long
> unwrapped line while the engine had the full text. See
> [`tui-viewport-clips-long-translation-no-softwrap`](tui-viewport-clips-long-translation-no-softwrap.md).
> The fix below is a *separate*, real robustness gap: a genuine mid-stream connection drop
> would otherwise be rendered as a finished result. Both share the "truncated output, no
> error" symptom — cross-check the CLI (full text ⇒ display bug; also truncated ⇒ stream bug).

## Root cause

Both SSE readers treated **any clean EOF as success**. `readAnthropicSSE` only handled
`content_block_delta` events and dropped `message_delta` (carries `stop_reason`) and
`message_stop` (the terminal marker); it returned `sc.Err()`, which is `nil` on a clean
EOF. `readSSE` `break`ed on `[DONE]` but recorded no flag, so a stream ending *without*
`[DONE]` was indistinguishable from a completed one. When copilot-proxy or the Copilot
upstream drops the connection mid-stream (an intermittent behaviour), the reader
returned `nil`, the caller emitted a normal `ChunkDone` with whatever partial text had
accumulated, and the TUI cached + persisted it as the real answer.

Anthropic streaming *does* provide a reliable completeness signal — verified against the
live proxy, a successful stream ends with `message_delta` (`delta.stop_reason:"end_turn"`)
**and** `message_stop`; OpenAI ends with a final chunk `finish_reason:"stop"` **and**
`[DONE]`. The old code parsed none of them.

**How often does the proxy actually truncate?** Rarely. Raw `curl` reproductions of the
exact request completed cleanly **29/29** (all `end_turn` + `message_stop`, 119–176 output
tokens). So the drop is an intermittent edge, not the norm — which is why an automatic
retry reliably recovers (see Workaround).

**The recurrence trap (cost ~20 min):** after the fix was built and installed, the symptom
"still" appeared with no `⚠`. Cause was a **stale running process** — a long-lived TUI
started *before* the reinstall keeps the old binary in memory; `go install` only replaces
the on-disk file. Verify the running code, not just the file: `translate --version` prints
the VCS revision + `+dirty` marker, and `ps -eo pid,lstart,command | grep translate` shows
process start times. Restart the TUI after any reinstall.

## Workaround

Fixed in code (no runtime workaround needed). The fix, for reference:

- Parse the terminal signals: `finish_reason` on OpenAI chunks, `stop_reason` on the
  Anthropic `message_delta`, and the `message_stop` event (streaming); `stop_reason` /
  `finish_reason` on the decoded body (non-streaming, for piped/`--json` output).
- Readers return `(complete bool, err error)`. Completeness accepts **either** terminal
  marker (so a proxy that forwards only one still reads as complete — avoids
  false-positives): Anthropic `complete = stop_reason != "max_tokens" && (sawStop ||
  stop_reason in {end_turn, stop_sequence})`; OpenAI `complete = finish != "length" &&
  (sawDone || finish == "stop")`.
- On `!complete`, keep the partial text but set `TranslateResult.Truncated` and append a
  `⚠ … stream truncated` warning.
- The TUI **auto-retries once** on a truncated stream (a fresh request almost always
  completes, per the 29/29 above); only a *second* truncation settles on the partial
  text + `⚠` (press Enter to retry manually). Truncated results are never cached or saved
  to history, so retries always re-fetch.

If you see this again after a proxy change, first re-check that a *successful* stream still
emits a terminal marker (the false-positive gate):

```sh
curl -N -s http://localhost:4141/v1/messages \
  -H 'content-type: application/json' -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"claude-sonnet-5","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}' \
  | grep -E 'message_(delta|stop)'
```

If that prints nothing, the proxy stopped sending terminal markers and the completeness
rule would flag every stream — revisit before trusting the `⚠`.

## Prevention

- Never treat a streamed response as complete on EOF alone — require the protocol's
  terminal marker (`message_stop`/`[DONE]`) or an explicit finish/stop reason.
- Keep partial output visible with a warning rather than discarding or silently accepting
  it — see `TranslateResult.Warnings`/`Truncated` and `renderResult` in `internal/tui/view.go`.
- After reinstalling, confirm the *running* binary with `translate --version` and restart
  any long-lived TUI — a stale process masks the fix.
- Regression coverage: `internal/engine/llm_test.go` feeds canned complete / dropped /
  `max_tokens` / `length` bodies (streaming and non-streaming) and asserts `Truncated` +
  preserved partial text.

## Related

- Sibling reference: the `copilot-proxy-model-availability` agent memory note — Claude
  models route via `/v1/messages`; the same proxy is the truncation source.
- Follow-up: `TODO.md` P3 — the streaming path still uses `http.Client{Timeout: 60s}`,
  which caps the *entire* streamed read; a long translation is cut at 60s via this same
  path (now visible as `⚠ truncated`, but better fixed by relying on `ctx` deadlines).
- Anthropic streaming events: https://docs.anthropic.com/en/api/messages-streaming
