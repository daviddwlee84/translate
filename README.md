# translate

A fast CLI/TUI translation tool for the terminal — one-shot from the shell or an
interactive live-translating TUI. Backed by an auto fallback chain over your
local LLM providers (copilot-proxy → Ollama), a free web API (Google), and a
dictionary, with typo-tolerant language resolution and translation history.

## Install

```sh
brew install daviddwlee84/tap/translate               # macOS/Linux via Homebrew (builds from source)
go install github.com/daviddwlee84/translate@latest   # or with a Go toolchain, into $GOBIN (~/.local/bin)
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
| `translate define <word>` | Dictionary lookup (bilingual: zh↔en local, or English API); records history |
| `translate <text> --learn` | Tutor mode: translate + gloss, or grammar-correct + explain |
| `translate <question> --learn-mode explain` | Answer what a term means *in the context asked about* |
| `translate dict search <prefix>` | Ranked headword candidates with definition previews (`--limit`, `--json`) |
| `translate history` / `history search <q>` | Recent history / fuzzy search (`--tsv`, `--json`) |
| `translate init` | Interactive config wizard (probes providers) |
| `translate models` | Models declared by the configured providers, per tier (`--json`) |
| `translate config path\|show` · `lang resolve <q>` · `lang list` | Introspection helpers (all `--json`) |

Flags: `--engine smartauto|auto|<provider>|google`, `--provider`, `--model`, `--tier default|fast|max`, `--preset concise|contextual|dictionary`, `--instructions`, `--pair`/`--no-pair`/`--pair-with`, `--learn`/`--learn-mode auto|teach|correct|explain`, `--bilingual`/`-2`, `--no-history`, `--debug`.
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
  Default tier is **fast** (prefers `claude-haiku-4-5`); `--tier default` prefers
  `claude-sonnet-5`, and `--tier max` prefers `claude-opus-4-8`. Before each
  Copilot request, `translate` checks the live catalog: if that preference is no
  longer served, it chooses a live replacement while preserving the tier:
  `fast` prefers Luna/mini/Flash, `default` prefers Terra/balanced flagships, and
  `max` prefers Sol/highest-quality models. The ranking follows the same model
  families as `copilot-model --auto`. Claude models route through `/v1/messages`,
  Responses-only GPT models through `/v1/responses`, and chat-capable alternatives
  through `/chat/completions`.
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

**Pair overrides `--to`.** With `pair = true`, home-language input is rerouted to
`pair_with` no matter what `--to` says — that is the point of the mode, but it
means `--to ja` does *not* guarantee Japanese output. Use `--no-pair` for a single
one-directional run:

```sh
translate "早安" --to ja                # → en   (pair rerouted it)
translate "早安" --to ja --no-pair      # → ja   (translate INTO ja, as asked)
```

Precedence is `--no-pair` > `--pair` > `TRANSLATE_PAIR` > `[general] pair`. Learn
mode is inherently bidirectional and still implies pair. The TUI toggles the same
thing with `^g`; the Raycast target dropdowns expose it as an **Auto** entry.

### Learn mode (`--learn`, `--learn-mode`)

Tutor output instead of a bare translation. It is bidirectional (implies pair,
with your home language as *native* and `pair_with` as *foreign*), always uses an
LLM provider, and returns a structured payload rather than a stream. `^n` toggles
it in the TUI; `[general] learn = true` (or `[cli]`/`[tui]`) makes it the default.

Three directions. The first two are picked automatically by script, the way pair
routing is; the third is opt-in because it cannot be detected — a bare term is
foreign-language input whether you want it corrected or explained.

| `--learn-mode` | When | What you get |
|---|---|---|
| `auto` (default) | — | `teach` for native input, `correct` for foreign |
| `teach` | you wrote your own language | translation + `vocab` (part of speech, KK/IPA, meaning) + examples |
| `correct` | you wrote the language you're learning | corrected sentence + per-mistake `issues` + a native gloss |
| `explain` | you asked a *question* about a term | the sense that fits the context, plus glosses and examples |

`explain` is the one a plain translation gets wrong, because meaning is
context-dependent and a dictionary is not:

```console
$ translate "shim" --to zh-TW
n. 填片, 万能开锁片                        # the carpentry sense — correct, and useless here

$ translate '"shim" 在 rate limit 中是什麼意思' --learn-mode explain --to zh-TW
◆ 軟體工程中的「墊片/中介層」用法
在 rate limiting 的情境下，「shim」指的是一層薄薄的中介程式碼…
  • shim (n.) /ʃɪm/ — 薄的相容層或轉接程式碼，用來攔截並修改行為
  • throttle (v./n.) /ˈθrɒtl/ — 節流，限制請求速率的動作
  ✎ We added a shim in front of the API client to enforce rate limiting.
    ↳ 我們在 API 客戶端前面加了一層 shim 來強制限流。
