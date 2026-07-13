# Plan: TUI ergonomics + smart-auto default + pair-mode fix + debug mode

## Context

`translate` is a Go CLI/TUI (Cobra + Bubble Tea v2 under `charm.land/*`, TOML config,
shared `internal/engine` layer). Five issues, gathered from the user + a code/history audit:

1. **No discoverable "clear input" in the TUI.** A clear binding already exists (`ctrl+u`,
   `internal/tui/keys.go:34`, handled `internal/tui/update.go:233-242`) but is **absent from
   the footer help** (`internal/tui/view.go:128,156`), so it's effectively hidden.
2. **No focus switch / keyboard scroll.** The output `viewport` scrolls **only via mouse wheel**
   (`update.go:493-501`); no key ever reaches it (`handleKey` routes every unmatched key to the
   textarea, `update.go:257-258`). There is **no focus concept** between the input/output panes and
   no Tab handling. Input (a `textarea`) does scroll, but only by cursor movement in a 4-row box.
3. **Default mode isn't "pair + dictionary-with-LLM-fallback."** A bare `translate <word>` runs
   plain LLM translate via the auto chain. "Dictionary with LLM fallback" (`SmartDictEngine`,
   `internal/engine/smartdict.go`) already exists but is reachable only via `translate define` /
   the TUI `^e` cycle, and `translate init` never exposes it. **Decision: build a new "smart-auto"
   default** — single words → dictionary (with LLM fallback), phrases → LLM translate, bidirectional.
4. **Pair mode "not working ideally."** Root cause confirmed from the user's config + history
   (`~/.config/translate/config.toml`: `default_target='zh-TW' pair=true pair_with='en'`;
   `history.jsonl`): routing is **already correct** — English "test" was routed to `target_lang=zh-TW`
   — but the **LLM echoed** `test → test` instead of translating to `測試`. The concise prompt even
   hands the model a "return the text unchanged if source == target" escape hatch
   (`internal/engine/prompt.go:30-31`). A secondary latent bug: `lang.PairTarget`
   (`internal/lang/detect.go:36-44`) only tests the *home* language, so the reversed `home=en` config
   and short Latin input fall through incorrectly. **Decision: pair-aware LLM prompt (model detects &
   routes, never echoes) + make `PairTarget` script-symmetric.**
5. **No debug mode.** There's no way to see the intermediate decisions (resolved config, pair routing,
   word-vs-phrase classification, dict hit/miss, chain fallback) to diagnose issues. **Add one.**

Intended outcome: a TUI that's comfortable for long input (clear/focus/scroll), a `translate` default
that behaves like a smart bilingual dictionary+translator, reliable pair-mode translation that never
echoes, and a `--debug` trace to diagnose the rest.

---

## Changes

### A. TUI — surface the quick clear (point 1)  ·  `internal/tui/view.go`
- Keep `ctrl+u`'s existing behavior (resets textarea **and** result pane — `update.go:233-242`).
- Add `^u clear` to both footer help strings: normal mode `view.go:156` and dict mode `view.go:128`.

