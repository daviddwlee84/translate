# Raycast integration — reference

How `translate` plugs into [Raycast](https://raycast.com), the options we
weighed, how the shipped integration works, and how it compares to existing
translation extensions. This is present-tense reference reading; the actual code
lives in [`../raycast/`](../raycast/), and store-publishing is tracked in
[`../backlog/raycast-extension.md`](../backlog/raycast-extension.md).

**Core principle: reuse the binary, don't reimplement.** Every tier below shells
out to the existing `translate` CLI (`--json`) and renders the result. No
translation, engine, or dictionary logic is duplicated in TypeScript/bash.

## Integration tiers

Raycast offers four ways to surface functionality. We ship the middle two.

| Tier | Reuses binary? | Effort | Selection → translate | Persisted defaults | Rich UI / history | Streaming | Distribution |
|---|---|---|---|---|---|---|---|
| **Quick Link** | ✗ (can't exec) | 5 min | via deeplink only | ✗ | ✗ | ✗ | launcher only |
| **Script Command** (bash) — *shipped* | ✓ | ~0.5 h | ✗ (needs `pbpaste`) | ✗ (no prefs) | ✗ (plain text) | only `fullOutput` | Add Script Directory |
| **TS Extension** (`@raycast/api`) — *shipped* | ✓ | 1–2 d | ✓ `getSelectedText()` | ✓ preferences | ✓ List/Detail/Actions | ✓ (manual `spawn`) | `npm run dev` / store |
| **AI Extension** (tools) | ✓ | +0.5 d | AI-orchestrated | — | AI Chat | `AI.ask` | store; **Pro-gated** |

- **Quick Links** open a URL/file/app/`raycast://` deeplink — they have no
  shell-exec surface, so they can't run the CLI directly. Useful only as a hotkey
  that deeplinks into a real command.
- **AI Extensions** expose "tools" the Raycast AI can call. Feasible (a tool file
  can `execFile` the binary and return its `--json`), but the AI API requires
  **Raycast Pro**, so it's an additive Pro-only layer — deferred.

## How Raycast extensions work

- **Manifest** (`package.json`): a `commands[]` array; each command has a
  required `mode`:
  - `view` — default-exports a React (TSX) component (our `Translate`).
  - `no-view` — default-exports an async function; runs and exits, no UI
    (our `Translate Selection` → `getSelectedText` → `Clipboard.paste` + `showHUD`).
  - `menu-bar` — a persistent `MenuBarExtra` (not used yet).
- **Running the binary:** extensions run in a Node runtime, so `child_process`
  works. We use `execFile` (typed wrapper in
  [`../raycast/extension/src/lib/translate.ts`](../raycast/extension/src/lib/translate.ts))
  rather than the `useExec` hook, because `useExec` buffers with a 10 s default
  timeout that LLM engines exceed (and its `timeout: 0` is coerced back to 10000).
  The CLI's **`--stream` flag** forces token streaming even when stdout is piped
  (which Raycast always is; without it the CLI treats non-TTY as buffered). A
  streaming `Detail` view can spawn `translate … --stream` and append `stdout`
  chunks into React state bound to `Detail.markdown`. Caveat: visible progressive
  output depends on the provider — ollama streams; **copilot-proxy currently buffers
  its claude `/v1/messages` responses**, so the result appears after first-token
  latency. The default live view uses `--json` (buffered, structured), which returns
  fast enough.
- **Preferences** (`getPreferenceValues()`): persisted per-extension settings —
  our `binaryPath`, `defaultTarget`, `engine`, `tier`.
- **Input/UI:** `getSelectedText()` (frontmost app's selection),
  `Clipboard.copy/paste`, `Action.CopyToClipboard`/`Action.Paste`; `List` +
  `List.Dropdown` (searchBarAccessory, `storeValue` to remember the last target);
  `Detail`/`List.Item.Detail` with `.Metadata` for structured fields; `showHUD`/
  `showToast` for feedback.
- **Tooling:** the `ray` CLI (ships in the extension's dev deps). `ray develop`
  (`npm run dev`) registers the extension with the running Raycast app and
  hot-reloads; **it persists in root search after you stop dev** — no store
  publish needed for personal use. `ray build`/`ray lint` type-check and lint.

## Gotchas (learned the hard way)

- **launchd PATH:** Raycast launches under launchd and does *not* inherit your
  shell PATH, so a bare `translate` fails with `ENOENT` / `command not found`.
  Both tracks resolve an **absolute** path (preference first, then probe
  `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `~/go/bin`). Always test
  **from Raycast**, not a terminal (a terminal's PATH hides the bug). See
  [`../pitfalls/raycast-launchd-path-translate-not-found.md`](../pitfalls/raycast-launchd-path-translate-not-found.md).
- **HOME / config, not shell env:** the CLI reads providers/keys from
  `~/.config/translate/config.toml`, *not* environment variables. A key exported
  in `~/.zshrc` is absent under launchd — put it in the config (`translate init`).
  We pass `HOME` through so the CLI finds its config.
- **Exit code is 0 even on engine failure** (auto-fallback), so we read
  `warnings[]` from `--json` rather than branching on exit codes. The extension
  surfaces warnings in the Detail pane / HUD; the plain-text scripts don't.
- **Extension icons must be a real PNG** (512×512) — emoji icons work only for
  Script Commands. `ray build`/`lint` complains otherwise.

## Competitive landscape (2026)

The Raycast translation category is crowded, but the field is almost entirely
remote-API/cloud-LLM wrappers configured via extension preferences.

| Extension | Backend | Streaming | TTS | History | Offline dict |
|---|---|---|---|---|---|
| gebeto/translate (Google) | Google web (keyless) | ✗ | ✗ | ✗ | ✗ |
| tisfeng/Easydict | 10 remote providers + Apple `say`/Shortcuts | ✗ | ✓ | ✗ | ✗ (remote) |
| douo/openai-translator | multi-LLM incl. Ollama / custom endpoint | ✓ | ✗ (roadmap) | ✓ | ✗ |
| deepcast / itranslate | DeepL / multi (BYO API key) | ✗ | ✗ | ✗ | ✗ |
| Raycast built-in Translate | unstated | ✓ | ✗ | ✗ | ✗ (Pro-gated) |

**What `translate` does that they don't:**

- **Shells out to a dedicated local translator CLI** — no surveyed extension
  wraps a standalone translator binary (Easydict shells to `osascript`/Apple
  Shortcuts; none to a CLI like this).
- **Automatic multi-engine failover** — competitors that support multiple engines
  do *parallel aggregation*, not "try A, on failure fall back to B".
- **Offline CC-CEDICT / ECDICT dictionaries** via `translate define` — all
  competitors' dictionaries are remote.
- **Unified, searchable history** (`translate history [search]`).
- **Free local TTS** (`--speak`) — note openai-translator lists TTS as an
  unshipped roadmap item, so this is a real gap.
- **Fuzzy language resolution** (`translate lang resolve`, `chinees → zh`).

Frame differentiation as **offline-resilient + auto-fallback + unified history**,
*not* "local LLM" (openai-translator already reaches Ollama) and *not* "study
modes" (Anki / Vocabulary Builder already exist in the store).

## How ours is built

- **Script Commands** — [`../raycast/script-commands/`](../raycast/script-commands/):
  `translate-text` (dropdown language + text, `fullOutput`), `translate-and-copy`
  (`silent`, copies; blank arg → clipboard), `define-word`. Install:
  `just raycast-scripts`, then add the directory in Raycast once.
- **TS extension** — [`../raycast/extension/`](../raycast/extension/): commands
  `translate` (view: type-to-translate, language dropdown, engine-override submenu,
  Copy/Paste/Speak, selection/clipboard prefill), `translate-selection` (no-view:
  grabs the selection/clipboard and opens Translate prefilled via `launchCommand` —
  editable, not blind-paste), `define` (view: dictionary lookup +
  LLM fallback + "did you mean" suggestions), and `history` (view: browse/search
  past translations). All share `src/lib/translate.ts` (binary resolve + typed
  `execFile` wrappers mirroring `internal/engine/engine.go`'s `TranslateResult`).
  Run: `just raycast-dev` (`build`/`lint` variants exist). Live translate is
  debounced + abortable to avoid an LLM call per keystroke, with an opt-in `⌘↵`
  streaming view (`spawnTranslateStream` → `translate … --stream` → live `Detail`).

## Publishing & distribution

The journey from local development to the store, and why we default to
local/personal for now.

### Three distribution paths

| Path | Review | Audience | How | Our stance |
|---|---|---|---|---|
| **Local dev** | none | just you | `just raycast-dev` (`ray develop`) — persists in root search after you stop dev | **current default** |
| **Private / org store** | none (org-gated) | your Raycast org | set `owner` in package.json → `npm run publish`; needs Raycast Pro/Team | option if sharing to a team |
| **Public store** | **human review** | everyone | `npm run publish` opens a PR to `raycast/extensions` → CI + Raycast-team review → merge auto-publishes | deferred → [../backlog/raycast-extension.md](../backlog/raycast-extension.md) |

### It is *not* like PyPI

PyPI is self-service: register + `twine upload`, published instantly, no review.
The Raycast **public** store is curated — closer to Homebrew core:

- `npm run publish` (= `npx @raycast/api@latest publish`) **authenticates with
  GitHub** and auto-opens a pull request into `github.com/raycast/extensions`.
- Automated CI checks run, then the **Raycast team reviews** and "request[s] changes
  when required"; on merge it auto-publishes. Turnaround is days, not seconds.
- Auth is **GitHub** (for the PR). Your **Raycast account** only supplies the
  validated `author` handle (ours: `da-wei_lee`).

### Public-store requirements (checklist)

Verified against developers.raycast.com. `✓` = done in this repo.

- `✓` `author` = registered Raycast username; `license: "MIT"`; ≥1 category;
  one-sentence description; `platforms`; `package-lock.json`; `npm run build` +
  `ray lint` clean.
- `✓` **512×512 PNG icon** that reads in light + dark (not the default Raycast icon).
- `✓` **`CHANGELOG.md`** at the extension root (h2 headers; `{PR_MERGE_DATE}` placeholder).
- `✓` **`README.md`** at the extension root (setup/onboarding; media under a top-level
  `media/` folder).
- `☐` **≥3 (max 6) screenshots at 2000×1250 PNG** — captured from the *running*
  extension (a GUI step; not scriptable here).
- `☐` `npm run publish` (GitHub auth) + choose public vs private.

### The binary-dependency hurdle

The store guideline *"Avoid asking users to perform additional downloads"* is exactly
our case — the extension needs a user-installed `translate` (brew / `go install`).
"Calling known system binaries" is allowed, but a custom go-install CLI is a gray
area reviewers may push back on. Mitigations already in place:

- Extension `README.md` documents the dependency + install commands.
- A graceful **binary-not-found onboarding** view (`src/lib/binary-not-found.tsx`) with
  copyable install commands + a jump to preferences — instead of a bare error.
- We do **not** bundle the ~22 MB Go binary (store discourages "heavy" binaries;
  shipping release binaries is a separate item — `backlog/release-binaries.md`).

This friction is why the public store is deferred and local/private is the current
path; the analysis lives in [../backlog/raycast-extension.md](../backlog/raycast-extension.md).

## References

- Raycast API — manifest, commands, UI: https://developers.raycast.com/
- `useExec`: https://developers.raycast.com/utilities/react-hooks/useexec
- AI Extensions: https://developers.raycast.com/ai
- Script Commands: https://github.com/raycast/script-commands
- Prepare / Publish / Teams: https://developers.raycast.com/basics/prepare-an-extension-for-store
