# Plan: `[cli]`/`[tui]` config split + `smart-dict` engine

## Context

Two gaps prompted this work:

1. **CLI and TUI share one `[general]` block.** `Resolve` (`internal/config/resolve.go:46`) is
   called exactly once (`cmd/root.go:112`) *before* the CLI-vs-TUI branch, so the one-shot CLI and
   the interactive TUI cannot have different defaults. Users want e.g. CLI = `concise`/`fast` for
   quick piped translation, TUI = `contextual`/`max` for exploration. Today the only workarounds are
   env-var aliases or juggling `$TRANSLATE_CONFIG` files.

2. **The dictionary is purely offline/deterministic.** On a miss it returns either a `Suggestions`-only
   "did you mean" result or a terminal `ErrNoDictEntry` (`internal/engine/localdict.go:92,118`), never
   the actual meaning. We add a **distinct `smart-dict` engine** (per user choice — not a boolean bolted
   onto the existing dict) that keeps close typo suggestions but falls back to the LLM when a lookup
   misses **or** the fuzzy match is too far off, returning a translation **plus example sentences**.

Decisions locked with the user:
- Fallback trigger: **miss OR fuzzy too far** (close typos still show "did you mean").
- LLM output: **dictionary preset** (gloss + 1–2 example sentences).
- Packaging: **a separate engine type**, selectable — not an always-on flag.

---

## Feature 1 — `[cli]` / `[tui]` config overlays

Precedence becomes **flag > env (`TRANSLATE_*`) > `[cli]`/`[tui]` > `[general]` > default**.
The overlay sits at the "config" position, so flags and env still win.

