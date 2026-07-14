# Learn mode (語言學習 / 文法修正) — bidirectional pedagogical mode

## Context

Today the tool has three intents: **translate** (bare args; presets concise/contextual/dictionary),
**define** (offline/​smart dictionary), and **speak** (TTS). None of them help a learner *practice* —
they translate or look up, they don't teach or correct.

The user wants a foreign-language **learning mode** that is a specialised LLM prompt. The design fork
we resolved with the user:

- **Scope = 兩者合一（自動判方向）**: one mode that adapts to the input direction —
  - **native → foreign** (母語輸入): translate *and teach* — idiomatic translation + vocab glosses
    (part-of-speech, phonetics/KK, native-language meaning) + 1–2 example sentences.
  - **foreign → native** (外語輸入): **grammar-correct** the sentence + explain each error in the
    native language + give the native translation.
- **Entry = pair-mode extension (`--learn` toggle)**: not a new `^e` engine-cycle entry; a boolean
  toggle layered on the existing bidirectional *pair* concept, reusing pair's offline direction
  detection. Direction is auto-detected; the user just types.
- **Output = structured fields + dedicated rendering**: new typed result fields + a bespoke renderer
  (CLI + TUI), not free-form text.

Why this shape avoids the "入口 confuse" problem: `translate`/`define` overlap because "learn" as a
mere *output style* looks the same at the input. Making it a **distinct toggle with a distinct,
adaptive task** (teach vs correct) gives it its own clear intent, while reusing pair's machinery keeps
the surface small.

This plan was validated by a design review that confirmed the orthogonal-flag approach (no new
`engine.Mode`) and surfaced **three correctness bugs** now folded in (see *Correctness fixes*).

## Key existing pieces we reuse (do not reinvent)

- **Offline direction detection**: `lang.PairTarget(home, away, text)` (`internal/lang/detect.go:64`)
  — returns the *other* language; reliable for zh↔en (CJK vs Latin script), offline, short input.
  `PairTarget(home,away,text) == away` ⇒ input was native → **teach**; `== home` ⇒ foreign → **correct**.
- **Prompt chokepoint**: system prompts assemble in `internal/engine/prompt.go`
  (`buildTranslatePrompt`, `systemPromptFor`). Add a sibling `buildLearnPrompt`.
- **Pair plumbing**: `Request.Pair/PairHome/PairAway` (`internal/engine/engine.go:42`) already carries
  the two languages through CLI `oneShot` and TUI `launch`.
- **Structured result precedent**: `TranslateResult.Dictionary *DictEntry` + `renderDictionary`
  (`internal/tui/view.go:197`) is the exact template for a typed payload + dedicated renderer.
- **Per-front-end overlay**: `config.Overlay` + `resolve.go` `pick(...)` lets `[cli]`/`[tui]` default
  learn on/off, mirroring how `Pair` resolves (`resolve.go:143`).
- **CLI direct-engine pattern**: `define` builds a *specific* engine (`defineEngine`) rather than the
  resolved default — do the same for learn (`llmFromProvider`).
- **Non-stream contract**: engines emit exactly one `ChunkDone` (engine.go:127-142). The TUI
  `handleStream` (update.go:90) makes **no assumption** that a `ChunkToken` arrives first, and the
  spinner placeholder animates the whole time `status==statusTranslating && streamBuf==""` — so a
  non-streaming learn request shows a clean spinner, then flips to the structured view.

## Correctness fixes (must-do — found in review)

1. **`res.Target = req.PairAway`** (the foreign language) in `finalizeLearn`, **both directions** —
   because `res.Translation` always holds the foreign sentence. Using `req.Target` mislabels the
   correct-direction result (target=native, translation=foreign) and misroutes speak/history. *High.*
2. **Extend the session cache key**: `cacheKey`/`cacheKeyFor` (`internal/tui/cache.go:13`) currently
   hashes `{preset, engineName, model, source, target, text}` — no `learn`/`pair`. A learn result and a
   plain translation of the same text **collide**. Add `learn bool` (and, fixing a pre-existing latent
   pair collision, `pair bool` + `pairWith string`). Direction need not be in the key — it is
   deterministic from `text`, which is already hashed. *High.*