```

`--instructions` supplies the same context when you would rather not write it
into the question. `--json` returns the full `learn` object (`direction`, `sense`,
`answer`, `vocab[]`, `examples[]`, `issues[]`, `notes`), which is what the HTTP
API, the MCP `explain` tool, and the Raycast extension consume.

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

Two strategies, via `--bilingual-mode`:

- **`doc`** (default) — one **context-aware** call: the whole document is sent as
  numbered segments (prose to translate, commands as context), so the model knows a
  bare `rg` is the command being documented (not an abbreviation), stays in one
  target variant, and doesn't leak reasoning. Fewer tokens than per-block, and needs
  an LLM provider.
- **`blocks`** — the older per-block strategy (each block translated in isolation,
  concurrent, works with any engine). `doc` automatically falls back to this when no
  LLM is available or the structured reply can't be parsed.

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
translate dict reindex         # rebuild the CC-CEDICT search index from an existing download (no network)
```

Until then, English lookups fall back to dictionaryapi.dev (`[dict] api_fallback`),
and Chinese lookups prompt you to run the update. Misses show a ranked "did you
mean" list. Set `[dict] source = "api"` to use only dictionaryapi.dev.

`dict update cedict` also builds `cedict.db`, a SQLite index beside the plain
`cedict_ts.u8`. Without it every process re-parses the whole 9.8 MB file (~1.7 s per
Chinese lookup); with it a lookup is ~30 ms. Installs that predate the index get it
from `translate dict reindex` — no re-download.

### Headword search (`translate dict search`)

Ranked candidates for a partially-typed word, each with a one-line definition
preview — the data behind a type-ahead picker (the Raycast **Look up Word**
command uses it):

```sh
translate dict search test              # test, testing, testimony, testify, tester, …
translate dict search recieve --json    # typo → fuzzy candidates with an edit distance
translate dict search 貓 --limit 20     # CC-CEDICT prefix matches
```

Ranking is exact match → prefix, ordered by ECDICT frequency (`frq` is a **rank**:
1 is the most common word, 0 means unranked) → edit-distance candidates, themselves
re-ranked by frequency. It reads local data only — no network, no LLM — so it is
cheap enough to run on every keystroke, and it never touches history. Finding
nothing is not an error: you get an empty `candidates` array and exit status 0.

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
translate define <word> --no-history  # look up without recording it
```

A successful lookup is written to history (suppress with `--no-history`). A hit
reports the language its glosses are *actually* in — `zh-CN` for ECDICT, `en` for
CC-CEDICT — since the offline tiers are script-fixed; `--to` steers the LLM
fallback's definition language. Suggestion-only misses are never recorded.

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

## HTTP API server (`translate serve`)

Run a persistent **loopback** HTTP service so other clients (Raycast, Alfred,
editors, `curl`, a browser) reuse one warm engine instead of spawning the binary
per call:

```sh
translate serve                       # http://127.0.0.1:4155  (docs at /docs)
translate serve --port 8080 --token "$TOK"
```

Endpoints (JSON in, JSON out — same shape the CLI emits with `--json`):

```sh
curl -s localhost:4155/v1/translate -d '{"text":"hello","target":"zh-TW"}'
curl -s localhost:4155/v1/define    -d '{"word":"ephemeral"}'
curl -s 'localhost:4155/v1/dict/search?q=test&limit=10'
curl -s 'localhost:4155/v1/history?q=hello&limit=10'
curl -N 'localhost:4155/v1/translate/stream?text=hello&target=zh-TW'   # SSE
```

- **Docs**: interactive Swagger UI at `/docs`, spec at `/openapi.json` (both embedded, offline).
- **Security**: binds `127.0.0.1` only — a non-loopback `--bind` is refused unless a
  token is set. `/v1/history` requires `Authorization: Bearer <token>` when a token
  is configured (`[server].token`, or `token_env` to keep it out of `config.toml`).
  `/v1/dict/search` is *not* token-guarded: dictionary headwords are public
  reference data, unlike your own history.
- **Streaming caveat**: SSE token cadence depends on the provider — copilot-proxy
  buffers Claude `/v1/messages`, so Claude models still arrive in a burst.

```toml
[server]
port      = 4155
bind      = "127.0.0.1"
token_env = "TRANSLATE_API_TOKEN"   # optional; guards /v1/history
```

## MCP server (`translate mcp`)

Expose translate as tools to any MCP host (Claude Desktop/Code, Cursor, …) over
**stdio** — no port, no token, history never leaves the pipe. Register:

```json
{ "command": "translate", "args": ["mcp"] }
```

Tools: `translate` (text + optional target/source), `explain` (a question about a
term + the context it appeared in), `define` (word), `history` (query + limit).
Each returns readable text plus a typed structured result. Built on the official
Go MCP SDK.

`explain` is a separate tool rather than a flag on `translate` because an LLM host
picks tools by their description — a boolean buried in `translate`'s schema would
never be found.

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

`internal/appcore` is the transport-agnostic core: it holds the engine builders and
a `Service` (a warm engine + history store) that the one-shot CLI, the HTTP server
(`internal/server`), and the MCP server (`internal/mcpserver`) all drive — so every
front-end shares one translation path and `appcore` never imports `tui` or `cmd`.

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
