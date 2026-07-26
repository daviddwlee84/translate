# Translate Changelog

## [Initial Version] - {PR_MERGE_DATE}

- **Translate** — type-to-translate with a target-language dropdown, debounced live
  translation, an engine-override submenu, an opt-in streaming view (⌘↵), and
  selection/clipboard prefill.
- **Translate Selection** — grabs the current selection (or clipboard) and opens
  Translate prefilled, so you can review/edit before copying.
- **Translate Text** — a multi-line form for whole paragraphs, since Raycast's
  search bar refuses a long paste. Load the selection or clipboard explicitly,
  translate to a full page, or stream with ⌘↵.
- **Look up Word** — a Define Word-style dictionary picker: recent lookups when the
  search bar is empty, ranked headword candidates with definition previews as you
  type, and a full definition page on ↵ (which is what records the lookup). The
  definition language is pickable as a command argument or from an in-view dropdown.
  Typing reads only local dictionary data; the LLM fallback runs when you open a word.
- **Define** — dictionary lookup (offline CC-CEDICT / ECDICT) with an LLM fallback
  and "did you mean" suggestions.
- **History** — browse and search past translations.
- Backed by the local `translate` CLI: multi-engine auto-fallback (local LLM via
  copilot-proxy/Ollama, keyless Google, offline dictionaries) and free TTS.