3. **Footer / key-gating coherence** while learn bypasses `m.active()` (details in §6). *Medium.*

## Design

### 1. Request/result types — `internal/engine/engine.go`

- Add `Learn bool` to `Request` (orthogonal flag, like `Pair`; **no new `engine.Mode`** — `LLMEngine`
  never reads `Mode`, and the learn path uses a bare `*LLMEngine`, not a chain, so `Supports` is never
  consulted).
- Add `Learn *LearnResult json:"learn,omitempty"` to `TranslateResult` (auto-flows into `--json`).

```go
type LearnResult struct {
    Direction   string         `json:"direction"`             // "teach" | "correct"
    Original    string         `json:"original"`              // user input as received
    Corrected   string         `json:"corrected,omitempty"`   // correct-dir: fixed foreign sentence
    Translation string         `json:"translation"`           // teach: foreign translation; correct: native translation
    Notes       string         `json:"notes,omitempty"`       // short tip, in the NATIVE language
    Issues      []LearnIssue   `json:"issues,omitempty"`      // correct-dir grammar/usage fixes
    Vocab       []LearnGloss   `json:"vocab,omitempty"`       // teach-dir glosses
    Examples    []LearnExample `json:"examples,omitempty"`    // teach-dir usage
}
type LearnIssue   struct { Span, Fix, Explanation string } // json: span/fix/explanation
type LearnGloss   struct { Term, Pos, Phonetic, Meaning string }
type LearnExample struct { Foreign, Native string }
```

- The engine **also sets `res.Translation`** to the main foreign-language sentence (correct-dir:
  `Corrected`; teach-dir: `Translation`). This keeps copy (`copyText`), history (`recordFor`/`toRecord`),
  and speak side-selection working with **zero changes** — the structured extras ride in `res.Learn`.
  (History's `store.Record` has no field for the structured payload → only the sentence + notes persist;
  acceptable, document it.)

### 2. Prompt — `internal/engine/prompt.go`

- `learnDirection(req) string` → `"teach"` if `lang.PairTarget(req.PairHome, req.PairAway, req.Text) ==
  req.PairAway`, else `"correct"`. (Trust this offline detection over whatever the model says.)
- `buildLearnPrompt(req) (system, user string)`: pick `learnTeachPrompt` or `learnCorrectPrompt`, name
  the home/away languages via `lang.Name`, append `req.Extra` (reuse the "User preferences" convention).
- Both prompts **demand ONE strict JSON object, no markdown fence, no prose**, filling only
  direction-relevant fields, with all glosses/explanations/notes **in the native (home) language**.
  Phonetics: KK/IPA for English, pinyin for Chinese. Caps: `vocab` ≤ ~8 key content words, 1–2 `examples`.
  Correct-dir: if already correct, echo `corrected` with `issues: []`.

### 3. Engine — `internal/engine/llm.go`

- **Force non-streaming for learn inside the engine**, not the caller: compute
  `stream := req.Stream && !req.Learn` and use it for the wire request + SSE branch decision. Robust
  against the TUI always passing `Stream:true`.
- In `translateOpenAI`/`translateAnthropic`: when `req.Learn`, swap `buildTranslatePrompt` →
  `buildLearnPrompt`, and `finalize` → `finalizeLearn`. Reuse all existing auth/decode/`httpError`/
  truncation plumbing and the shared `if !complete { markTruncated(res) }`.
- **Raise the token cap for learn**: `anthropicMaxTokens = 4096` (llm.go:128) truncates gloss-rich JSON;
  use ~8192 for learn requests specifically.
