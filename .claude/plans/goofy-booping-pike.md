# Windows portability + a Quick AI (`@translate`) layer for the Raycast extension

## Context

Two questions, both answered "yes, with bounded work".

**Windows.** The extension is a local-CLI wrapper, which the Raycast platform docs
put at 70–90% shared code. Verified here: no `menu-bar` command, no
`runAppleScript`, no `sips`/`open`/`osascript`, no `process.platform` branching
anywhere in `src/`, and every subprocess call is already `execFile` with an argv
array (never `shell: true`). On the Go side `GOOS=windows GOARCH=amd64 go build`
succeeds today (ran it), `internal/tts/native.go` already branches to PowerShell,
`internal/store/flock_other.go` is the `!unix` fallback, and `internal/xdgpath`
uses `os.UserHomeDir()` so config resolves to `%USERPROFILE%\.config\translate\`.
The blockers are three concrete things, listed in Part 1.

`backlog/raycast-extension.md:35` currently records the opposite — *"the binary
shell-out doesn't work on Raycast's iOS/Windows clients"*. That is wrong for
Windows; `execFile` of a user-installed binary is supported there. Correcting it
is part of this work.

**Quick AI.** `@translate` in Quick AI / AI Chat needs a `tools[]` array and an
`ai.yaml`. Neither exists; `docs/raycast-extension.md:22-29` deferred the whole
idea because "the AI API requires Raycast Pro". That reason is stale — Raycast
Settings → AI also offers a free message allowance, Ollama, and Custom Providers
(GitHub Copilot is listed), so the same copilot-proxy backend this CLI already
uses can back Raycast AI. The correct gate is `environment.canAccess(AI)`, and a
`tools[]` entry needs no gate at all, because Raycast only invokes it after the
user has cleared whichever gate applies.

The motivating failure is real and reproducible:

```console
$ ./translate "shim" --to zh-TW --json
"translation": "n. 填片, 万能开锁片"        # a carpentry wedge — routed to the dictionary engine

$ ./translate "shim" --to zh-TW --learn --json --instructions "in the context of API rate limiting"
"translation": "墊片（在軟體工程中指用來相容或攔截 API 呼叫的小型程式碼層）"
```

And the tutor prompt is already ~80% written: `internal/engine/prompt.go:186-208`
(`learnTeachPrompt`) has the warm-tutor tone and a `vocab[]` schema carrying
`pos` / `phonetic` (KK/IPA) / `meaning` — the 詞性/KK音標/意思 triple from the GPT
prompt — plus `examples[]` and `notes`. `--instructions` is injected into the
system prompt at `prompt.go:108-110` for all three prompt families, which is the
zero-code lever for domain context.

## Decisions taken

| Question | Answer |
| --- | --- |
| Windows | Port the code **and** declare `"platforms": ["macOS", "Windows"]` now |
| Learning mode | Both, staged: `ai.yaml` persona first (Part 2), Go `explain` direction second (Part 3) |
| Tool surface | **Minimal — two tools.** Intelligence lives in the prompt |
| Table rendering | Fix at both ends (Part 4): prompt emits markdown-table *structure*, Raycast normalizes defensively, and the toggle is an Action rather than a preference |

On the third: "先純tool prompt就好 不然感覺其實有點重複" is read as *keep the tool
surface small and do the work in the prompt*. So **no `learn-text` tool** (the
`ai.yaml` persona already tells the model to teach, so a tool that also teaches is
the duplication) and **no `search-history` tool** (duplicates the History command).
Two tools ship: `translate-text` and `look-up-word`. Both earn their place by
being things the model *cannot* do itself — reach the user's configured engine,
and read authoritative ECDICT phonetics.

**One flagged risk, not a blocker.** Declaring Windows ships code neither of us
has run on Windows hardware. Part 1 ends with an explicit manual verification
checklist and a README caveat rather than silence.

**One tension worth re-litigating separately.** `docs/raycast-extension.md:135`
says to position this as *"offline-resilient + auto-fallback + unified history,
not 'study modes' (Anki / Vocabulary Builder already exist in the store)"*. That
predates this idea. It does not block anything here (the extension is
personal/dogfood; `backlog/raycast-extension.md:56` defers store publishing), but
if it ever goes to the store the listing copy and this feature disagree.

---

## Part 1 — Windows portability

### 1.1 A platform seam for binary resolution

New `raycast/extension/src/lib/platform/`, branching **once** at import time —
never inside a component:

```ts
// index.ts
export const platform = process.platform === "win32" ? windows : macos;
```

Each module exports the same shape: `binaryName`, `probeDirs`, `installHint`,
`configPathHint`.

| | macOS (today's values) | Windows |
| --- | --- | --- |
| `binaryName` | `translate` | `translate.exe` |
| `probeDirs` | `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `~/go/bin` | `~/.local/bin`, `~/go/bin`, `%LOCALAPPDATA%\Programs\translate`, `~\scoop\shims`, `%ChocolateyInstall%\bin`, `%ProgramFiles%\translate` |
| `installHint` | `brew install …` / `go install …` | `go install …` only — there is no Windows release artifact yet |
| `configPathHint` | `~/.config/translate/config.toml` | `%USERPROFILE%\.config\translate\config.toml` |

