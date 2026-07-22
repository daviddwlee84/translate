# Raycast integration for `translate` — Script Command + local-dev TS Extension

## Context

`translate` is a fast Go CLI/TUI translator with a clean, structured surface
(`--json`, `define`, `history`, `lang resolve`, `--speak`, multi-engine
auto-fallback, offline CC-CEDICT/ECDICT dicts, pair/learn/bilingual modes). The
user wants to bring "整套體驗" into **Raycast** so translation is reachable from a
global hotkey / root search, not just a terminal.

Research (this session) confirmed the pragmatic strategy is **reuse the binary,
don't reimplement**: a Raycast front-end shells out to `translate --json` and
renders the result. `translate --json` already emits clean JSON on this machine
(`{"translation":"Hello world","target":"en","engine":"copilot",...}`), Raycast
v1.104.23 + Node 24 are installed, and the binary is at `~/.local/bin/translate`.

**This plan ships two of the four possible tiers now**, per the user's staged ask:

1. **Track A — Script Commands** (bash): zero-build MVP to test the flow fast.
2. **Track B — local-dev TS Extension** (`@raycast/api`), **minimal loop only**:
   `Translate` (view) + `Translate Selection` (no-view). Run via `npm run dev`;
   no store submission. Define/History/Speak deferred as fast-follows.

Plus supporting artifacts:
- `just` recipes for both tracks (house style).
- **Docs**: `docs/raycast-extension.md` (competitive research + how Raycast
  extensions work + how ours is built).
- **Backlog**: `backlog/raycast-extension.md` + `TODO.md` `P?` entry — "publish
  to the store" as a deferred decision.
- **Pitfall**: the Raycast launchd-PATH trap.

**Intended outcome:** translate/define from Raycast root search and a
"translate my selection → paste" hotkey, all backed by the existing binary, with
the store path captured for later.

