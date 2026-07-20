# translate

A fast CLI/TUI translation tool for the terminal — one-shot from the shell or an
interactive live-translating TUI. Backed by an auto fallback chain over your
local LLM providers (copilot-proxy → Ollama), a free web API (Google), and a
dictionary, with typo-tolerant language resolution and translation history.

## Install

```sh
go install github.com/daviddwlee84/translate@latest   # into $GOBIN (or ~/go/bin)
```

## Build & run

```sh
go build -o translate .      # Go 1.25+ (developed on 1.26)
./translate "hola mundo" --to en          # one-shot
echo "bonjour" | ./translate --to en      # pipe
./translate                               # interactive TUI
```

First run writes a default config to `~/.config/translate/config.toml`; run
`./translate init` for a guided setup that probes which providers are up.

## Usage

| Command | What it does |
|---|---|
| `translate <text>` / `… \| translate` | One-shot translation (streams to a TTY; plain text when piped) |
| `translate` (no args, TTY) | Interactive TUI |
| `translate --to <lang> --from <lang>` | Language override; both are fuzzy (`chinees` → `zh`) |
| `translate --json` | Emit the full structured result |
| `… \| translate --bilingual` (`-2`) | Bilingual pipe view: keep the original (with color) + translation beneath (stdin only) |
| `translate define <word>` | Dictionary lookup (bilingual: zh↔en local, or English API) |
| `translate history` / `history search <q>` | Recent history / fuzzy search (`--tsv`, `--json`) |
| `translate init` | Interactive config wizard (probes providers) |
| `translate config path\|show` · `lang resolve <q>` | Introspection helpers |

Flags: `--engine smartauto|auto|<provider>|google`, `--provider`, `--model`, `--tier default|fast|max`, `--preset concise|contextual|dictionary`, `--instructions`, `--pair`/`--pair-with`, `--bilingual`/`-2`, `--no-history`, `--debug`.
Env overrides: `TRANSLATE_TARGET`, `TRANSLATE_SOURCE`, `TRANSLATE_ENGINE`, `TRANSLATE_PROVIDER`, `TRANSLATE_MODEL`, `TRANSLATE_CONFIG`, `TRANSLATE_DEBUG`.
Precedence: **flag > env > `[cli]`/`[tui]` > `[general]` > default**.

### Per-front-end defaults (`[cli]` / `[tui]`)

The CLI (arguments or piped stdin) and the interactive TUI share `[general]`, but
either can override any subset of it via an optional `[cli]` or `[tui]` table — e.g.
a snappy concise CLI and a richer, higher-tier TUI:

```toml
[general]
preset = "contextual"
tier   = "fast"

[cli]                 # one-shot / piped translation
preset = "concise"

[tui]                 # interactive front-end
tier          = "max"
live_translate = true
```

Only keys you list are overridden; the rest inherit `[general]`. Flags and env vars
still win over both.

## TUI keys

`↵` translate · live-debounce auto-translates ~700ms after you stop typing (off by default) ·
`^y` copy · `^l` toggle live · `^e` cycle engine · `^t` target language · `^p` model ·
`^o` style · `^g` toggle pair mode · `^u` clear · `⇥` switch input/output focus · `^r` history · `⌥↵` newline · `^c`/`esc` quit.

`⇥` (Tab) moves keyboard focus between the input box and the result box; the focused
box gets an accent border. With the result focused, `↑/↓ j/k PgUp/PgDn g/G space` scroll
it; typing any character snaps focus back to the input. A mouse click focuses the box
it lands in.

## Engines & fallback

The `auto` engine tries `chain.order` in turn, failing over **before the first
token** (so a down provider switches cleanly; a mid-stream error surfaces rather
than restarting):

- **copilot-proxy** — OpenAI-compatible at `http://localhost:4141/v1`, no API key.
  Default tier is **fast** (`claude-haiku-4-5`); `--tier default` → `claude-sonnet-5`,
  `--tier max` → `claude-opus-4-8`. Claude models are served only via the proxy's
  Anthropic Messages endpoint (`/v1/messages`) — the engine routes `claude-*` ids
  there automatically (they 400 on `/chat/completions`).
- **Ollama** — `http://localhost:11434/v1` (offline; `llama3.2:3b`).
- **Google** — free, keyless; also reports the detected source language.
- **openrouter** — configured but off the default chain; needs `OPENROUTER_API_KEY`.

> ⚠️ Using a GitHub Copilot subscription to back non-GitHub tools (copilot-proxy)
> may violate Copilot's Terms of Service. Reorder `chain.order` (drop `copilot`,
> lead with `ollama`/`google`) in the config if that matters to you.

Config lives at `~/.config/translate/config.toml`, history at
`~/.local/share/translate/history.jsonl`, last-pair state at
`~/.local/state/translate/state.json` (XDG-honored on macOS and Linux).

### Smart auto (`smartauto`)

The recommended default (choose it in `translate init`). It routes by input shape:
a **single word/term** is looked up in the dictionary (with the same LLM fallback as
`smart-dict` on a miss), while a **phrase or sentence** is translated by the LLM. Set
`engine = "smartauto"` in `[general]` (or `--engine smartauto`). It composes with
pair mode, so a single word and a full sentence both translate in the right direction.

### Pair mode (bidirectional)