- `finalizeLearn(full, model, req)`:
  - Extract JSON defensively: trim → strip ```` ```json ```` fences → slice first `{` … last `}` →
    `json.Unmarshal` into `LearnResult`.
  - Success: `res.Learn = &lr`; `lr.Direction = learnDirection(req)`;
    `res.Translation =` `lr.Corrected` (correct-dir, if non-empty) else `lr.Translation`;
    **`res.Target = req.PairAway`**; fill `DetectedSource` as today.
  - Failure: `res.Translation = strings.TrimSpace(full)` + `Warning` ("could not parse structured learn
    output"). Never a hard error.

### 4. Routing — CLI `cmd/root.go` + `cmd/build.go`

- Flag: `f.BoolVar(&flagLearn, "learn", false, "learning mode: teach (native→foreign) or grammar-correct (foreign→native)")`; `overrides()` carries `Learn: flagLearn`.
- In `runRoot`, after `Resolve`:
  - **Provider gate before dispatch** (the existing gate at root.go:157 only fires for non-`auto`
    engines, so learn+auto+no-provider would slip through):
    `if res.Learn && res.Provider == nil { return fmt.Errorf("learn mode requires an LLM provider; check %s", config.Path()) }`.
  - **Pair defaulting** (post-`resolvePair`, where `tgt`/`pairWith` are known):
    `if res.Learn && (pairWith == "" || strings.EqualFold(pairWith, tgt)) { pairWith = defaultAway(tgt) }`
    where `defaultAway` mirrors the `^g` heuristic (`strings.HasPrefix(lower(tgt),"en") ? "zh-TW" : "en"`,
    update.go:214). Also set `res.Pair = true` when `res.Learn` (do it in `Resolve`).
  - **Engine swap** (after `buildEngine`): `if res.Learn { eng = llmFromProvider(res.Provider, res.Model) }`
    — bypasses smart-auto/dictionary routing.
- `oneShot` gains a `learn bool` param: set `req.Learn`, `req.Mode = engine.ModeTranslate` (explicit;
  don't inherit a possibly-dict Mode), and gate streaming off:
  `stream := streamPref && stdoutTTY && !flagJSON && !learn` (so `onTok` stays nil). In the final
  `switch`, add `case res.Learn != nil:` **before** `default`, calling `renderLearnCLI(res)` — a
  pipe-safe plain-text block (headline = corrected/translation, ✎ issues, vocab list, examples),
  modeled on `renderDict` (define.go:84). `--json` already dumps the full struct.
- `cmd/build.go`: `learnEngineFromConfig(res) engine.Engine` (= `llmFromProvider(res.Provider, res.Model)`),
  used by the CLI path and passed to `runTUI` as `Params.LearnEngine`.

### 5. Config — `internal/config/config.go` + `resolve.go`

- `General.Learn bool` (`toml:"learn"`, default false); `Overlay.Learn *bool`; `Overrides.Learn bool`;
  `Resolved.Learn bool`. Wire `applyOverlay` (copy non-nil) and `Resolve`
  (`Learn: o.Learn || g.Learn || envVal("TRANSLATE_LEARN") != ""`, mirroring `Pair` at resolve.go:143);
  set `r.Pair = true` when `r.Learn`.

### 6. TUI — `keys.go`, `model.go`, `update.go`, `view.go`, `cache.go`

- `keys.go`: add `Learn key.Binding` = `ctrl+n` (help `^n learn`). Free key; `handleKey`'s global switch
  intercepts before `ta.Update`, same as the working `^p`.
- `model.go`: add `learn bool` to `Model`; `Params.Learn` (initial) + `Params.LearnEngine engine.Engine`
  (nil when no provider); init `learn: p.Learn`.
- `cache.go`: extend `cacheKey` + `cacheKeyFor` with `learn`(+`pair`,`pairWith`) — see *Correctness fix #2*.
- `update.go`:
  - **Toggle** (mirror `TogglePair`, update.go:211): `if turning on && m.p.LearnEngine == nil` → flash
    "learn needs an LLM provider" and revert; else `m.learn = !m.learn`, set **`m.pair = true`** and
    default `m.pairWith` (so `foreignPref()` returns the away language for speak), `relayout()`,
    re-launch if live else `clearResult()`.
  - `launch()` (update.go:352): when `m.learn`, route to `m.p.LearnEngine` (not `m.active().Engine`),
    and build `engine.Request{ Learn:true, Mode: engine.ModeTranslate, Stream:false, Pair:true,
    PairHome:m.target, PairAway:m.pairWith, Model:m.modelOverride, ModelProvider:m.p.ModelProvider,
    Extra:m.p.Instructions, ... }`.
  - **Freeze `^e`** while learn is on (no-op / flash); **relax the `^p` guard** to
    `if !m.learn && m.active().Mode == engine.ModeDict` (model picker genuinely affects learn via
    `req.Model`); **no-op `^o`** (learn has its own prompt).
- `view.go`:
  - `footerContent`: add a dedicated `m.learn` branch at the top (structured like the `ModeDict` branch,
    view.go:124, single-line for stable `footerHeight`): `learn <home>⇄<away>` + `curEngine`/`curModel`
    + `live` + a learn help line with `^n learn`. This sidesteps every `m.active()` segment.
  - `renderResult`: `if res.Learn != nil { return m.renderLearn(res) }` at the top; add `renderLearn`
    modeled on `renderDictionary` — headline (corrected/translation) in `st.trans`, `✎` issues in
    `st.warn`/`st.notes`, vocab & examples in `st.alt`/`st.dim`.

### 7. Tests

- `internal/engine` — `finalizeLearn` table test: clean JSON, ```` ```json ```` fence, JSON-in-prose,
  and malformed/truncated → assert `LearnResult` fields, `res.Translation`, `res.Target == PairAway`
  (both directions), and the fallback warning.