`resolveBinary()` (`src/lib/translate.ts:123-156`) keeps its structure — preference
first with `existsSync` validation, then the probe, then throw — and takes
`platform.probeDirs` / `platform.binaryName` instead of the module-level
constants. Its `~/.local/bin` and `~/go/bin` entries already use `join(homedir(),
…)`, so those two resolve correctly on Windows unchanged; only the two hardcoded
POSIX absolutes and the missing `.exe` are the bug. Build every path with
`node:path` — no string literal containing a separator.

`baseEnv()` (`translate.ts:158-161`) should force `HOME` only on non-win32; Go
reads `%USERPROFILE%` there, and `...process.env` already carries it.

`src/lib/binary-not-found.tsx` and the install/config strings in
`translation-detail.tsx:90` and `define-detail.tsx:108,119` read from
`platform.installHint` / `platform.configPathHint`.

### 1.2 One shortcut table, two branches

All 20 `Keyboard.Shortcut` objects in `src/` hardcode `cmd`, which Windows
**drops in silence** — no error, no lint failure, the action just renders with no
key. New `src/lib/shortcuts.ts` is the single source:

```ts
const S = (key: Keyboard.KeyEquivalent, ...extra: Keyboard.KeyModifier[]): Keyboard.Shortcut => ({
  macOS:   { modifiers: ["cmd",  ...extra], key },
  Windows: { modifiers: ["ctrl", ...extra], key },
});

export const SHORTCUTS = {
  submit: S("return"), info: S("i"), cycleEngine: S("e"), speak: S("l"),
  refresh: S("r"), loadSelection: S("s", "shift"), loadClipboard: S("v", "shift"),
  clear: S("x", "shift"), advanced: S("a", "shift"), translateWord: S("t", "shift"),
} as const;
```

Deliberately **not** using `Keyboard.Shortcut.Common.*`: it maps per platform for
free, but its bindings surprise (`Common.Remove` is ⌃X, not ⌘⌫; `Common.Duplicate`
is ⌘D) and would silently change the existing keys. The two-branch form is
explicit and the type makes both branches mandatory.

The same file exports a `label(name)` helper so the ⌘-glyph copy at
`translate-text.tsx:189` (`"⌘⇧S loads the selection · …"`) and `translate.tsx:227`
(`"(⌘E)"`) is derived from the table rather than restated — that is where drift
would otherwise start.

Call sites to update: `translate.tsx` (3), `translate-text.tsx` (5),
`look-up-word.tsx` (4), `define.tsx` (1), `history.tsx` (2),
`lib/translation-detail.tsx` (1), `lib/history-detail.tsx` (2),
`lib/define-detail.tsx` (2).

### 1.3 Manifest

- `"platforms": ["macOS", "Windows"]` (currently `["macOS"]` at `package.json:10`).
- `binaryPath` preference: replace the POSIX `placeholder` with a platform-keyed
  object, `{"macOS": "/Users/you/.local/bin/translate", "Windows":
  "C:\\Users\\you\\go\\bin\\translate.exe"}`. Note the doubled backslashes — a
  single `\U` is invalid JSON and fails before `ray lint` sees it. **Verify
  `ray lint` accepts a platform-keyed `placeholder`**; the schema allows the form
  "anywhere a plain value is allowed", but if it rejects `placeholder`
  specifically, fall back to a neutral one-line string.