### B. TUI — focus switch + keyboard scroll for both panes (point 2)
- `internal/tui/model.go`: add a `focus` field (`focusInput`/`focusOutput`, iota; default input).
- `internal/tui/keys.go`: add `SwitchFocus` = `tab` (+ `shift+tab`), help `⇥ focus`.
- `internal/tui/update.go` `handleKey`:
  - On `SwitchFocus`: toggle `m.focus`; call `m.ta.Focus()` / `m.ta.Blur()` accordingly (the input
    border already keys off `ta.Focused()`, `view.go:24-27`).
  - Fallthrough (`update.go:257`): when `focusOutput`, forward the key to `m.vp.Update(msg)` instead of
    the textarea — this reuses the viewport's **dormant default keymap** (↑/↓, j/k, PgUp/PgDn, space, b,
    u/d half-page, g/G-ish). Exception: a printable rune while `focusOutput` **snaps focus back to input**
    and forwards the key to the textarea (so typing "just works"). When `focusInput`, keep today's path
    (arrows already scroll the textarea's internal viewport).
  - Global bindings (Enter/`^y`/`^e`/… matched earlier in the switch) keep working regardless of focus.
- `internal/tui/styles.go`: add `resultHi` (accent border, mirror `inputHi`).
  `internal/tui/view.go:32`: render the result box with `resultHi` when `focusOutput`.
- `view.go` footer: add `⇥ focus` to the help string.
- Optional nicety: `handleMouseClick` (`update.go:506`) sets focus to the clicked pane.
- Note: `tab` overrides the textarea's tab insertion — acceptable for a translate box.

### C. Smart-auto default engine (point 3)
- **New** `internal/engine/smartauto.go` — `SmartAutoEngine{ smart Engine; llm Engine; cfg }`,
  `Supports(ModeTranslate)=true`, streaming preserved.
  - `Translate`: classify the input with an `isLookup(text)` heuristic —
    *single word/term* → delegate to `SmartDictEngine` (dict → LLM fallback, `ModeDict`);
    *phrase/sentence* → delegate to the LLM (`ModeTranslate`) with the pair-aware prompt (D).
  - `isLookup`: trimmed, no internal whitespace; Latin → one alpha token (len ≤ ~32); CJK → contains
    Han and rune-length ≤ 4; anything with spaces/sentence punctuation → phrase. Emits a debug line (E).
  - Thread the pair target through so a single word still targets `zh-TW` (not bare `zh`) — reuse/relax
    `smartTarget` (`smartdict.go:131`) to honor an explicit request target.
  - Stamp a `Warning`/note indicating which path served (mirrors smart-dict's transparency rule).
- `internal/engine/engine.go`: reuse `TranslateResult.Dictionary` (already rendered by
  `view.go:renderResult`/`root.go`), so no rendering changes are needed for word results.
- `cmd/build.go`: `buildEngine` — add `case res.Engine == "smartauto"` → `smartAutoFromConfig(res)`
  (requires `res.Provider != nil`; otherwise fall back to the existing chain). Add
  `smartAutoFromConfig(res)` next to `smartDictFromConfig` (`build.go:108`).
  `buildEngineSet` (`build.go:120`): when the primary is `smartauto`, keep it at index 0 and retain
  google/dictionary/smart-dict in the `^e` cycle.
- `internal/config/config.go`: accept `engine = "smartauto"`; document it. (Leave built-in `Default()`
  engine as `auto`; `init` writes `smartauto` — existing configs are only changed on re-`init`.)
- `cmd/init.go`: add engine option **"Smart auto — dictionary for words, LLM for phrases,
  bidirectional (recommended)"** → writes `engine='smartauto'`; and **validate** `pair_with != target`
  when pair is enabled (see D's degenerate-default guard).

### D. Pair-aware prompt + symmetric router + anti-echo (point 4)
- `internal/engine/engine.go` `Request`: add `Pair bool`, `PairHome string`, `PairAway string`
  (the two pair languages; `Target` remains the routed best-guess).
- `internal/engine/prompt.go`:
  - New pair-aware system prompt used by `buildTranslatePrompt` when `req.Pair` and both langs set:
    *"You translate between {Home} and {Away}. Detect which of these two the text is in and translate it
    into the OTHER. Always translate — never return the text unchanged, even for a single word, name, or
    loanword. Output only the translation."*
  - Harden the concise prompt (`prompt.go:30-31`): replace the blanket "return unchanged if
    source == target" with a rule that still forbids echoing a translatable word.
- Thread `Pair/PairHome/PairAway` into the `Request` from `cmd/root.go:oneShot` (`root.go:331`) and
  `internal/tui/update.go:launch` (`update.go:348`).
- `internal/lang/detect.go`: rewrite `PairTarget` to be **script-symmetric** — when exactly one pair
  member is CJK, route purely by "does the text contain CJK" (fixes the reversed `home=en` case and
  short Latin input); same-script pairs fall back to best-effort `inLang`. Add `isCJKLang(code)` +
  generalize `IsChinese`→`containsCJK` (Han + Hiragana/Katakana/Hangul). This drives the **display target
  + non-LLM (google/dict) engines**; the LLM now self-routes via the prompt.
- `internal/tui/update.go:194-196` (`^g` toggle): when enabling pair with an empty/target-colliding
  `pairWith`, pick a **distinct** away language (target≈en → `zh-TW`, else `en`).
- CLI + TUI: warn (via debug/stderr) when `pair` is on but `pair_with == target` (degenerate no-op).

### E. Debug mode (point 5)
- **New** `internal/debug/debug.go`: process-global gate + `Logf(format, args…)` writing to a
  destination `io.Writer`. `Enable(w)` sets it; no-op when disabled.
- Enable via: `--debug` persistent flag (`cmd/root.go`), `TRANSLATE_DEBUG=1` env, or config
  `[general].debug` (+ `[cli]`/`[tui]` overlay in `internal/config`).
- Destination: **stderr** for the one-shot CLI; a **file** `~/.local/state/translate/debug.log`
  (`xdgpath.StateDir()`) for the TUI, since the alt-screen hides stderr.
- Emit trace lines at the decision points: config resolve + `applyLastPair` overrides
  (`root.go:126,190`); `PairTarget` (home/away/text→target + reason: script vs detect); `buildEngine`
  / `Chain` attempts + fallback reason (`internal/engine/chain.go`); smart-dict dict hit/miss/fuzzy
  (`smartdict.go:86`); smart-auto word/phrase classification (C); the LLM request (source/target/preset,
  full prompt only at higher verbosity); and the final serving engine/model.
- Stretch (optional): a TUI `^d` overlay showing the last request's decision trace inline.

---

## Files to touch (primary)
- TUI: `internal/tui/keys.go`, `internal/tui/update.go`, `internal/tui/view.go`,
  `internal/tui/model.go`, `internal/tui/styles.go`
- Engine: **new** `internal/engine/smartauto.go`, `internal/engine/prompt.go`,
  `internal/engine/engine.go`, `internal/engine/smartdict.go`, `internal/engine/chain.go`
- Routing/lang: `internal/lang/detect.go`
- CLI/config: `cmd/build.go`, `cmd/init.go`, `cmd/root.go`, `internal/config/config.go`
  (+ `resolve.go` if `debug` joins the overlay)
- Debug: **new** `internal/debug/debug.go`
- Reuse: `SmartDictEngine` (`smartdict.go`), `dictFromConfig`/`llmFromProvider` (`build.go`),
  `renderResult` dict path (`view.go:162`), `xdgpath.StateDir()` (`internal/xdgpath/paths.go:37`).

## Verification
1. `go build ./...` and `go test ./...` (add tests below).
2. **Pair / echo (point 4):** `TRANSLATE_DEBUG=1 translate "test"` → Chinese (`測試`), **not** "test";
   `translate "測試"` → English; a full sentence → LLM translate. Inspect the stderr trace.
3. **Smart-auto (point 3):** single word (`escalations`) → dictionary entry (ECDICT) with LLM fallback
   on a miss; multi-word phrase → LLM translate; both bidirectional.
4. **TUI:** run `translate`; type `test` → Chinese; `Tab` to focus the output box (accent border),
   scroll with `j/k`/`PgDn`; type a printable key → focus snaps back to input; `^u` clears input+result;
   footer shows `^u clear` and `⇥ focus`. Confirm `~/.local/state/translate/debug.log` fills when debug on.
5. **Unit tests:** `PairTarget` symmetric cases (zh-TW⇄en and en⇄zh-TW, short Latin, CJK);
   smart-auto `isLookup` classification (word vs phrase, CJK vs Latin); pair prompt contains both
   languages and the no-echo instruction (extend `internal/engine/*_test.go`, `internal/lang`).
