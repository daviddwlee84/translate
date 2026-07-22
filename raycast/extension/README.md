# Translate

Translate, define, and speak text from Raycast — backed by the local
[`translate`](https://github.com/daviddwlee84/translate) CLI. Multi-engine
auto-fallback (local LLM via copilot-proxy / Ollama, keyless Google, offline
CC-CEDICT / ECDICT dictionaries), unified history, and free TTS.

## Requirements

This extension **shells out to the `translate` CLI** — it does not translate on its
own. Install the binary first:

```sh
brew install daviddwlee84/tap/translate
# or, with a Go toolchain:
go install github.com/daviddwlee84/translate@latest
```

On first run, `translate init` sets up your providers/config
(`~/.config/translate/config.toml`). The extension auto-detects the binary in
`~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, and `~/go/bin`; if it lives
elsewhere, set **“translate binary path”** in the extension preferences. (If it
isn’t found, the extension shows an install-instructions screen instead of failing.)

> **Note:** Raycast runs under a restricted `PATH` and reads no shell env, so API
> keys must live in `translate`’s config file (via `translate init`), not in
> `~/.zshrc`. “Translate Selection” and selection-prefill need macOS **Accessibility**
> permission for Raycast (System Settings → Privacy & Security → Accessibility).

## Commands

- **Translate** — type to translate; pick the target language; ⌘↵ for a streaming
  view; actions for Copy / Paste / Speak and an engine override.
- **Translate Selection** — grabs your selection (or clipboard) and opens Translate
  prefilled and editable.
- **Define** — dictionary lookup with phonetics and meanings (offline dictionaries,
  LLM fallback, and suggestions).
- **History** — browse and search your past translations.

## Preferences

Binary path, default target language, engine, model tier, live-translate debounce,
and how Translate prefills its input (selection / clipboard / nothing).