### 1.4 Known-acceptable Windows behaviours (document, don't fix)

- `child.kill("SIGTERM")` (`translate.ts:216`, `:610`) — Node terminates hard on
  Windows regardless of the signal name. Add a comment; the semantics we need
  (cancel a superseded call) still hold.
- `getSelectedText()` is undocumented on Windows. All three call sites already
  `try/catch` into a clipboard fallback, so it degrades rather than breaks.
- `raycast/script-commands/*.sh` stay macOS/Linux — a separate, bash-only track.
- Config lands at `%USERPROFILE%\.config\translate\config.toml`, which is
  un-idiomatic for Windows but consistent across hosts. Leave `internal/xdgpath`
  alone; document it.

### 1.5 Distribution

No Go changes are required, but there is no Windows artifact. `go install
github.com/daviddwlee84/translate@latest` works on Windows and becomes the
documented path. Add `windows/{amd64,arm64}` to the target list in
`backlog/release-binaries.md` (it currently lists only darwin+linux) and note in
`raycast/extension/README.md` that Homebrew is macOS/Linux only.

### 1.6 Manual verification (cannot be automated from here)

Ships in the PR description and `raycast/README.md`:

1. `go install …@latest` on Windows; confirm `translate.exe` in `~\go\bin`.
2. `translate init` writes a config; `translate "hola" --to en` works from
   PowerShell.
3. **Open each command from Raycast root search, not `ray develop`'s console** —
   the dev console inherits the interactive environment and hides exactly this
   class of bug.
4. Every action shows a Ctrl-based key in the action panel.
5. Translate Selection and selection-prefill: verify or record as macOS-only.

---

## Part 2 — Quick AI: two tools + the persona

### 2.1 `tools[]` (manifest)

```json
"tools": [
  { "name": "translate-text", "title": "Translate Text",
    "description": "Translate text using the user's own configured translate engine, preset and fallback chain." },
  { "name": "look-up-word", "title": "Look up Word",
    "description": "Look up a word in the local bilingual dictionary and return its phonetic transcription and senses." }
]
```

`description` has a 12-character minimum and is **prompt engineering, not
documentation** — it is the primary signal the model uses to choose between them.
Schemas can be lifted from `internal/mcpserver/tools.go:17-32`, which already
ships equivalents as MCP tools.

`tools[]` alongside `"platforms": ["macOS", "Windows"]` is undocumented by Raycast
but shipped in the store (`extensions/emoji-kitchen`), so the combination passes
review.

### 2.2 `src/tools/translate-text.ts`

Filename must equal `tools[].name`. Wraps the existing `runTranslate`.

```ts
type Input = {
  /** The text to translate, verbatim. Do not pre-translate or rephrase it. */
  text: string;
  /** Target language code, e.g. "en", "zh-TW", "ja". Omit to use the user's configured default. */
  to?: string;
  /** Prompt style. "concise" returns only the translation; "contextual" lists 2-4 distinct senses with context labels; "dictionary" adds example sentences. Prefer "contextual" when the user asks how a word's meaning shifts. */
  preset?: "concise" | "contextual" | "dictionary";
  /** Domain context for the translation, e.g. "API rate limiting", "React". Pass this whenever the user names a context. */
  instructions?: string;
};
```

Typing discipline is load-bearing — schema extraction is the one thing `ray build`
*does* enforce, and it fails the build on `any`/`unknown`/`Pick`/`Partial`/numeric
unions. Two silent traps to avoid: `interface Input extends Base {}` extracts as
`{}` with no error, and JSDoc on the *same line* as the field is dropped without
warning.

Supporting change in `src/lib/translate.ts`: `TranslateOptions` gains
`preset?: "concise" | "contextual" | "dictionary"` and `instructions?: string`,
pushed as `--preset` / `--instructions` in `runTranslate`'s argv builder
(`translate.ts:286-334`). Both flags already exist on the CLI; the extension just
never used them. This also unlocks a future UI control.

Return the `TranslateResult` as data. Catch `isBinaryMissing` / spawn failures and
return `{ error: "…" }` — an unhandled throw ends the run with an error screen
instead of letting the model explain itself.

