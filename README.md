# translate

A fast CLI/TUI translation tool for the terminal — one-shot from the shell or an
interactive live-translating TUI. Backed by an auto fallback chain over your
local LLM providers (copilot-proxy → Ollama), a free web API (Google), and a
dictionary, with typo-tolerant language resolution and translation history.

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
| `translate define <word>` | Dictionary lookup (exact → fuzzy on typo) |
| `translate history` / `history search <q>` | Recent history / fuzzy search (`--tsv`, `--json`) |
| `translate init` | Interactive config wizard (probes providers) |
| `translate config path\|show` · `lang resolve <q>` | Introspection helpers |

Flags: `--engine auto|<provider>|google`, `--provider`, `--model`, `--tier default|fast|max`, `--no-history`.
Env overrides: `TRANSLATE_TARGET`, `TRANSLATE_SOURCE`, `TRANSLATE_ENGINE`, `TRANSLATE_PROVIDER`, `TRANSLATE_MODEL`, `TRANSLATE_CONFIG`.
Precedence: **flag > env > config > default**.

## TUI keys

`↵` translate · live-debounce auto-translates ~400ms after you stop typing ·
`^l` toggle live · `^r` history (↵ recall) · `^u` clear · `⌥↵` newline · `^c`/`esc` quit.

## Engines & fallback

The `auto` engine tries `chain.order` in turn, failing over **before the first
token** (so a down provider switches cleanly; a mid-stream error surfaces rather
than restarting):

- **copilot-proxy** — OpenAI-compatible at `http://localhost:4141/v1`, no API key.
  Default tier is **fast** (`gemini-3.5-flash`) for snappy short translations;
  `--tier default` → `claude-sonnet-5`, `--tier max` → `gpt-5.4`. (The proxy's
  `/v1/models` lists more ids than it will serve — the config ships verified-working ones.)
- **Ollama** — `http://localhost:11434/v1` (offline; `llama3.2:3b`).
- **Google** — free, keyless; also reports the detected source language.
- **openrouter** — configured but off the default chain; needs `OPENROUTER_API_KEY`.

> ⚠️ Using a GitHub Copilot subscription to back non-GitHub tools (copilot-proxy)
> may violate Copilot's Terms of Service. Reorder `chain.order` (drop `copilot`,
> lead with `ollama`/`google`) in the config if that matters to you.

Config lives at `~/.config/translate/config.toml`, history at
`~/.local/share/translate/history.jsonl`, last-pair state at
`~/.local/state/translate/state.json` (XDG-honored on macOS and Linux).

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