### Non-obvious constraints that shape the design (verified)
- **Raycast runs under launchd with a restricted PATH** that does *not* inherit
  your shell rc. `spawn("translate")` → `ENOENT`. Both tracks must resolve an
  **absolute** path (preference first, then probe `~/.local/bin`,
  `/opt/homebrew/bin`, `/usr/local/bin`, `~/go/bin`). Always test **from
  Raycast**, never a terminal (a terminal's PATH hides the bug).
- **Exit code is 0 even when an engine fails** (it falls back), so callers read
  `warnings[]` from `--json`, never the exit code.
- **`useExec` buffers (no streaming) and its 10s timeout coerces `0`→10000**;
  LLM tiers exceed 10s. We use `execFile` with `timeout: 60_000`.
- **API keys must live in `translate`'s `config.toml`, not shell env** — a key
  exported in `~/.zshrc` is absent under launchd. We pass `HOME` through so the
  CLI finds its config; document this in `raycast/README.md`.

### Reuse (do not reimplement)
- JSON contract: `internal/engine/engine.go` — `TranslateResult` / `DictEntry`
  (the TS interfaces mirror these tags exactly).
- Flag surface: `cmd/root.go` — `--to/--from/--engine/--tier/--json/--no-history/--speak`.
- Install path + probe ordering rationale: `pitfalls/duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md`
  (`~/.local/bin` must come first in the probe).

---

## Directory tree (new top-level `raycast/`, sibling of `cmd/`, `internal/`, `backlog/`)

```
raycast/
├── README.md                       # one-time install steps for both tracks + config-key note
├── script-commands/
│   ├── translate-text.sh           # fullOutput; dropdown lang + text
│   ├── translate-and-copy.sh       # silent; pbcopy + HUD; pbpaste fallback
│   └── define-word.sh              # fullOutput; dictionary lookup
└── extension/
    ├── package.json
    ├── tsconfig.json
    ├── .eslintrc.json
    ├── assets/
    │   └── extension-icon.png      # REQUIRED 512×512 PNG (placeholder ok for dev) — committed
    └── src/
        ├── lib/translate.ts        # binary resolve + typed execFile wrapper + JSON interfaces
        ├── translate.tsx           # view command
        └── translate-selection.tsx # no-view command
```

---

## Track A — Script Commands (build first; fastest feedback)

Metadata is a comment block (`# @raycast.<field>`); each `@raycast.argumentN` is a
single-line JSON object; dropdown `data` is a static `{title,value}[]`; args arrive
as `$1..$3` (max 3). Modes: `fullOutput` (window), `silent` (HUD from last stdout
line). Script Commands have **no `getSelectedText` and no preferences** → the copy
variant falls back to `pbpaste`.

All three share the same PATH probe (shown once below; repeat verbatim in each):

```bash
find_translate() {
  for d in "$HOME/.local/bin" /opt/homebrew/bin /usr/local/bin "$HOME/go/bin"; do
    [ -x "$d/translate" ] && { printf '%s\n' "$d/translate"; return 0; }
  done
  command -v translate 2>/dev/null && return 0
  return 1
}
```

### `translate-text.sh` (fullOutput)
```bash
#!/usr/bin/env bash
# @raycast.schemaVersion 1
# @raycast.title Translate Text
# @raycast.mode fullOutput
# @raycast.packageName Translate
# @raycast.icon 🌐
# @raycast.argument1 { "type": "dropdown", "placeholder": "Language", "data": [ {"title":"English","value":"en"}, {"title":"Chinese (Traditional)","value":"zh-TW"}, {"title":"Chinese (Simplified)","value":"zh-CN"}, {"title":"Japanese","value":"ja"}, {"title":"Korean","value":"ko"}, {"title":"Spanish","value":"es"}, {"title":"French","value":"fr"}, {"title":"German","value":"de"}, {"title":"Italian","value":"it"}, {"title":"Portuguese","value":"pt"} ] }
# @raycast.argument2 { "type": "text", "placeholder": "Text to translate" }
# @raycast.description Translate text into the chosen language via the translate CLI.
# @raycast.author David Lee
# @raycast.authorURL https://github.com/daviddwlee84
set -euo pipefail
# <find_translate() here>
BIN="$(find_translate)" || {
  echo "translate CLI not found (looked in ~/.local/bin, /opt/homebrew/bin, /usr/local/bin, ~/go/bin)."
  echo "Install: 'just install' or 'brew install daviddwlee84/tap/translate'."
  exit 1
}
"$BIN" "$2" --to "$1"
```

### `translate-and-copy.sh` (silent; clipboard-input fallback)
Same header but `# @raycast.title Translate & Copy`, `# @raycast.mode silent`,
`# @raycast.icon 📋`, and `argument2` marked `"optional": true` with placeholder
`"Text (blank = use clipboard)"`. Body after `find_translate`:
```bash
TEXT="${2:-}"; [ -z "$TEXT" ] && TEXT="$(pbpaste)"
[ -z "$TEXT" ] && { echo "Nothing to translate (empty arg and clipboard)."; exit 1; }
RESULT="$("$BIN" "$TEXT" --to "$1")"
printf '%s' "$RESULT" | pbcopy
echo "Copied → $1: $RESULT"   # silent mode surfaces this last line as a HUD
```

### `define-word.sh` (fullOutput)
Header `# @raycast.title Define Word`, `# @raycast.mode fullOutput`,
`# @raycast.icon 📖`, single `argument1` text "Word". Body: `"$BIN" define "$1"`.

---

## Track B — TS Extension (minimal loop: `translate` view + `translate-selection` no-view)

Defer `define`/`history`/`speak` *commands* (same `--json` plumbing → cheap
follow-ups). `--speak` is wired as an **action inside the view**, not a command.
Pin React 18.3 / `@types/react` ^18 to avoid v19 friction with `@raycast/api`.

### `package.json`
Key fields: `name/title/description`, `icon: "extension-icon.png"`,
`author: "daviddwlee84"`, `license: "MIT"`, `platforms: ["macOS"]`,
`categories: ["Productivity","Developer Tools"]`.
- `commands`: `{name:"translate",mode:"view"}`, `{name:"translate-selection",mode:"no-view"}`.
- `preferences`: `binaryPath` (textfield, optional), `defaultTarget` (default `"en"`),
  `engine` (textfield, optional), `tier` (dropdown default/fast/max, default `"default"`).
- `dependencies`: `@raycast/api ^1.103.6`, `@raycast/utils ^2.2.2`.
- `devDependencies`: `@raycast/eslint-config`, `@types/node ^22`, `@types/react ^18.3`,
  `eslint ^8.57`, `prettier ^3.5`, `react ^18.3`, `typescript ^5.8`.
- `scripts`: `dev: "ray develop"`, `build: "ray build"`, `lint: "ray lint"`,
  `fix-lint: "ray lint --fix"`, `publish: "npx @raycast/api@latest publish"`.

`tsconfig.json`: standard Raycast (`module: commonjs`, `target: ES2022`,
`jsx: react-jsx`, `strict`, `resolveJsonModule`, include `src/**/*` + `raycast-env.d.ts`).
`.eslintrc.json`: `{ "root": true, "extends": ["@raycast"] }`.

### `src/lib/translate.ts` — the one integration seam
- TS interfaces mirroring `TranslateResult` / `DictEntry` (translation,
  detected_source, target, alternatives[], notes, confidence, warnings[], engine,
  model, dictionary{word,phonetic,meanings[]}, suggestions[]).
- `resolveBinary()`: preference `binaryPath` (if `existsSync`) → else probe
  `[~/.local/bin, /opt/homebrew/bin, /usr/local/bin, ~/go/bin]` (cached); throw a
  friendly "not found — set preference / just install / brew install" error.
- `runTranslate(text, opts)`: `execFile(bin, [text,"--to",target,"--json", …engine/tier/from/--no-history])`
  with `{ timeout: 60_000, maxBuffer: 16MB, env: {...process.env, HOME} }`;
  `JSON.parse(stdout) as TranslateResult`. (`execFile`, not `useExec`, for the
  60s timeout.)
- `runDefine(word)` and fire-and-forget `speak(text,to)` (`--speak`) for reuse.

### `src/translate.tsx` (view)
`List` with `onSearchTextChange`(throttle) as the input, `List.Dropdown`
(searchBarAccessory, `storeValue`) as the 10-language target picker, driven by
`usePromise(runTranslate, [text, to])`. Render the result as a `List.Item` with
`List.Item.Detail` (markdown = translation + alternatives + notes + warnings;
metadata = engine/model/source/target/confidence). `ActionPanel`:
`Action.CopyToClipboard`, `Action.Paste`, and a `Speak` action calling `speak()`.
`List.EmptyView` for the idle and error states.
> Implementation note: use `List.Item.Detail.Metadata.*` (not `Detail.Metadata.*`)
> inside `List.Item.Detail`; reconcile the exact component name when `ray develop`
> type-checks.

### `src/translate-selection.tsx` (no-view)
`getSelectedText()` (try/catch → fall back to `Clipboard.readText()` → else
`showHUD("No text selected")`); `runTranslate(text, {to: defaultTarget})`;
`Clipboard.paste(res.translation)`; `showHUD` success (append `⚠ warnings[0]` if
present) or failure. Uses programmatic `Clipboard.paste` (no-view has no ActionPanel).

`raycast-env.d.ts` is generated by `ray develop`/`ray build` — never hand-written
(and git-ignored).

---

## Justfile recipes (append; lowercase/kebab, one-line `#` doc each, plain sh)

```makefile
# make the Raycast script-commands executable + show how to add them
raycast-scripts:
    chmod +x raycast/script-commands/*.sh
    @echo "Add in Raycast → Settings → Extensions → Script Commands → Add Script Directory:"
    @echo "  {{justfile_directory()}}/raycast/script-commands"

# run the TS extension in dev (registers it in Raycast; persists after you stop)
raycast-dev:
    cd raycast/extension && ([ -d node_modules ] || npm install) && npm run dev

# type-check / build the extension bundle (does NOT install into Raycast)
raycast-build:
    cd raycast/extension && ([ -d node_modules ] || npm install) && npm run build

# lint the extension with the Raycast eslint config
raycast-lint:
    cd raycast/extension && ([ -d node_modules ] || npm install) && npm run lint
```

**Honest headless-install answer (state in `raycast/README.md`):** neither track
can be fully installed by `just` alone.
- Script Commands need a one-time GUI action (Add Script Directory); the recipe
  only `chmod`s and prints the path.
- The extension is registered by `ray develop` (`npm run dev`) talking to the
  running Raycast app; it then **persists in root search after dev stops**.
  `ray build` only bundles/type-checks. There is no `ray install <dir>`.

---

## `.gitignore` additions
```gitignore
# Raycast TS extension
raycast/extension/node_modules/
raycast/extension/dist/
raycast/extension/.raycast/
raycast/extension/raycast-env.d.ts
```
`assets/extension-icon.png` is **committed** (required input, not a build artifact).

---

## Docs — `docs/raycast-extension.md` (new `docs/` dir; matches the project's reserved `docs/<tool>.md` reference surface)

Present-tense reference reading. Sections:
1. **Integration tiers** — comparison table (Quick Link / Script Command / TS
   Extension / AI Extension) across: reuses binary?, effort, selection→translate,
   persisted defaults, rich UI/history, streaming, distribution.
2. **How Raycast extensions work** — manifest `commands[].mode` (view/no-view/menu-bar);
   `execFile`/`useExec` vs manual `spawn` streaming; `preferences` +
   `getPreferenceValues`; `getSelectedText`/`Clipboard`; `List`/`List.Dropdown`/`Detail`;
   `ActionPanel`; `ray` CLI + `npm run dev` persistence.
3. **Gotchas** — launchd PATH; timeout coercion; HOME/config.toml (not shell env);
   exit-code-0-on-fallback → read `warnings[]`; no streaming in MVP.
4. **Competitive landscape** — table of prior art and our gaps:

   | Extension | Backend | Streaming | TTS | History | Offline dict |
   |---|---|---|---|---|---|
   | gebeto/translate | Google web (keyless) | ✗ | ✗ | ✗ | ✗ |
   | tisfeng/Easydict | 10 remote providers + Apple `say`/Shortcuts | ✗ | ✓ | ✗ | ✗ (remote) |
   | douo/openai-translator | multi-LLM incl. Ollama/custom endpoint | ✓ | ✗ (roadmap) | ✓ | ✗ |
   | deepcast / itranslate | DeepL / multi (BYO key) | ✗ | ✗ | ✗ | ✗ |
   | Raycast built-in Translate | unstated (Pro-gated) | ✓ | ✗ | ✗ | ✗ |

   **Our differentiation** (verified genuinely novel): shells out to a dedicated
   local translator CLI; **automatic multi-engine failover** (competitors do
   parallel aggregation, not fallback); **offline CC-CEDICT/ECDICT** via
   `define`; **unified searchable history**; free local **TTS** (`--speak`);
   fuzzy `lang resolve`. Frame as "offline-resilient + auto-fallback + unified
   history", NOT "local LLM" (openai-translator already has Ollama) and NOT
   "study modes" (Anki/Vocabulary Builder exist).
5. **How ours is built** — pointer to `raycast/script-commands/` and
   `raycast/extension/`, the `just raycast-*` recipes, install steps.
6. **References** — developers.raycast.com (manifest, useExec, AI), script-commands repo.

Update `pitfalls/README.md`'s "Cross-referenced pitfalls" note only if needed;
`docs/<tool>.md` is already referenced there as the reserved reference surface.

---

## Backlog — deferred "publish to store" decision (house style: 3 bold lines + sections)

### `backlog/raycast-extension.md`
```markdown
# Publish the Raycast extension to the store

**Status**: P? — local-dev shipped; publish deferred
**Effort**: L
**Related**: `TODO.md` P? · `raycast/extension/` · [docs/raycast-extension.md](../docs/raycast-extension.md) · [homebrew-distribution.md](homebrew-distribution.md)

## Context        # local dev via `npm run dev` works + persists; publishing is the open question
## Investigation  # store review SOFT-discourages requiring a manual binary install (our go install/brew dep);
                  # calling a user-installed binary is a sanctioned pattern; private org store has no public review;
                  # public needs MIT wrapper + 512×512 icon + ≥3 screenshots (2000×1250) + CHANGELOG + category;
                  # no telemetry / no Keychain; expose openrouter key via prefs, not config.toml, if published.
## Options considered   # | Option | What | Verdict |
                        # A. Local dev only (personal)  — zero review, per-machine, current default
                        # B. Private org store (Pro/Team) — internal share, no public review
                        # C. Public store — widest reach, review friction over the binary dependency
## Current blocker / open questions  # is public worth it vs crowded category? binary-detection onboarding UX?
## Decision (if any)   # `2026-07 deferred` — ship local-dev (Track B) first; revisit publish after dogfooding
## References          # developers.raycast.com Prepare/Publish/Teams pages
```

### `TODO.md` — new bullet under `## P?`
```markdown
- [ ] **[?/L] Publish the Raycast extension to the store** — the local-dev extension (`raycast/extension`) works via `npm run dev`; publishing publicly hits the "avoid requiring manual installs" review guideline (it depends on the `translate` binary). Evaluate private org store vs public, icon/screenshots/CHANGELOG, and graceful binary-not-found onboarding. → [research](backlog/raycast-extension.md)
```

### `backlog/README.md` — new Index row (alphabetical, after `homebrew-distribution`)
```markdown
| `raycast-extension` | P? — local dev shipped, publish deferred (2026-07) | P? "Publish the Raycast extension to the store…" |
```

---

## Pitfall — Raycast launchd PATH trap

### `pitfalls/raycast-launchd-path-translate-not-found.md` (symptom-first)
```markdown
# Raycast can't find `translate` (`spawn translate ENOENT` / "command not found") though it works in a terminal

**Symptoms** (grep this section): Raycast script command prints "translate: command not found"; TS extension throws `Error: spawn translate ENOENT`; the same command works fine in a terminal; translation works from the shell but not from Raycast root search.
**First seen**: 2026-07 (anticipated — designed around, not yet hit in prod)
**Affects**: Raycast (macOS) Script Commands + TS extensions on this host; launchd-launched processes do not inherit the interactive shell PATH, so `~/.local/bin`, `/opt/homebrew/bin`, `~/go/bin` are absent.

## Symptom      # bare `translate`/`spawn("translate")` fails only under Raycast
## Root cause   # Raycast runs under launchd with a minimal PATH; ~/.zshrc/~/.zprofile additions are not applied
## Workaround   # resolve an ABSOLUTE path: preference `binaryPath` first, else probe [~/.local/bin, /opt/homebrew/bin, /usr/local/bin, ~/go/bin]; probe order must put ~/.local/bin first (see sibling)
## Prevention   # never call bare `translate` from Raycast; always test FROM Raycast (a terminal's PATH hides the bug)
## Related      # sibling: duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md ; docs/raycast-extension.md "Gotchas"
```
Add the matching alphabetical row to `pitfalls/README.md` Index (symptom keywords:
`spawn translate ENOENT, command not found in Raycast, works in terminal not Raycast, launchd PATH not inherited`).

---

## Sequencing & end-to-end verification (this machine: Raycast 1.104.23, Node 24, binary present)

**Tier 0 — prereqs (already true):** `which -a translate` prefers `~/.local/bin`;
`translate "hola" --to en --json --no-history` returns JSON with `translation`/`engine`.

**Tier 1 — Script Commands (do first):**
1. Create the 3 `.sh` files → `just raycast-scripts` (chmod + prints path).
2. Raycast → Settings → Extensions → Script Commands → **Add Script Directory** →
   `…/raycast/script-commands`.
3. Verify **from Raycast** (not a terminal): Translate Text (pick lang + type →
   window shows translation); Translate & Copy (arg text, then arg blank after
   copying → HUD "Copied →", clipboard holds result); Define Word (→ dictionary).

**Tier 2 — TS extension MVP:**
1. Create `extension/` files; drop a real 512×512 `assets/extension-icon.png`
   (a plain placeholder PNG is fine for dev — a scripted solid-color PNG or any
   square image; only store publish needs a designed icon).
2. `just raycast-dev` → installs deps + `ray develop` (hot reload, registers).
3. Verify: **Translate** (type, switch dropdown — persists via `storeValue`,
   Detail shows translation/alternatives/notes/engine, Copy/Paste/Speak work);
   **Translate Selection** (select text elsewhere → run → pasted + HUD; select
   nothing → HUD "No text selected").
4. Ctrl-C `ray develop` → both commands still appear in root search (dev
   registration persists; no publish needed).
5. `just raycast-lint` clean.

**Docs/backlog/pitfall:** land the four prose files; `scripts/todo-kanban.sh`
(if present) still validates `TODO.md`; `backlog/README.md` + `pitfalls/README.md`
Index rows added.

**Tier 3 — deferred (not this plan):** `define`/`history`/`history search`/`speak`
commands, command arguments, menu-bar, streaming via `spawn`, and store publish
(`backlog/raycast-extension.md`).

## Risks
- **launchd PATH** (highest) — mitigated by preference + 4-dir probe; test from Raycast.
- **Icon required & binary** — extensions can't use emoji icons; need a real PNG or
  `ray build`/`lint` complains (manual asset step).
- **`List.Item.Detail.Metadata` vs `Detail.Metadata`** — reconcile exact component
  name when `ray develop` type-checks the view.
- **Config keys, not shell env** — API keys must be in `translate`'s `config.toml`;
  document in `raycast/README.md`.
- **Commit hygiene** — Conventional Commits (`feat(raycast): …`); `.gitignore` must
  land before any `npm install` so `node_modules/` isn't staged.
