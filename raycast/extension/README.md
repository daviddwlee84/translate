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
- **Look up Word** — a dictionary picker: an empty search bar lists your recent
  lookups, typing shows ranked headword candidates with definition previews, and ↵
  opens a full definition page (which is also what records the lookup in history).
  Pick the definition language before opening the command or from the dropdown
  inside it. Typing only reads the local dictionary — the LLM is called when you
  open a word, not while you type.
- **Define** — single-result dictionary lookup with phonetics and meanings (offline
  dictionaries, LLM fallback, and suggestions).
- **History** — browse and search your past translations.

For the best **Look up Word** experience run `translate dict update all` once
(offline CC-CEDICT + ECDICT); existing installs should also run
`translate dict reindex`, which makes Chinese lookups ~20× faster without
re-downloading anything.

## Preferences

Binary path, default target language, engine, model tier, live-translate debounce,
and how Translate prefills its input (selection / clipboard / nothing). Leave
**target / tier / debounce** empty to inherit them from the `translate` config
(`~/.config/translate/config.toml`, read via `config show --json`) — configure them
once and Raycast honors them; set a preference to override the config for Raycast.

> Raycast applies a preference's default **only when no value is stored yet**. If a
> command still behaves per an older default, change the setting explicitly in
> Raycast → Extensions → Translate.