With `pair = true` + `pair_with`, the tool detects which of the two languages your
input is in and translates it into the **other** — one box, both directions
(`你好 → hi`, `test → 測試`). For a CJK/non-CJK pair (e.g. `zh-TW`⇄`en`) direction is
decided by script, so even short input routes correctly; the LLM translate prompt is
also told to detect-and-route and to **never echo** the input. `pair_with` must differ
from the target (`translate init` enforces this; a degenerate config warns).

### Bilingual mode (`--bilingual` / `-2`)

Pipe colored docs and read both languages at once — inspired by Immersive
Translate. `cmd | translate --to <lang> --bilingual` keeps each original block
verbatim (its ANSI/color intact) and prints the translation beneath each **prose**
block, dimmed on a TTY:

```sh
tldr rg | translate --to zh-TW --bilingual     # or -2
```

Indented command/example blocks are echoed **untranslated** (so `rg pattern` stays
runnable), and blank-line spacing is preserved. It is opt-in and **stdin-only** —
never the pipe default, so plain `cmd | translate | less`/`grep` stays clean
single-output. Best for prose docs (tldr, man, `--help`); tabular output
(`ls -l`, `git status`) and hard-wrapped prose don't interleave cleanly.
`--json`/`--learn` take precedence. Design notes + why per-word recoloring was
rejected: [`backlog/bilingual-immersive-mode.md`](backlog/bilingual-immersive-mode.md).

Separately, ANSI escapes are now **stripped from piped input** before translating,
so colored input (e.g. `grep --color=always`) no longer pollutes the prompt.

### Debugging

`--debug` (or `TRANSLATE_DEBUG=1`, or `[general] debug = true`) logs the intermediate
decisions — resolved settings, pair routing, word-vs-phrase classification, dictionary
hit/miss, and chain fallback. The one-shot CLI logs to **stderr**; the TUI logs to
`~/.local/state/translate/debug.log` (its alt-screen hides stderr).

## Dictionary mode

`^e` cycles to the dictionary engine (or use `translate define <word>`). It is an
**offline bilingual** dictionary that routes by script — Chinese → CC-CEDICT
(zh→en), English → ECDICT (en→zh):

```sh
translate dict update all      # one-time ~67 MB download/build into ~/.local/share/translate/dict
```

Until then, English lookups fall back to dictionaryapi.dev (`[dict] api_fallback`),
and Chinese lookups prompt you to run the update. Misses show a ranked "did you
mean" list. Set `[dict] source = "api"` to use only dictionaryapi.dev.

### Smart dictionary (`smart-dict`)

A distinct engine that keeps the offline dictionary fast and deterministic, but on
a miss — no entry, or a fuzzy match too far off — falls back to the LLM for a gloss
plus example sentences (never silently: the result carries a `⚠ … defined via <provider> (LLM)`
warning). Close typos (edit distance ≤ `[smartdict] close_distance`, default 1) still
show "did you mean" without calling the LLM.

```sh
translate define serendipity        # exact ECDICT entry
translate define zzzznotaword       # miss → LLM definition (⚠ warning on stderr)
translate define helllo             # distance-1 typo → "did you mean: hello, …"
translate define --plain <word>     # force the offline dictionary, no LLM fallback
```

`translate define` uses smart-dict whenever an LLM provider is reachable
(`[smartdict] define_default`, `--smart`/`--plain` to force); in the TUI, `^e`
cycles to a `smart-dict` engine alongside the plain `dictionary`.

```toml
[smartdict]
close_distance = 1            # en edit-distance ≤ this stays "did you mean"; beyond → LLM
preset         = "dictionary" # LLM output style for the fallback
define_default = true         # `translate define` prefers smart-dict when a provider is up
```

Data: **CC-CEDICT** (CC BY-SA 4.0, © Paul Andrew Denisowski / MDBG) and
**ECDICT** (MIT, © skywind3000).

## Developer note — Charm v2 (`charm.land/*`)

This project pins the **v2** Charm stack on the `charm.land/*` vanity module paths
(`charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, `huh/v2`) — **not**
`github.com/charmbracelet/*`. v1 code will not compile here. When editing the TUI:
`View()` returns `tea.View` (wrap strings with `tea.NewView`); keys arrive as
`tea.KeyPressMsg`; alt-screen is the declarative `View.AltScreen` field; window
size is requested via `tea.RequestWindowSize`.

## Architecture

The `internal/engine` and `internal/store` layers are pure Go (no TUI imports);
the one-shot CLI and the Bubble Tea TUI drive the identical engine values and
diverge only at presentation. A single monotonic `seq` counter in the TUI drives
debounce-collapse, in-flight cancellation, and stale-result dropping.

<!-- project-knowledge-harness:readme-roadmap -->
<!-- Snippet for project's README.md, placed near other meta sections like
     "Customization" or "Contributing". -->

## Roadmap & lessons learned

Forward-looking work — long-term ideas, deferred items, things needing
evaluation — lives in [`TODO.md`](TODO.md), prioritised P1 → P3 with effort
estimates (S/M/L/XL). Items with accompanying research, design notes, or paused
troubleshooting link to a corresponding [`backlog/<slug>.md`](backlog/) doc.

Backward-looking knowledge — past traps and non-obvious debugging — lives in
[`pitfalls/`](pitfalls/), titled by symptom so future-you can grep the error
message and land on the root cause + workaround instead of re-debugging from
scratch.
<!-- project-knowledge-harness:readme-roadmap --> (end)
