# Translate

Translate, define, and speak text from Raycast — backed by the local
[`translate`](https://github.com/daviddwlee84/translate) CLI. Multi-engine
auto-fallback (local LLM via copilot-proxy / Ollama, keyless Google, offline
CC-CEDICT / ECDICT dictionaries), unified history, and free TTS.

## Requirements

This extension **shells out to the `translate` CLI** — it does not translate on its
own. Install the binary first:

```sh
# macOS / Linux
brew install daviddwlee84/tap/translate

# any platform, with a Go toolchain (the only route on Windows today)
go install github.com/daviddwlee84/translate@latest
```

On first run, `translate init` sets up your providers/config. The extension
auto-detects the binary; if it lives elsewhere, set **“translate binary path”** in
the extension preferences. (If it isn’t found, the extension shows an
install-instructions screen instead of failing.)

| | Config file | Binary probed in |
|---|---|---|
| macOS | `~/.config/translate/config.toml` | `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `~/go/bin` |
| Windows | `%USERPROFILE%\.config\translate\config.toml` | `~\.local\bin`, `~\go\bin`, `%LOCALAPPDATA%\Programs\translate`, Scoop shims, Chocolatey `bin`, `%ProgramFiles%\translate` |

> **Windows support is declared but not yet verified on Windows hardware.** The
> code paths exist and are gated (no macOS-only APIs, per-platform shortcuts, a
> `.exe`-aware binary probe), but it has been developed and tested on macOS. If
> something is wrong there, please open an issue — and note Homebrew is
> macOS/Linux-only, so `go install` is currently the only Windows install route.

> **Note:** Raycast runs under a restricted `PATH` and reads no shell env on
> either platform, so API keys must live in `translate`’s config file (via
> `translate init`), not in `~/.zshrc`. “Translate Selection” and selection-prefill
> need macOS **Accessibility** permission for Raycast (System Settings → Privacy &
> Security → Accessibility); on Windows they fall back to the clipboard.

## Ask Translate (Raycast AI)

Type `@translate` in Quick AI or AI Chat. Two tools are exposed:

- **translate-text** — translates through *your* configured engine, presets and
  fallback chain, so the answer matches what the CLI would give you.
- **look-up-word** — the offline dictionary, whose `phonetic` field is real data
  rather than something the model made up.

The extension's `ai.yaml` instructs the model to answer bilingually and gloss
unfamiliar words (part of speech, KK phonetic, meaning). That makes it useful for
the context-sensitive questions a plain translation gets wrong — asking what
`shim` means *in rate limiting* returns the software sense, where a bare lookup
returns a carpentry wedge.

AI access is a Raycast setting, not necessarily a paid one: the free message
allowance, a local Ollama model, and Custom Providers (`providers.yaml`, which
supports GitHub Copilot) all work.

## Commands

- **Translate** — type to translate; pick the target language; ⌘↵ for a streaming
  view; actions for Copy / Paste / Speak and an engine override.
- **Translate Text** — a multi-line box for whole paragraphs. Raycast's search bar
  refuses a long paste ("the text you are trying to paste is too long"), and that
  limit is the app's, not ours — so long text gets its own form. ⌘⇧S (Ctrl+Shift+S
  on Windows) loads the
  selection, ⌘⇧V the clipboard, ⌘↵ streams, ⌘⇧A reveals engine + model.
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

Every target dropdown leads with **Auto**, which follows the bidirectional pair
configured in `translate` (`pair` + `pair_with`) — the equivalent of `^g` in the
TUI. Picking a specific language instead always translates *into* it.

## Preferences

Binary path, default target language, engine, model tier, live-translate debounce,
and how Translate prefills its input (selection / clipboard / nothing). Leave
**target / tier / debounce** empty to inherit them from the `translate` config
(`~/.config/translate/config.toml`, read via `config show --json`) — configure them
once and Raycast honors them; set a preference to override the config for Raycast.

> Raycast applies a preference's default **only when no value is stored yet**. If a
> command still behaves per an older default, change the setting explicitly in
> Raycast → Extensions → Translate.
