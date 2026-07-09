# translate — a fast CLI/TUI translation tool (Go)

## Context

There is no quick terminal translator on this machine (`trans`/translate-shell is absent, no
Python translate libs), yet the box has rich LLM access (a live **copilot-proxy** at
`localhost:4141` exposing Claude/GPT/Gemini, plus Ollama and OpenRouter) and a strong Charm
tooling culture (`gum`, `glow`, `vhs`, `television`). The goal is a **fast, smooth in-terminal
translation experience** that works two ways — a one-shot CLI (`translate "text"` / pipe) and an
interactive TUI (`translate`) — with typo tolerance, XDG config with a guided `init`, and a
pluggable backend covering free APIs, dictionaries, and LLMs. Grammar-correction, vocabulary
storage, and spaced review are explicitly **out of scope** for this iteration (future direction).

This is being built greenfield in the `tries/` scratch dir as a standalone Go module named
`translate`, structured so it can later graduate into the chezmoi dotfiles (`~/.dotfiles/bin/` +
a `tv translate` channel) if it proves useful.

### Locked decisions (from user)
- **Stack:** Go + **cobra** (CLI) + **Bubble Tea** (TUI) + **Lip Gloss** + **Bubbles** + **huh** (init wizard). Single static binary.
- **Default engine:** AUTO fallback chain — copilot-proxy → Ollama → Google free API (configurable).
- **v1 engines (all four):** LLM backend, free translate API (Google), dictionary lookup (exact→fuzzy), history + memory.
- **Interaction:** live-debounce **~400ms default-on** (auto-translate after you stop typing) **plus Enter = translate immediately**; debounce toggleable; in-flight requests cancellable so stale results never render.
- **Config:** XDG — `~/.config/translate/config.toml`, data `~/.local/share/translate/`, state `~/.local/state/translate/`; auto-create on first run; `translate init` wizard that probes providers.

---

## Critical technical facts & gotchas (verified)

1. **Charm is v2 on `charm.land/*` vanity paths** (out of beta ~2026-02-23), NOT
   `github.com/charmbracelet/*`. v1 code will not compile against v2. Go 1.26.4 here clears the
   Go 1.25 floor. **Put this loudly in `go.mod` and a `CLAUDE.md`/README note** so v1-trained
   codegen doesn't poison the build. v2 API deltas that bite: `View() tea.View` (wrap strings
   with `tea.NewView(...)`); keys arrive as `tea.KeyPressMsg` (was `KeyMsg`; `.String()` still
   matches); `tea.RequestWindowSize` (was `WindowSize`); declarative cursor via `tea.View.Cursor`.
   `Init()/Update()/tea.Batch/tea.Tick/tea.Quit` unchanged. `go get` the set and **pin whatever
   resolves** rather than hand-copying versions (bubbletea v2.0.8 confirmed on proxy.golang.org).
