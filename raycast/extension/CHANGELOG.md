# Translate Changelog

## [Initial Version] - {PR_MERGE_DATE}

- **Translate** — type-to-translate with a target-language dropdown, debounced live
  translation, an engine-override submenu, an opt-in streaming view (⌘↵), and
  selection/clipboard prefill.
- **Translate Selection** — grabs the current selection (or clipboard) and opens
  Translate prefilled, so you can review/edit before copying.
- **Translate Text** — a multi-line form for whole paragraphs, since Raycast's
  search bar refuses a long paste. Load the selection or clipboard explicitly,
  translate to a full page, or stream with ⌘↵. ⌘⇧A reveals engine and model.
- **Look up Word** — a Define Word-style dictionary picker: recent lookups when the
  search bar is empty, ranked headword candidates with definition previews as you
  type, and a full definition page on ↵ (which is what records the lookup). The
  definition language is pickable as a command argument or from an in-view dropdown.
  Typing reads only local dictionary data; the LLM fallback runs when you open a word.
- **Define** — dictionary lookup (offline CC-CEDICT / ECDICT) with an LLM fallback
  and "did you mean" suggestions.
- **History** — browse and search past translations.
- **Ask Translate (Raycast AI)** — `@translate` in Quick AI or AI Chat, with two
  tools: `translate-text` (routes through your own configured engine, presets and
  fallback chain) and `look-up-word` (the offline dictionary, whose phonetics are
  real data rather than model guesses). The bundled instructions answer
  bilingually and gloss unfamiliar words, so a context-sensitive question — what
  `shim` means *in rate limiting* — gets the software sense instead of a
  carpentry wedge.
- **Windows support** — `platforms: ["macOS", "Windows"]`. Binary resolution,
  install hints and config paths sit behind a platform adapter, and every
  keyboard shortcut is declared per platform (a hardcoded `cmd` is dropped in
  silence on Windows). Homebrew is macOS/Linux-only, so Windows installs the CLI
  with `go install`.
- **Tabular output renders as a table.** A table translated for a terminal loses
  its alignment twice over — CJK glyphs are double-width, and Raycast's markdown
  is proportional — so the CLI is now asked for markdown table *structure* rather
  than space padding, and the extension repairs anything that arrives without it.
  An action toggles back to the raw model output.
- Target dropdowns lead with **Auto**, following the CLI's configured
  bidirectional pair (like `^g` in the TUI); picking a language always translates
  into it.
- Backed by the local `translate` CLI: multi-engine auto-fallback (local LLM via
  copilot-proxy/Ollama, keyless Google, offline dictionaries) and free TTS.