### 2.3 `src/tools/look-up-word.ts`

Wraps `runDefine`. Its value is `DictEntry.phonetic` from ECDICT/CC-CEDICT: real
KK/IPA the model would otherwise invent. `runDefine` throws a tagged
`isNoDictEntry` error on a hard miss — catch it and return
`{ error, suggestions }` as data, since `TranslateResult.suggestions[]` is exactly
what the model needs to offer a "did you mean".

### 2.4 `ai.yaml`

Exactly one AI file in the extension root (`ai.json`/`ai.json5`/`ai.yaml`/`ai.yml`
are all accepted and the *first* match wins, so a stray one shadows silently).
`instructions` is injected as a system message whenever the extension is
mentioned. This is where the GPT prompt is ported — it carries the persona, and
the two tools carry the facts.

```yaml
instructions: |
  You help the user with Chinese ⇄ English, and you teach the other language
  while you do it.

  Tools — prefer them over your own knowledge:
  - The user hands you text to translate → call `translate-text` and present its
    `translation` as the primary answer. It comes from the user's own configured
    engine; do not re-translate or "improve" it.
  - A word's meaning, part of speech, or pronunciation → call `look-up-word`
    first. Its `phonetic` field is authoritative. Never invent KK/IPA.
  - When the user names a domain ("in rate limiting", "in React"), pass it as
    `instructions`, and explain the sense that fits that domain. Do not give the
    literal dictionary gloss and stop — `shim` in an API context is not a
    carpentry wedge.

  Answering, unless the user explicitly asks for one language only:
  - Answer bilingually. Every English paragraph is followed by its Chinese
    translation; every Chinese paragraph by its English. Use Traditional Chinese.
  - Gloss the words worth learning: term, part of speech, KK phonetic, and a
    simple meaning in Chinese. Take the phonetic from `look-up-word`.
  - Warm and encouraging. Ask when a question is genuinely ambiguous; otherwise
    make a reasonable guess and answer usefully.

evals:
  - input: "@translate 「shim」在 rate limit 的情境下是什麼意思？"
  - input: "@translate 幫我把這句翻成英文：這個部署在金絲雀失敗後被回滾了"
  - input: "@translate 幫我看看這句英文的文法：I have went to the office yesterday"
  - input: "@translate throttle、debounce、rate limit 有什麼差別？順便教我這幾個字"
```

`evals[].input` **is** the Suggested Prompts list under `@translate` — an
extension with tools and no evals ships an empty list, which is the difference
between "what is this" and a demo. `mocks`/`expected` are not in the published
schema and validate only by omission, so a typo in either is silently ignored;
keep the evals to bare `input` unless a real regression case needs more.

### 2.5 What Raycast AI supplies that the CLI cannot

Web search, follow-up turns, and the choice of model — the three things the
original GPT used tools for. That is why the conversational half lives here and
not in Go.

---

## Part 3 — Go `explain` direction (second stage)

Stage 2 of "both, staged". `--learn` today knows only two directions, chosen
offline by script detection (`learnDirection`, `prompt.go:177-182`): `teach`
(native→foreign) and `correct` (foreign→native). Neither is "answer a question
about a term". Work:

1. `internal/engine/prompt.go` — add `learnExplainPrompt` beside the existing two,
   same JSON-object discipline, reusing the `vocab[]` / `examples[]` / `notes`
   schema so `LearnResult` barely changes (likely one new `answer` field in
   `engine.go:140-170`).
