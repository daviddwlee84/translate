# Raycast integration for `translate`

Bring `translate` into [Raycast](https://raycast.com) — translate from root
search or a global hotkey, backed by the **existing `translate` binary** (this
directory contains no translation logic; it shells out to the CLI).

There are two independent tracks. **The TypeScript extension is the primary,
full-featured path;** the script commands are an optional zero-Node fallback.

| Track | Path | Build | Best for |
|---|---|---|---|
| **TypeScript extension** (primary) | [`extension/`](extension/) | `npm` / `ray` | full UI: live translate, selection prefill, define, history, engine switch, streaming |
| **Script Commands** (bash, optional) | [`script-commands/`](script-commands/) | none | zero-Node fallback; one-shot translate/define |

Everything requires the `translate` CLI on the machine:

```sh
just install                                  # → ~/.local/bin/translate
# or
brew install daviddwlee84/tap/translate
```

## Track A — Script Commands (optional)

The TypeScript extension (Track B) supersedes these; keep them only if you want a
zero-Node fallback that runs without `ray develop`.

```sh
just raycast-scripts   # chmod +x the scripts and print the directory to add
```

Then, **one time** in Raycast: **Settings → Extensions → Script Commands →
Add Script Directory** → select `raycast/script-commands`. (There is no CLI to
register a script directory; this step is manual.)

Commands: **Translate Text** (language dropdown + text), **Translate & Copy**
(copies the result; leave the text blank to translate the clipboard — Script
Commands can't read the selection), **Define Word** (dictionary lookup).

## Track B — TypeScript extension (local dev)

```sh
just raycast-dev       # npm install (first run) + `ray develop`
```

`ray develop` registers the extension with the running Raycast app and hot-reloads.
Four commands appear in root search and **stay installed after you stop
`ray develop`** — no store publish needed for personal use:
**Translate** (type-to-translate, language dropdown, engine-override submenu,
streaming ⌘↵, Copy/Paste/Speak), **Translate Selection** (grabs the selection or
clipboard and opens Translate prefilled, with an optional target-language argument),
**Define** (dictionary lookup + LLM fallback), and **History** (browse/search past
translations). `just raycast-build` / `just raycast-lint` type-check and lint.

Configure the binary path and defaults in the extension's **Preferences**
(the binary is auto-probed in `~/.local/bin`, `/opt/homebrew/bin`,
`/usr/local/bin`, `~/go/bin`; override with an absolute path if it lives elsewhere).
Translate-as-you-type is **debounced** (default 700 ms, tunable via the
"Live translate debounce" preference) and cancels superseded in-flight requests,
so typing a phrase doesn't fire an LLM call per keystroke. Opening **Translate**
seeds the input from the current selection (or clipboard) per the "Prefill input
from" preference — set it to "Nothing" to always start empty.

## Gotchas (both tracks)

- **Raycast runs under launchd with a restricted PATH** — it does *not* inherit
  your shell's PATH, so a bare `translate` fails with `command not found` /
  `spawn translate ENOENT`. Both tracks resolve an **absolute** path. Always test
  **from Raycast**, not a terminal (a terminal's PATH hides the bug). See
  [`../pitfalls/raycast-launchd-path-translate-not-found.md`](../pitfalls/raycast-launchd-path-translate-not-found.md).
- **API keys must live in `translate`'s config**, not shell env — a key exported
  in `~/.zshrc` is absent under launchd. Put providers/keys in
  `~/.config/translate/config.toml` (run `translate init`); `HOME` is passed
  through so the CLI finds it.
- **Exit code is 0 even when an engine fails** (it falls back). The extension
  surfaces `warnings[]` from `--json`; the plain-text scripts don't show them.

See [`../docs/raycast-extension.md`](../docs/raycast-extension.md) for how Raycast
extensions work, the full integration-tier comparison, and the competitive
landscape. Store publishing is tracked in
[`../backlog/raycast-extension.md`](../backlog/raycast-extension.md).