- `internal/engine` — `learnDirection`/`buildLearnPrompt`: zh input → teach, en input → correct for a
  zh-TW⇄en pair; assert the right system prompt is chosen.
- `internal/tui` — `cacheKey`: same text learn-on vs learn-off yields distinct keys.
- `internal/config` — mirror `tts_test.go`: `[tui] learn = true` overlay resolves; partial unmarshal
  keeps other defaults.

## Files to touch

- `internal/engine/engine.go` — `Request.Learn`; `TranslateResult.Learn`; `LearnResult` & sub-structs.
- `internal/engine/prompt.go` — `learnDirection`, `buildLearnPrompt`, the two learn system prompts.
- `internal/engine/llm.go` — non-stream force, learn token cap, learn branch in both transports, `finalizeLearn`.
- `cmd/root.go` — `--learn` flag, provider gate, pair defaulting, engine swap, `oneShot(learn)`, `renderLearnCLI`, `runTUI` Params.
- `cmd/build.go` — `learnEngineFromConfig`.
- `internal/config/config.go`, `internal/config/resolve.go` — Learn in General/Overlay/Overrides/Resolved + `learn⇒pair`.
- `internal/tui/keys.go`, `model.go`, `update.go`, `view.go`, `cache.go` — toggle key, state, cache key, launch routing, footer + `renderLearn`.
- Tests as above.

## Verification (end-to-end)

Prereq: copilot-proxy at `http://localhost:4141/v1` (default provider); default tier `fast` →
`claude-haiku-4-5` (verified-working; memory `copilot-proxy-model-availability`).

1. `go build ./...` && `go test ./...`.
2. CLI teach: `translate --learn --to zh-TW --pair-with en "我昨天去公園散步"`
   → English translation + vocab (pos/KK/meaning) + example sentences.
3. CLI correct: `translate --learn --to zh-TW --pair-with en "I has a apple"`
   → corrected sentence + issues (span→fix + 中文解釋) + 中文翻譯.
4. `--json` on both includes the `learn` payload; non-learn results omit it.
5. Provider-absent: `--engine google --learn "x"` errors cleanly ("requires an LLM provider").
6. Malformed-JSON resilience (unit test or forced bad reply): raw text + ⚠ warning, no crash.
7. TUI: launch, `^n` (footer shows `learn zh⇄en`); Chinese → teach; English → correct; `^y` copies the
   foreign sentence; `^s` speaks the foreign side; history records it; `^e`/`^o` frozen, `^p` works.
8. Cache: type the **same** text learn-on then learn-off → results differ (no collision).
9. Truncation path: long input → one auto-retry then graceful fallback warning.