### `internal/config/config.go`
- Add an `Overlay` struct of **pointer** fields (nil = "inherit from `[general]`" — required so we can
  tell "unset" from "set to zero value", which `Load()`'s Default()+Unmarshal cannot otherwise do):
  ```go
  type Overlay struct {
      Engine        *string `toml:"engine,omitempty"`
      Tier          *string `toml:"tier,omitempty"`
      Preset        *string `toml:"preset,omitempty"`
      Instructions  *string `toml:"instructions,omitempty"`
      Model         *string `toml:"model,omitempty"`
      DefaultTarget *string `toml:"default_target,omitempty"`
      DefaultSource *string `toml:"default_source,omitempty"`
      Pair          *bool   `toml:"pair,omitempty"`
      PairWith      *string `toml:"pair_with,omitempty"`
      Stream        *bool   `toml:"stream,omitempty"`
      LiveTranslate *bool   `toml:"live_translate,omitempty"`
      DebounceMs    *int    `toml:"debounce_ms,omitempty"`
  }
  ```
- Add to `Config`: `CLI *Overlay `toml:"cli,omitempty"`` and `TUI *Overlay `toml:"tui,omitempty"``.
  `Default()` leaves both nil so first-run config stays clean (omitempty → not serialized).

### `internal/config/resolve.go`
- Add invocation mode: `type Mode int; const ( ModeCLI Mode = iota; ModeTUI )`.
- Change signature to `func (c *Config) Resolve(o Overrides, mode Mode) Resolved`.
- Compute an effective `General` before the existing `pick()` calls:
  ```go
  g := c.General
  applyOverlay(&g, overlayFor(c, mode))   // overlayFor returns c.CLI or c.TUI (may be nil → no-op)
  ```
  `applyOverlay` copies each non-nil pointer onto `g`. Then replace every `c.General.X` in the
  `pick(...)`/`Pair` lines with `g.X`.
- Introduce a config-level model source: the overlay's `Model` becomes the cfg fallback for the model
  pick — `r.Model = pick(o.Model, "TRANSLATE_MODEL", derefOr(overlay.Model, ""))`, still falling through
  to `Provider.ModelForTier(tier)` when empty.
- Add `LiveTranslate bool` and `DebounceMs int` to `Resolved`, populated from `g`, so the TUI no longer
  reads `res.Cfg.General.*` directly (which would bypass the overlay).

### `cmd/root.go`
- New helper mirroring the existing dispatch switch (`root.go:134-164`):
  ```go
  func invocationMode(args []string) config.Mode {
      if len(args) > 0 { return config.ModeCLI }
      if !term.IsTerminal(int(os.Stdin.Fd())) { return config.ModeCLI }
      return config.ModeTUI
  }
  ```
- `runRoot`: `res := cfg.Resolve(overrides(), invocationMode(args))` (compute mode before Resolve).
- `runTUI`: use `res.LiveTranslate` / `res.DebounceMs` instead of `res.Cfg.General.LiveTranslate/DebounceMs`
  (`root.go:275-276`). `remember_last_pair` stays a `[general]`-level global (read via `cfg.General`).

*(Docs: note the new precedence + `[cli]`/`[tui]` example in the README/config help. `translate init`
stays `[general]`-only for now — out of scope.)*

---

## Feature 2 — `smart-dict` engine (dict → LLM fallback)

A new composing engine, mirroring the existing embed-and-delegate precedent
(`LocalDictEngine.APIFallback`, `localdict.go:96-103`) but branching on **result quality**, which is a
first for this codebase (`Chain` never inspects result content).

### Surface the fuzzy distance (needed for "too far off")
`wordIndex.nearestN` (`internal/engine/dict.go:229`) computes Levenshtein distance then discards it.
- Change it to `nearestN(word, maxDist, n) ([]string, int)` returning the **best (min) distance** among
  results (0 when none).
- Add a transient field to `TranslateResult` (`internal/engine/engine.go`):
  `SuggestDistance int `json:"-"`` (0 = not computed / not applicable).
- `suggestResult(sugg, req, bestDist)` sets it. Update call sites:
  - `localdict.go:114` (English) → pass the real distance (1 or 2).
  - `localdict.go:88` (Chinese `prefixSuggest`, no edit distance) → pass 0.
  - `dict.go` online-engine miss path (~`dict.go:91-92`) → same treatment.

### `internal/engine/smartdict.go` (new)
```go
type SmartDictConfig struct { CloseDistance int; Preset string } // Preset default "dictionary"
type SmartDictEngine struct { dict, llm Engine; cfg SmartDictConfig }
func NewSmartDict(dict, llm Engine, cfg SmartDictConfig) *SmartDictEngine
func (e *SmartDictEngine) Name() string        { return "smart-dict" }
func (e *SmartDictEngine) Supports(m Mode) bool { return m == ModeDict }
func (e *SmartDictEngine) Available(ctx) bool   { return e.dict.Available(ctx) || e.llm.Available(ctx) }
```
`Translate(ctx, req)` returns a channel driven by a goroutine:
1. Drain `dict.Translate` (single-shot) via `engine.Drain`.
2. **Decision:**
   - `Dictionary != nil` → exact hit → re-emit unchanged (`single`).
   - `len(Suggestions) > 0 && SuggestDistance != 0 && SuggestDistance <= CloseDistance` → close typo →
     re-emit the "did you mean" result unchanged.
   - otherwise (`ErrNoDictEntry` error, `Notes`-only "not installed", suggestions too far, or any zh
     miss where distance is unknown=0) → **LLM fallback**.
3. **LLM fallback:** build `r := req; r.Mode = ModeTranslate; r.Preset = cfg.Preset;
   r.Target = smartTarget(req.Text, req.Target)`, then pipe `llm.Translate(ctx, r)`'s chunks through
   (streaming preserved). On the terminal `ChunkDone`, stamp
   `Warnings = append(..., "no dictionary entry for %q — defined via %s (LLM)")` so the downgrade is
   never silent (consistent with `Chain`'s ethos). `smartTarget`: `lang.IsChinese(text)` → `"en"`, else
   the request target (fallback `"en"`).

This yields, for a missed word, a gloss + example sentences rendered by the **existing** result pane
(`renderResult` priority Dictionary→Suggestions→Translation, `view.go:162`): the fallback result has
only `Translation`, so it renders as a normal translation with a `⚠` note.

### Wiring — `cmd/build.go`
- `func smartDictFromConfig(res config.Resolved) engine.Engine`:
  `dict := dictFromConfig(res.Cfg)`, `llm := llmFromProvider(res.Provider, res.Model)`,
  return `engine.NewSmartDict(dict, llm, engine.SmartDictConfig{ CloseDistance: res.Cfg.SmartDict.CloseDistance, Preset: presetOr(res.Cfg.SmartDict.Preset, engine.PresetDictionary) })`.
  Caller guards `res.Provider != nil`.
- `buildEngineSet` (`build.go:108`): after the existing `"dictionary"` entry, when
  `cfg.Dict.Enabled && res.Provider != nil`, append a distinct
  `tui.NamedEngine{Name: "smart-dict", Engine: smartDictFromConfig(res), Mode: engine.ModeDict}` — so
  the TUI `^e` cycle offers plain **and** smart dictionary.

### CLI — `cmd/define.go`
- Resolve a provider: `res := cfg.Resolve(overrides(), config.ModeCLI)` (also lets `define` honor
  `--model`/`--tier`).
- Engine selection: `--plain` flag (or no provider) → `dictFromConfig(cfg)`; otherwise
  `smartDictFromConfig(res)`. Default = smart when a provider resolves; `[smartdict] define_default`
  (bool) can flip the default. `renderDict` already handles a `Translation`-only result (`define.go:60`).

### Config — `internal/config/config.go`
```toml
[smartdict]
close_distance = 1          # en edit-distance <= this stays "did you mean"; beyond → LLM
preset         = "dictionary"
define_default = true       # `translate define` uses smart-dict when a provider is available
```
`Default()`: `CloseDistance: 1`, `Preset: "dictionary"`, `DefineDefault: true`.

---

## Files to modify
- `internal/config/config.go` — `Overlay`, `Config.CLI/TUI`, `SmartDict` struct + defaults.
- `internal/config/resolve.go` — `Mode`, `applyOverlay`, model-from-overlay, `Resolved.LiveTranslate/DebounceMs`.
- `cmd/root.go` — `invocationMode`, pass mode to `Resolve`, TUI reads `res.LiveTranslate/DebounceMs`.
- `internal/engine/engine.go` — `TranslateResult.SuggestDistance`.
- `internal/engine/dict.go` — `nearestN` returns best distance; online miss path sets it.
- `internal/engine/localdict.go` — `suggestResult` gains `bestDist`; en/zh call sites updated.
- `internal/engine/smartdict.go` — **new** `SmartDictEngine`.
- `cmd/build.go` — `smartDictFromConfig`; `buildEngineSet` adds the `smart-dict` entry.
- `cmd/define.go` — resolve provider, `--plain` flag, use smart-dict by default when available.
- README / config docs — precedence + `[cli]`/`[tui]`/`[smartdict]` examples.

## Verification
- `go build ./... && go test ./...`.
- **New unit tests:**
  - `resolve_test.go`: config with `[cli] preset="concise"` + `[tui] preset="contextual"` →
    `Resolve(o, ModeCLI).Preset=="concise"`, `Resolve(o, ModeTUI).Preset=="contextual"`; a flag/env
    still overrides both.
  - `smartdict_test.go` (mirror `internal/engine/llm_test.go` fakes): fake dict returning (a) exact hit,
    (b) suggestions dist=1, (c) suggestions dist=2, (d) `ErrNoDictEntry`; fake llm. Assert LLM fires
    only for (c)/(d), the hit/near-typo pass through unchanged, and the fallback result carries the
    `⚠ no dictionary entry` warning + the llm Translation.
- **Manual (drive the real binary):**
  - `translate define zzzznotaword` → LLM gloss + examples with `⚠` warning.
  - `translate define helllo` → distance-1 "did you mean: hello" preserved (no LLM).
  - `translate define --plain zzzznotaword` → pure miss (no fallback).
  - TUI: `^e` cycles to `smart-dict`; a missed word streams an LLM definition.
  - Split defaults: set `[cli] preset="concise"`, `[tui] preset="contextual"`; confirm
    `echo hola | translate --to en` uses concise while bare `translate` (TUI) starts contextual.