2. **Streaming vs structured output:** stream the **primary translation as plain text** ("output
   only the translation"). It renders token-by-token identically on copilot/Ollama/generic and is
   latency-optimal. `DetectedSource` comes free from Google's `data[2]` or the offline detector.
   The richer **TranslateResult** (alternatives/notes/confidence) is produced by an **optional,
   cancellable second non-streaming call pinned to the finished translation** — never mid-stream
   JSON parsing. This deletes `response_format=json_schema`, brace-balance scanners, and
   "show only the growing field" hacks from the hot path.
3. **XDG on macOS:** neither `github.com/adrg/xdg` nor stdlib `os.UserConfigDir()` returns
   `~/.config` on macOS (both → `~/Library/Application Support`). Roll a ~10-line resolver that
   honors `XDG_*` env then falls back to `~/.config` / `~/.local/share` / `~/.local/state` on all
   platforms.
4. **`seq` counter is the single correctness mechanism** for debounce-collapse + cancel +
   stale-drop (mirrors Charm's `examples/debounce` fused with `examples/realtime`). `context`
   cancel is a work-saving optimization layered on top, not the correctness guarantee.
5. **copilot model-id normalization:** strip any `[1m]` suffix, keep hyphenated ids
   (`claude-sonnet-5`), reject dated ids. Send **no** `Authorization` header to copilot-proxy
   (auth only when a provider's `api_key_env` is set).
6. **Google endpoint is unofficial** — decode into `[]json.RawMessage` and **guard every index**
   (never `data[8][2][0]` blindly); 429/403 is a *when*, fail over on any non-200. The 400ms
   debounce + cancel already throttles, so no client-side rate limiter needed in v1.
7. **copilot-proxy ToS caveat:** using a Copilot sub for non-GitHub agents violates Copilot ToS.
   Surface this on first run and keep `chain.order` trivially reconfigurable (drop copilot, lead
   with Ollama/Google).
8. **Bubble Tea model receivers:** use **one convention everywhere** — value receiver returning
   the modified copy (`func (m Model) Update(...) (tea.Model, tea.Cmd)`), including helpers. Mixed
   value/pointer receivers are the #1 "state didn't stick" footgun.
9. **cobra Ctrl-C:** wire `rootCmd.ExecuteContext(signal.NotifyContext(...))` so one-shot LLM
   calls actually cancel.

---

## Architecture

Module `translate` (Go 1.26). Engine + store layers are **pure Go with `context.Context` and zero
Bubble Tea imports** — the one-shot CLI and the TUI call the identical `engine`/`store` objects;
they diverge only at presentation.

```
translate/
  main.go                         # cobra Execute() with signal-cancel context
  cmd/
    root.go                       # RunE: args→once, piped stdin→once, TTY→TUI
    init.go                       # huh standalone wizard + provider probes
    history.go search.go config.go providers.go lang.go   # breadth subcommands
  internal/
    xdgpath/paths.go              # ~/.config resolver (§gotcha 3)
    config/{config.go,resolve.go} # TOML structs; Default/Load/Save; flags>file>env precedence
    state/state.go                # last language pair / source-mode (state.json)
    lang/lang.go                  # ISO-639 table + fuzzy Resolve("chinees"→zh) + whatlanggo detect
    store/{store.go,jsonl.go}     # Store iface + JSONL impl (sqlite is a later drop-in)
    engine/
      engine.go                   # Engine iface, Request, TranslateResult (Marvin-lite)
      llm.go                      # OpenAI-compat client (copilot/ollama/openrouter/litellm/generic)
      google.go                   # free translate_a/single (defensive parse)
      dict.go                     # dictionaryapi.dev exact→local Levenshtein fuzzy
      chain.go                    # AUTO fallback router (cached probes, pre-token failover)
      prompt.go models.go         # built-in prompts + model recommendation/normalization
    tui/{model,update,view,msgs,keys,styles}.go
  go.mod
```

### Engine seam (shared spine)
```go
type TranslateResult struct {              // "Marvin-lite" typed result = history record shape
    Translation, DetectedSource, Target string
    Alternatives []string; Notes string; Confidence float64
    Engine, Model string; Fuzzy bool; FuzzyMatched string
    Dictionary *DictEntry                  // set only in dict mode
}
type Request struct { Text, Source, Target string; Mode Mode; MaxAlts int; Stream bool }

type Engine interface {
    Name() string
    Translate(ctx context.Context, req Request) (<-chan Chunk, error) // closes after 1 terminal Chunk
    Detect(ctx context.Context, text string) (string, error)
    Available(ctx context.Context) bool     // cheap health probe, cached ~5s by chain
    Supports(m Mode) bool                    // Google=translate only; dict=dict only; LLM=both
}
```
- Channel-return maps 1:1 onto the TUI's self-resubscribing reader; one-shot just drains it.
- Runtime/network errors flow as a terminal `ChunkError` (uniform failover/drain path); the
  synchronous `error` is only for setup failures.
- **Chain:** probe in configured order (filtered by `Supports(Mode)`), cached availability with
  immediate `markDown` on error; **fail over only before the first token** (once tokens have
  streamed, surface the error rather than garble the pane — the common "copilot not running" case
  fails at connect time, pre-token, so it switches cleanly to Ollama).

### TUI core — `seq`-guarded debounce + cancel + stream
Every keystroke `m.seq++`, cancels any in-flight ctx, and arms `tea.Tick(400ms → debounceMsg{seq})`.
`launch()` (the one place a translation starts) bumps seq again, opens a fresh ctx + buffered
channel, spawns the engine goroutine, and records `inflight = seq`. Every `debounceMsg`/`chunk`/
`done`/`err` carries its birth `seq`; handlers drop anything where `msg.seq != m.<current>` — so
superseded ticks and cancelled/stale tokens are unrenderable in O(1). Enter binds to `Translate`;
Alt+Enter inserts a newline. Footer shows `auto→es · claude-sonnet-5 (copilot) · live●` + help.
Keys: `enter` translate, `^l` toggle live, `^e` cycle engine, `^s` swap langs, `^t` pick target,
`^r` history recall, `^y` copy, `^c` quit.

### Config (condensed `config.toml`)
```toml
[general]
default_target = "en"; default_source = "auto"   # auto=detect, or fixed code e.g. "zh"
remember_last_pair = true; live_translate = true; debounce_ms = 400
engine = "auto"                                   # auto | llm | google | dict
tier = "default"; alternatives_count = 3; stream = true; color = "auto"
[chain]
order = ["copilot", "ollama", "google", "dict"]
[[provider]]                                       # copilot-proxy: NO auth header
name="copilot"; type="openai"; base_url="http://localhost:4141/v1"
model="claude-sonnet-5"; model_fast="claude-haiku-4-5"; model_max="claude-opus-4-8"
[[provider]]
name="ollama"; type="ollama"; base_url="http://localhost:11434"; model="llama3.2:3b"
[[provider]]
name="openrouter"; type="openrouter"; base_url="https://openrouter.ai/api/v1"
model="anthropic/claude-sonnet-5"; api_key_env="OPENROUTER_API_KEY"
[google] enabled=true; extra_dt=["bd","at"]; timeout_ms=4000
[dict]   enabled=true; endpoint="https://api.dictionaryapi.dev/api/v2/entries"; lang="en"; fuzzy=true
[history] enabled=true; backend="jsonl"           # jsonl (v1) | sqlite (upgrade)
```
Precedence per setting (mi-router idiom): **CLI flag > config.toml > env (`TRANSLATE_*`) >
interactive prompt**. Secrets are named via `provider.api_key_env` and read at request time — never
stored. `state.json` holds `{source,target,source_mode,provider,engine}`, rewritten after each
success and on TUI exit; bare `translate` restores it when `remember_last_pair`.

### Built-in prompt (translate mode) + model recommendations
System prompt: precise/faithful/register-aware translator; **interpret the user's intended meaning
despite typos/slang — don't translate the typo literally, don't refuse**; detect source when
`auto`. v1 hot path asks for **plain translation only**; the optional enrichment pass asks for
`{alternatives, notes, confidence}` JSON pinned to the finished translation.
Recommended models surfaced by `init`/`--help` (only offered if the provider probes up):
default `claude-sonnet-5` · fast `claude-haiku-4-5` (or `gpt-5.4-mini`) · max `claude-opus-4-8` ·
offline `llama3.2:3b`.

### Typo tolerance (three layers)
- Subcommands: cobra `SuggestionsMinimumDistance = 2` ("did you mean").
- Language names/codes: `lang.Resolve` (exact code → name/alias/native → `agnivade/levenshtein`
  fuzzy); `chinees→zh`, `spanich→es`, `中文→zh`; prints `(interpreted "chinees" as zh)` on stderr.
- Dictionary: exact API hit → on 404 resolve nearest headword locally via bundled wordlist +
  Levenshtein, then re-query.

---

## Build order (vertical slices — each is runnable)

Slices **1–5 deliver the entire "fast smooth translation" promise**; 6–9 add the breadth the user
selected (all four engine types + history + init). Build in order:

0. **Bootstrap & de-risk stack.** `go mod init translate`; `go get` the `charm.land/*` v2 set +
   cobra; compile a throwaway Bubble Tea "hello" to prove v2 builds on this box. → `go.mod`.
1. **Smallest E2E: one-shot → copilot-proxy → print** (non-streaming, plain text). `translate
   "text"` + stdin pipe; base_url/model/target from flags+env, no config yet. →
   `main.go`, `cmd/root.go`, `internal/engine/{engine,llm}.go`. Ship: `echo hola | translate --to en` → `hello`.
2. **Config + XDG + lang resolver.** First-run `Default()`; flags>file>env; `chinees→zh`. →
   `internal/xdgpath`, `internal/config`, `internal/lang`.
3. **Streaming one-shot.** SSE parse in `llm.go`; stream to stdout only when it's a TTY; gate ANSI
   (`translate x | pbcopy` stays clean); `--json` for scripts.
4. **TUI MVP.** Bare `translate` → textarea + viewport + footer; Enter=translate (non-streaming);
   render result. → `internal/tui/*`.
5. **Debounce + cancel + live stream (the "smooth" core).** `seq`, `armDebounce`, `launch`,
   `waitStream`, ctx cancel, streaming render, `^l` live toggle. → `internal/tui/{update,msgs,model}.go`.
6. **Fallback chain + Ollama.** `chain.go`, cached `Available()`, pre-token copilot→ollama failover.
7. **Free Google engine + offline detect.** Defensive nested-array parse, `DetectedSource` for free,
   `whatlanggo` fallback, appended to chain. → `internal/engine/google.go`, `internal/lang` detect.
8. **Dictionary engine (exact→fuzzy).** `dictionaryapi.dev` + bundled wordlist + Levenshtein
   nearest-headword; routed by `Supports(ModeDict)`. → `internal/engine/dict.go`.
9. **History (JSONL) + recall + `init` wizard.** Write on success (one-shot + TUI `doneMsg`);
   `Recent`/`Search` via `sahilm/fuzzy`; `^r` history pane; `state.json` last-pair; huh standalone
   `init` with concurrent provider probes. → `internal/store`, `internal/state`, `cmd/{history,init}.go`.
10. **Deferred (post-v1 / if wanted):** optional structured **enrichment second pass**
    (alternatives/notes/confidence); `tv translate` channel + `list --tsv`/`show --field`/favorites;
    MyMemory secondary free API; sqlite+FTS5 upgrade behind the existing `Store` iface; graduate
    into dotfiles (`executable_*` or `go install`, completions, `docs/tools/translate.md`).

---

## Dependencies to pin
```
github.com/spf13/cobra
charm.land/bubbletea/v2   v2.0.8        # vanity path — NOT github.com/charmbracelet
charm.land/bubbles/v2                    # textarea, textinput, viewport, list, spinner, help, key
charm.land/lipgloss/v2
charm.land/huh/v2                        # init wizard only (slice 9)
github.com/pelletier/go-toml/v2          # config (round-trips [[provider]])
github.com/sahilm/fuzzy                  # history + lang fuzz
github.com/agnivade/levenshtein          # lang + dict nearest-word
github.com/abadojack/whatlanggo          # offline detect (slice 7)
golang.org/x/term                        # IsTerminal
```
Deferred-only: `modernc.org/sqlite` (cgo-free, FTS5 built in) for the history upgrade;
`github.com/atotto/clipboard` or OSC52 / the repo's `x` tool for copy. Use
`time.Now().UnixNano()` + random bytes for sortable ids (no ulid dep).

## Verification (end-to-end, per slice)
- **Slice 1:** `echo "hola mundo" | ./translate --to en` prints `hello world`; `./translate "bonjour" --to en` works. Confirm copilot-proxy is up first: `curl -s localhost:4141/v1/models | head`.
- **Slice 3:** run against a long paragraph and watch tokens stream in the terminal; `./translate x | cat` emits clean text with no ANSI; `--json` emits the struct.
- **Slice 5 (the core):** in the TUI, type continuously and confirm only the final pause triggers a translation, edits cancel the prior request, and no stale result ever flashes. Toggle `^l` off → only Enter translates. Test `chinees` → resolves to `zh`.
- **Slice 6:** `copilot-proxy stop` then translate → transparently falls back to Ollama (`llama3.2:3b`); `providers probe` shows live health.
- **Slice 7/8:** offline (proxy+ollama down) → Google still translates and reports detected source; `translate define serendipity` returns definitions; a typo (`serendpity`) fuzzy-matches.
- **Slice 9:** delete `~/.config/translate/` → first run creates defaults; `translate init` probes providers, writes config; history persists across runs and `^r` recalls; `state.json` remembers the last pair.
- Throughout: `go build ./...`, `go vet ./...`, `gofmt`.

## Notes
- Honor the user's four-engine v1 scope (LLM + Google + dictionary + history); the phasing just
  front-loads the smooth core so an early stop still yields a usable tool.
- A design subagent left a stray scratch file at
  `.claude/plans/cli-tui-rosy-alpaca-agent-a4fd8d5dd7034165c.md` — safe to delete; not part of this plan.