2. Direction can no longer be fully auto-detected — add
   `--learn-mode auto|teach|correct|explain` (default `auto` = today's behaviour)
   rather than overloading the `--learn` bool.
3. Plumb the gap agent-2 found: `learn` is missing from `appcore.Params`
   (`service.go:43-52`), the HTTP `translateRequest` (`server/handlers.go:22-31`),
   and the MCP `translateInput` (`mcpserver/tools.go:17-32`). Adding it there is
   what gives MCP hosts and `translate serve` the same capability.
4. Document `--learn` in `README.md`, which does not mention it at all today.

**Sync risk, and the mitigation.** Once the Go direction exists, the persona lives
in two places. Do not restate it: trim `ai.yaml` `instructions` to routing rules
("call `explain-term` for this kind of question") and let the Go prompt own the
voice — the same relationship `translate-text` already has with the translation
presets.

---

---

## Part 4 — Tabular output renders as garbage in `Detail`

Reported with a screenshot: a translated results table reflowed into wrapped
`────` runs and loose `| dense-95 | 0.1210 |` rows instead of a table.

**Root cause.** `renderTranslation` (`lib/translation-detail.tsx:98-107`) builds
`[r.translation, ""]` — the model's raw output goes straight into `Detail`'s
markdown with no fencing and no normalization. `StreamView`
(`lib/stream-view.tsx:51`) does the same with the accumulated stream. So two
things break at once: long `─` rules are treated as prose and wrapped, and the
pipe rows never form a GFM table because no valid `|---|---|` separator precedes
them (or the cell counts differ row to row).

**The insight that decides the design:** character-level column alignment cannot
be portable across these front-ends. CJK glyphs are double-width in a terminal and
proportional in Raycast's markdown, so padding that aligns in one is guaranteed to
break in the other. Only *structure* (pipes + a separator row) travels. So the fix
is to stop preserving padding and start preserving structure.

### 4.1 Go: emit structure, not padding (the high-leverage half)

`internal/engine/prompt.go` — append a `tableDirective` in `buildTranslatePrompt`,
using the exact mechanism `pairDirective` already uses at `prompt.go:105-107`
(appended to whichever preset is active, so the concise/contextual/dictionary
output shape survives). Appending beats editing all three preset consts, which
would triplicate the rule.

Roughly: *"The source text is tabular. Reproduce it as a GitHub-flavoured markdown
table: one row per line, the same number of `|`-delimited cells in every row, and
a `|---|---|` separator row after the header. Translate the cell contents only —
keep numbers, identifiers and units verbatim. Do NOT pad cells with spaces to
align columns, and do not emit box-drawing rules."*

Gate it on a cheap pure detector so ordinary text pays no token cost — same spirit
as `internal/bitext`'s `Kind` classification, and a natural home for it (it is
already the project's pure, unit-tested text-shape package). Heuristic: ≥2
consecutive lines each holding ≥2 `|`, **or** ≥3 lines whose whitespace runs align
into consistent column starts.

This benefits the CLI, TUI, MCP and HTTP surfaces at once, not just Raycast.

### 4.2 Raycast: normalize defensively

New pure `src/lib/markdown.ts` — no `@raycast/api` import, so `dev-check.ts` can
assert it (the project's no-test-runner pattern):

- `normalizeTables(md)` — for each run of `|`-bearing lines: insert a `|---|`
  separator when one is missing, pad/truncate every row to the header's cell
  count, and drop bare `─`/`—` rule lines that aren't valid GFM separators.
- `fencePreformatted(md)` — for column-aligned blocks with **no** pipes, wrap in a
  code fence so it at least stays monospace, neutralising embedded backticks with
  a zero-width space so the output cannot close the fence early.

This is needed regardless of 4.1, because it also repairs output from an older
installed binary — the permanent version-skew problem from
`pitfalls/raycast-extension-uses-installed-binary-not-working-tree.md`.

Applied in `renderTranslation`, `history-detail.tsx`, and `define-detail.tsx`'s
LLM-fallback prose. **In `StreamView`, render raw while streaming and normalize
only in `onDone`** — a half-arrived table has no header row to normalize against,
and repairing it per chunk would make the view flicker between shapes.

### 4.3 The toggle, as an Action rather than a preference

`Action` "Show Raw Output" / "Show Formatted" in the `Detail` action panels, keyed
through the new `SHORTCUTS` table from §1.2. **Not** a preference: a stored
preference value survives a manifest `default` change
(`pitfalls/raycast-still-prefills-clipboard-after-default-changed-to-nothing.md`),
which is the wrong behaviour for something you want to flip per-result.

`Action.CopyToClipboard` and `Action.Paste` keep copying `data.translation`
**raw** — normalization is a viewing concern, and pasting repaired markdown into
the user's editor would be a surprise.

---

## Verification

Order matters. The extension runs the **installed** binary, never the working tree
(`pitfalls/raycast-extension-uses-installed-binary-not-working-tree.md`) — and
`--preset`/`--instructions` passthrough will silently no-op against a stale one.

```sh
just check && just install          # ALWAYS first
translate --version                 # confirm the dev build, not the tag
```

Add a `raycast-check` recipe to the `Justfile` — there is currently **no
typecheck at all** for the extension, and `ray build` in its default `dev`
environment does not typecheck (esbuild strips types without checking them):

```make
raycast-check:
    cd raycast/extension && ([ -d node_modules ] || npm install) \
      && npx tsc --noEmit -p tsconfig.json \
      && CI=true npx ray lint \
      && npx ray build \
      && npx ray build -e dist
```

`CI=true` matters: `ray lint` runs two extra checks only in CI mode, including
that every `resolved` URL in `package-lock.json` points at `registry.npmjs.org`.

Then:

```sh
just raycast-check
cd raycast/extension && npx ray build --print-tool-schemas
```

Read that output rather than trusting the build: assert both tools have non-empty
`properties`, and that **every** property carries a `description`. That is the
only check that catches the `interface … extends` → `{}` trap and same-line JSDoc.

```sh
npx ray develop --print-tool-calls      # then, in Quick AI: @translate 「shim」在 rate limit…
npx ray evals                            # needs AI access + network; manual, not a gate step
bash .claude/skills/raycast-extension-dev/scripts/check-store-readiness.sh raycast/extension
```

`check-store-readiness.sh` runs `platforms-windows-macos-apis`, which greps `src/`
for macOS-only APIs while `Windows` is declared — the check that would catch a
`runAppleScript` sneaking in later. It should pass today.

Windows itself: `GOOS=windows GOARCH=amd64 go build -o /tmp/translate.exe .`
proves the binary; the extension needs the manual checklist in §1.6.

Behavioural check that the whole point works, in Quick AI:

> `@translate 「shim」在 rate limit 的情境下是什麼意思？`

Should call `look-up-word` (real phonetic) and/or `translate-text` with
`instructions`, and answer bilingually with a software sense — **not** 填片/万能开锁片.

Table rendering (Part 4) needs the reported input, not a synthetic one. Keep the
original tabular text as a fixture:

```sh
go test ./internal/bitext/...                          # the table detector
node .build/verify/dev-check.js                        # normalizeTables / fencePreformatted
./translate "$(cat testdata/results-table.txt)" --to zh-TW   # structure, no space padding
```

then push it through **Translate Text** and confirm the `Detail` shows a real
table — and that ⌘↵ streaming still renders progressively rather than flickering
between shapes, since normalization there is deferred to `onDone`.

## Files

**New** — `raycast/extension/src/lib/platform/{index,macos,windows}.ts`,
`src/lib/shortcuts.ts`, `src/lib/markdown.ts`,
`src/tools/{translate-text,look-up-word}.ts`, `ai.yaml`.

**Modified** — `raycast/extension/package.json` (platforms, `tools[]`,
`binaryPath` placeholder), `src/lib/translate.ts` (`resolveBinary`, `baseEnv`,
`TranslateOptions` + argv builder), all 8 command/detail files (shortcut imports),
`src/lib/translation-detail.tsx` + `src/lib/stream-view.tsx` +
`src/lib/history-detail.tsx` + `src/lib/define-detail.tsx` (normalization + the
raw/formatted Action), `src/lib/binary-not-found.tsx`, `src/lib/dev-check.ts`,
`internal/engine/prompt.go` + `internal/bitext/` (table directive + detector),
`Justfile` (`raycast-check`), `raycast/extension/README.md` + `raycast/README.md`
(Windows install, untested caveat), `docs/raycast-extension.md` (retract the "AI
needs Pro" deferral), `backlog/raycast-extension.md:35` (retract the Windows
claim), `backlog/release-binaries.md` (windows targets), `CHANGELOG.md`.

**Part 3 only** — `internal/engine/engine.go`, `internal/appcore/service.go`,
`internal/server/handlers.go`, `internal/mcpserver/tools.go`, `cmd/root.go`,
`README.md`.

A new pitfall entry is warranted once Part 4 lands — the symptom ("a translated
table renders as wrapped `────` runs in Raycast but looks fine in the terminal")
is exactly the grep-able shape `pitfalls/` exists for, and the underlying rule —
*character padding cannot be portable between a terminal and a proportional
renderer* — will be re-learned otherwise.
