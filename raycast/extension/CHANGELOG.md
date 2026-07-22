# Translate Changelog

## [Initial Version] - {PR_MERGE_DATE}

- **Translate** — type-to-translate with a target-language dropdown, debounced live
  translation, an engine-override submenu, an opt-in streaming view (⌘↵), and
  selection/clipboard prefill.
- **Translate Selection** — grabs the current selection (or clipboard) and opens
  Translate prefilled, so you can review/edit before copying.
- **Define** — dictionary lookup (offline CC-CEDICT / ECDICT) with an LLM fallback
  and "did you mean" suggestions.
- **History** — browse and search past translations.
- Backed by the local `translate` CLI: multi-engine auto-fallback (local LLM via
  copilot-proxy/Ollama, keyless Google, offline dictionaries) and free TTS.
