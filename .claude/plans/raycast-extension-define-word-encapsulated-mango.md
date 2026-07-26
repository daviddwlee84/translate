# Look Up Word — a Define-Word-style Raycast command

## Context

Raycast's built-in **Define Word** has a UX our extension doesn't: open it and the list
below the (empty) search bar is your recent lookups; type and you get a *ranked list of
candidate headwords*, each with a definition preview as its subtitle; press ⏎ and you land
on a full page for that word.

Our current `define` command can't do that. It debounces 500 ms, calls
`translate define <word> --json` once, and renders **one** result inline via
`isShowingDetail`, plus a "did you mean" section on a miss. There is no candidate list, no
history, no full-page view, and no way to choose the definition language.

Two things the CLI is missing make that list impossible today:

1. **There is no "search the dictionary by prefix" capability at all.** `define` does an
   exact point lookup and, on a miss, returns bare suggestion *strings* with no previews.
   The data to do better is already on disk — ECDICT is a 770 k-row SQLite table with a
   populated frequency column — it's just never queried that way.
2. **`define` never writes history** (`cmd/define.go` opens no store), so "⏎ records the
   lookup" has nothing behind it.

The user also reported that **Translate still auto-fills from the clipboard**, which was
supposed to be fixed in `e87db06`.

**Outcome:** a new `look-up-word` Raycast command with the Define Word UX, backed by a new
`translate dict search` CLI surface (so the logic lives in `internal/engine` and every
front-end benefits, per the "reuse the binary, don't reimplement" principle in
`docs/raycast-extension.md:9-11`), plus the Translate prefill fix.

## Decisions (confirmed with the user)

| | Decision |
|---|---|
| **Command** | **New standalone command** `look-up-word`. The existing `define` command stays untouched so the two UXes can be compared side by side. Motivation for the new one: long content is much better as a full-page `Detail` with markdown rendering than as a `List.Item.Detail` pane. |
| **Language** | Raycast **command argument** dropdown (visible in the root bar *before* the view opens) **+** an in-view `List.Dropdown` searchBarAccessory. Default inherited from `general.default_target` via `readConfig()`. |
| **Empty-state list** | **All recent history**, not filtered to word lookups. |
| **Prefill** | Fix the `?? "selection"` fallback in code **and** document why an existing install still prefills (a stored preference value beats a changed manifest default). |

## Verified facts that shape the design

Measured against the live 93 MB `~/.local/share/translate/dict/ecdict.db` (770,611 rows):

- **`frq` is a frequency *rank*, not a count.** `the`=1, `a`=5, `test`=575, `tester`=7037,
  `testosterone`=10400, `zymurgy`=0 (unknown). So ordering is `(frq = 0) ASC, frq ASC` —
  **ascending, zeros last**. `frq DESC` would surface the most obscure words first and look
  plausible while being exactly backwards.
- **`LIKE 'test%'` does *not* use `idx_word_lc`** — `EXPLAIN QUERY PLAN` says `SCAN entries`
  (435 ms). A range predicate does use it (12 ms, 36× faster):
  `WHERE word_lc >= :q AND word_lc < :q || char(0x10FFFF)`.
- Full ranked query timings: `tes` 4 ms · `te` 20 ms · `a` (worst case) 33 ms warm /
  275 ms cold page cache. Bare `translate` process startup is ~55 ms.
- **Chinese is the slow path**: `translate define 貓 --plain` takes **1.7 s** because
  `cedictIndex.load()` re-parses the 9.8 MB `cedict_ts.u8` with a regexp per line on *every
  process* (`internal/engine/cedict.go:32-71`).
- `entries.exchange` carries inflections (`test` → `s:tests/d:tested/i:testing/p:tested`)
  and `tests`/`testifying` have `frq=0`. Not used in v1 — noted under Future work.
- Deps already present: `modernc.org/sqlite`, `github.com/agnivade/levenshtein`.

The verified ranking SQL, whose output for `test` is
`test, testing, testimony, testify, tester, testament, testosterone, testimonial, testicle, tested, …`
— near-identical to Raycast's built-in list:

```sql
SELECT word, phonetic, substr(translation,1,160), substr(definition,1,160), pos, frq
  FROM entries
 WHERE word_lc >= :q AND word_lc < :qUpper
 ORDER BY (word_lc = :q) DESC,          -- exact headword first
          (instr(word_lc,' ') > 0) ASC, -- single words before multi-word phrases
          (frq = 0) ASC,                -- known frequency before unknown
          frq ASC,                      -- lower rank = more common
          length(word_lc) ASC,          -- short forms before long compounds
          word_lc ASC                   -- stable
 LIMIT :n;
```

---

## Step 1 — Go core: dictionary headword search

**New `internal/engine/dictsearch.go`.** This is the whole feature's core; `cmd/` is wiring.

```go
// SearchResult is the payload of a dictionary headword search. A top-level object
// (not a bare array) so source/script/notes can ride along.
type SearchResult struct {
	Query      string      `json:"query"`
	Script     string      `json:"script"`     // "latin" | "han"
	Source     string      `json:"source"`     // "ecdict" | "cedict" | "wordlist" | "none"
	Candidates []Candidate `json:"candidates"` // never nil — encodes [] not null
	Notes      string      `json:"notes,omitempty"`
}

// Candidate is one ranked headword plus the one-line preview a picker renders as a subtitle.
type Candidate struct {
	Word     string `json:"word"`
	Phonetic string `json:"phonetic,omitempty"`
	Preview  string `json:"preview,omitempty"`
	POS      string `json:"pos,omitempty"`
	Rank     int    `json:"rank,omitempty"`     // ECDICT frq: 1 = most common, 0 = unknown
	Match    string `json:"match"`              // "exact" | "prefix" | "fuzzy"
	Distance int    `json:"distance,omitempty"` // edit distance when Match == "fuzzy"
}

// Searcher is the optional headword-search capability. *LocalDictEngine implements it;
// *DictEngine (dictionaryapi.dev) does not — remote lookup has no headword list.
type Searcher interface {
	Search(ctx context.Context, q string, limit int) (*SearchResult, error)
}

func (e *LocalDictEngine) Search(ctx context.Context, q string, limit int) (*SearchResult, error)
func mergeCandidates(exact *Candidate, prefix, fuzzy []Candidate, limit int) []Candidate // pure; test seam
func prefixUpper(p string) string { return p + "\U0010FFFF" }
```

`Search` behaviour:

1. Trim; empty query → `Candidates: []Candidate{}`, `err == nil`.
2. `limit <= 0` → `defaultSearchLimit = 12`; cap at `maxSearchLimit = 50`.
3. Route by script exactly as `Translate` does (`internal/engine/localdict.go:63-72`):
   `lang.IsChinese(q)` → `searchZh`, else `searchEn`.
4. **`searchEn`** — ECDICT present: run the ranking SQL; the row where `word_lc == q`
   becomes `Match:"exact"`, the rest `Match:"prefix"`. If fewer than `limit` rows *and*
   `len(q) >= 3`, top up from `e.wl.nearestN(lower(q), 2, remaining)`, dedupe against the
   prefix set, batch-enrich previews with one `lookupMany`, tag `Match:"fuzzy"` +
   `Distance`. Fuzzy tier is ordered **(distance asc, frq asc with 0 last, alpha)** — this
   *is* `backlog/suggestion-ranking.md` option B.
   ECDICT absent → wordlist-only tier, `Source:"wordlist"`, empty previews,
   `Notes: "ECDICT not installed — run \`translate dict update ecdict\`"`. Neither → `Source:"none"`.
5. **`searchZh`** — prefer `cedict.db` (Step 3) when present; else fall back to the in-RAM
   `cedictIndex` with `Notes` hinting at `translate dict reindex`. No edit distance for CJK
   (existing convention, `cedict.go:80-82`). Order: exact key, then rune-length asc, then key asc.
6. `Preview`: collapse newlines and the literal `\n` ECDICT uses as a sense separator
   (reuse `splitEcdict`, `ecdict.go:68-76`), prefer the Chinese `translation` field, fall
   back to `definition`, cap at 120 runes with `…`.

**Extend `internal/engine/ecdict.go`** — reuse the existing lazy `open()` (`:31-45`), still
`query_only(true)`:

```go
type ecdictRow struct{ Word, Phonetic, Translation, Definition, POS string; Frq int }
func (e *ecdictDB) prefixSearch(ctx context.Context, prefix string, limit int) ([]ecdictRow, error)
func (e *ecdictDB) lookupMany(ctx context.Context, words []string) (map[string]ecdictRow, error)
```

Add a comment on `prefixSearch` recording *both* gotchas above — `frq` is a rank, and
`LIKE` won't use the index.

**Bug fix that doubles as a test seam** — `NewLocalDict` (`localdict.go:35-42`) hardcodes
`wl: &wordIndex{path: "/usr/share/dict/words"}` and silently ignores the existing
`[dict] wordlist` config field (`internal/config/config.go:125`). Add `Wordlist string` to
`LocalDictConfig`, honor it (falling back to the hardcoded path), and wire it in
`appcore.DictFromConfig` (`internal/appcore/build.go:121-128`). This makes the fuzzy tier
testable without depending on the host's `/usr/share/dict/words`.

## Step 2 — Go: CLI wiring

**`cmd/dictupdate.go`** — `newDictCmd()` gains `dict search <prefix>`:

- `--limit` (default 12); `--json` comes from the root persistent flags (`cmd/root.go:88`).
- Plain output: `word\tpreview` per line (same spirit as `history --tsv`, so `fzf`/`tv` can
  consume it). Reuse `oneline()` from `cmd/history.go:99`.
- **Must exit 0 with `{"candidates":[]}` on a miss** — unlike `define`, which exits 1 with
  plain stderr and no JSON (`translate: dictionary: engine: no dictionary entry: "…"`).
  Only real I/O errors return non-nil.
- Opens **no** history store, so the typing path physically cannot record anything.
- `[dict] source = "api"` → `Source:"none"` + `Notes`, still exit 0.

**`cmd/lang.go`** — new `lang list [--json]` exposing `lang.List()` (35 entries,
`internal/lang/lang.go:30-66`) as `[{code, name, aliases}]`; plain output `code\tname`.
Also fix `lang resolve --json` silently ignoring `--json` (`cmd/lang.go:26-29`).

**`internal/appcore`** — keep the core reachable from every transport:

```go
// build.go
func DictSearcher(cfg *config.Config) (engine.Searcher, bool)   // (nil,false) when source = "api"
// service.go — new `search engine.Searcher` field
func (s *Service) SearchDict(ctx context.Context, q string, limit int) (*engine.SearchResult, error)
```

## Step 3 — Go: `cedict.db` index (recommended; degrades gracefully if skipped)

1.7 s per Chinese search is unusable for type-ahead, and the user's `default_target` is
`zh-TW`. Mirror `ecdictDB` in a new `internal/engine/cedictdb.go`:

```go
func CedictDBPath(dir string) string // "cedict.db", beside the existing cedict_ts.u8
const cedictSchema = `CREATE TABLE entries(key TEXT, trad TEXT, simp TEXT, pinyin TEXT, defs TEXT, n INTEGER);`
// One row per trad AND simp key (matching cedictIndex); n = rune length, for rank tiebreak.
// plus CREATE INDEX idx_cedict_key ON entries(key)

func BuildCedictDB(ctx context.Context, srcPath, dbPath string, prog func(string)) error // tmp + rename, no network
type cedictDB struct{ /* same shape as ecdictDB */ }
func (c *cedictDB) available() bool
func (c *cedictDB) lookup(ctx context.Context, word string) ([]cedictEntry, error)
func (c *cedictDB) prefixSearch(ctx context.Context, prefix string, limit int) ([]cedictRow, error)
```

- `cmd/dictupdate.go`: `dict reindex [cedict]` builds it from the already-downloaded `.u8`
  (no network, for existing installs); `dict update cedict` builds it after downloading.
- Also prefer `cedictDB` in `LocalDictEngine.lookupZh` (`localdict.go:74-94`) — this takes
  `translate define 貓` from **1.7 s → ~0.07 s** for *every* front-end (CLI, TUI, serve, mcp).
- **Deliberately do not auto-build from inside `dict search`**: a keystroke-driven process
  that Raycast aborts mid-build would never finish. Degrade to the correct-but-slow in-RAM
  path and surface `Notes` in the UI instead.

## Step 4 — Go: `define` records history, and stops lying about `target`

**`cmd/define.go`**, three changes:

1. **Set `Target`.** Line 45 builds `engine.Request{Text, Mode, Stream}` with no `Target`,
   which is why `define --json` returns `"target": ""` (`define.tsx:87` papers over it with
   `data.target || "en"`). `res := cfg.Resolve(overrides(), config.ModeCLI)` at line 41
   already honors `--to` — just pass `res.Target` through `lang.Resolve`.
2. **Record history** after `Drain`, using the existing `openStore` helper
   (`cmd/root.go:314`, which already honors `[history].enabled` + `--no-history`) and
   `appcore.ToRecord` (`internal/appcore/record.go:36-51`). This is what makes "⏎ writes
   history" true; the extension passes `--no-history` nowhere except where it must not record.
3. Help text notes that `define` now records, suppressed with `--no-history`.

**`internal/appcore/record.go`** — new `Recordable(res) bool`: false for nil, `Truncated`,
and results with no `Translation`, no `Dictionary` and no `Learn` (suggestion-only misses
and "dictionary not installed" notes). Apply in `cmd/define.go`, `cmd/root.go:335`
(`recordAndRemember`) and `internal/appcore/service.go:193`. The live history file already
has **7 of 72 `engine:"dictionary"` rows with `output:""`** (e.g. `input:"verbati"`), and
the new command's empty state will show them. Ship as its own revertible `fix(history):` commit.

**`internal/engine/localdict.go`** — `ecdictResult`/`cedictResult` currently echo
`req.Target`. Stamp the language the definition is *actually* in: `"zh-CN"` for ECDICT
glosses, `"en"` for CC-CEDICT. `--to` still controls the LLM fallback language via
`smartTarget` (`smartdict.go:134-143`) — unchanged. This makes the new history rows honest
and makes Speak pick the right voice.

## Step 5 — TS: the shell-out layer (`raycast/extension/src/lib/translate.ts`)

Still the only module that spawns the binary. Reuse `resolveBinary()` (`:89`), `baseEnv()`
(`:110`), `pexecFile`, `isBinaryMissing()` (`:106`) unchanged.

```ts
export interface Candidate { word: string; phonetic?: string; preview?: string; pos?: string;
  rank?: number; match: "exact" | "prefix" | "fuzzy"; distance?: number }
export interface DictSearchResult { query: string; script: string; source: string;
  candidates: Candidate[]; notes?: string }
export interface LangInfo { code: string; name: string; aliases?: string[] }

/** `translate dict search <q> --limit N --json`. Local data only — never an LLM call. */
export async function runDictSearch(q: string, limit = 12, signal?: AbortSignal): Promise<DictSearchResult>; // 10s / 8MB

/** `translate lang list --json`; module-level cache like readConfig(). Falls back to
 *  the hardcoded LANGS when the installed CLI predates the subcommand. */
export async function runLangs(): Promise<LangInfo[]>;

export interface DefineOptions { to?: string; smart?: boolean; noHistory?: boolean; signal?: AbortSignal }
export async function runDefine(word: string, opts?: DefineOptions): Promise<TranslateResult>;

/** True when `define` exited 1 with the plain-stderr hard-miss message and emitted no JSON. */
export function isNoDictEntry(e: unknown): boolean;

/** `translate speak <text> --lang <code>` — pronounce only; does NOT translate or record. */
export function speakText(text: string, lang?: string): void;
```

Notes:

- `runDefine` gains options (`--smart`, `--to`, `--no-history`) and a try/catch that
  inspects `(e as {stderr?: string}).stderr` for `no dictionary entry`, rethrowing a tagged
  error `isNoDictEntry` recognizes. Today that case surfaces as a raw "Lookup failed".
  Existing callers pass no options, so `define.tsx` keeps working unchanged.
- The existing `speak()` (`:191`) runs a full root translation (`[text, "--to", lang, "--speak"]`)
  and therefore **writes a history row every time you press Speak**. Add `--no-history` to
  it, and use the new `speakText` (→ `cmd/speak.go:116`) from the new command, where the
  intent is "pronounce this word", not "translate and speak".

## Step 6 — TS: the new `look-up-word` command

**New `src/look-up-word.tsx`** (existing `src/define.tsx` untouched):

```
Command()
├─ prefs = getPreferenceValues<Preferences.LookUpWord>()
├─ state: query, to  (to ← props.arguments.to → prefs.defaultTarget → config default_target → "en")
├─ debounced = useDebouncedValue(query, hasCJK(query) ? 300 : 180)   ← reuse src/lib/hooks.ts:4
├─ useEffect: inherit `to` from readConfig().general.default_target  ← same shape as translate.tsx:56-69
├─ search  = usePromise(runDictSearch, [debounced, ...], { abortable })
├─ history = usePromise(() => runHistory(undefined, 200, signal), [], { abortable })
└─ <List isLoading searchText={query} onSearchTextChange={setQuery}
         searchBarPlaceholder="Look up a word…"
         searchBarAccessory={<LanguageDropdown value={to} onChange={setTo} />}>
     ├─ error → <BinaryNotFound/> | <List.EmptyView icon={Icon.Warning}/>   ← reuse lib/binary-not-found.tsx
     ├─ query === ""  → <List.Section title="Recent">  all history rows  </List.Section>
     └─ query !== ""
         ├─ <List.Section title="Dictionary">   candidates where match !== "fuzzy"
         ├─ <List.Section title="Did you mean?">candidates where match === "fuzzy"  (accessory `~{distance}`)
         ├─ <List.Section title="Recent">       ≤5 already-loaded history rows matching the query
         └─ candidates.length === 0 → <List.Item icon={Icon.Stars}
              title={`Ask the LLM for “${query}”`} />  → pushes DefineDetail
            (+ a footer row when data.notes is set, e.g. the `dict reindex` hint;
               its action copies the suggested command)
```

**New `src/lib/define-detail.tsx`** — `DefineDetail({ word, to, onRecorded })`:
`usePromise(() => runDefine(word, { to, smart: true }), [word, to], { onData: onRecorded })`,
renders a full-page `<Detail markdown navigationTitle={word} metadata={…}>`. Actions: Copy
Definition · Copy Word (⌘I) · Paste · Speak (`speakText`) · Open in Translate
(`launchCommand`). `isNoDictEntry(error)` renders a friendly "No entry for X" page, not a
red error. **Move** `renderDefine`/`renderMetadata` here from `src/define.tsx:99-144`
almost verbatim (`List.Item.Detail.Metadata` → `Detail.Metadata`) — don't rewrite them; the
old `define.tsx` keeps its own copies since it stays untouched.

**New `src/lib/history-detail.tsx`** — `HistoryDetail({ entry })`: renders the **stored**
record as full-page markdown. No CLI call, no new history row, instant — this is the
"整頁的 view + markdown render" payoff for long past translations. Actions: Copy Output ·
Copy Input (⌘I) · Paste · Look Up Again (→ push `DefineDetail`) · Speak.

**New `src/lib/language-dropdown.tsx`** — `useLanguages()` (→ `runLangs()` with the
hardcoded `LANGS` as fallback) + `<LanguageDropdown>` rendering all 35 languages, tooltip:
*"Definition language — used for the LLM fallback; dictionary hits are zh↔en."*

Key behaviours, each worth a code comment so nobody "fixes" them later:

- **⏎ is the only thing that writes history.** `dict search` opens no store; `DefineDetail`
  runs `define` *without* `--no-history`. The list physically cannot record.
- **The LLM is never called while typing.** `dict search` is pure local data (≤33 ms). The
  "fuzzy × LLM fallback" requirement is met by deferring the fallback to ⏎ — either on a
  fuzzy candidate or on the explicit `Ask the LLM for "…"` row. A 6.5 s LLM call per
  keystroke would be unusable.
- **Minimum query length 2** before firing `runDictSearch` — 1 char is both the 275 ms
  worst case and useless. 1 char keeps showing the Recent section.
- **⏎ on a candidate** pushes `DefineDetail`; `⌘⏎` = "Use This Word" → `setQuery` (search
  again). `src/lib/did-you-mean.tsx` is deliberately **not** reused: its `onPick` re-runs
  the *search*, whereas here ⏎ must *define*. Two different ⏎ meanings in one list is a
  confusing-picker trap. It stays untouched for `translate.tsx`/`define.tsx`.
- **History refresh after popping back**: Raycast keeps the parent mounted under a pushed
  view, so `historyQuery.revalidate()` from `DefineDetail`'s `onData` updates the list while
  the Detail is still open. Add a `⌘R` Reload action as a fallback (mirroring `history.tsx:72-77`).
- **Target vs. dictionary direction, stated not hidden**: when `res.engine === "dictionary"`
  and `res.target !== to`, the Detail metadata shows
  `Definition language: zh-CN (ECDICT is en→zh; “<to>” applies to the LLM fallback)`.
  No silent Simplified→Traditional conversion — there is no s2t converter in this repo (filed as a TODO).

## Step 7 — TS: the Translate prefill fix

`e87db06` changed **only `package.json`** (`1 file changed, 2 insertions(+), 2 deletions(-)`).
Three compounding causes, in likelihood order:

1. **A stored preference value survives a manifest-default change.** Raycast persists a
   per-command preference once materialized; a manifest `default` applies only when nothing
   is stored. If the command was ever opened while the default was `"selection"`,
   `"selection"` is stored and the new default is inert. *No code change fixes this* — check
   Raycast → Extensions → Translate → *Translate* → "Prefill input from" = **Nothing** first.
2. **A stale installed dev build** — `ray develop` leaves the built bundle registered. If
   `just raycast-dev` hasn't run since `e87db06`, Raycast is running the old manifest.
3. **The code default contradicts the manifest.**

Why it *looks* like clipboard prefill from a `"selection"` setting: `getSelectedText()` on
macOS falls back to a copy-and-read-pasteboard path in apps that don't expose a selection.

Code fix in `raycast/extension/src/translate.tsx`:

```diff
-interface Prefs { defaultTarget?: string; liveDebounceMs?: string; prefill?: string }
-  const prefs = getPreferenceValues<Prefs>();
+  // Generated manifest type: `prefill` is required there, so future drift between
+  // package.json's default and this file becomes a compile error.
+  const prefs = getPreferenceValues<Preferences.Translate>();
-    const mode = prefs.prefill ?? "selection";
+    const mode = prefs.prefill ?? "none"; // must match package.json's default
```

The hand-written `Prefs` interface (`:35-39`) is what let this drift: it makes `prefill`
optional, while the generated `Preferences.Translate` (`raycast-env.d.ts:30`) has it
required. `defaultTarget`/`liveDebounceMs` come from `ExtensionPreferences`, which
`Preferences.Translate` extends — no other call site changes.

**Considered and rejected:** renaming the key `prefill` → `prefillMode` to force Raycast to
re-apply the default. It works, but silently discards a deliberate opt-in for anyone who
chose selection/clipboard on purpose.

## Step 8 — `raycast/extension/package.json`

Add a fifth command; leave the `define` entry exactly as it is.

```json
{
  "name": "look-up-word",
  "title": "Look Up Word",
  "description": "Search dictionary headwords, browse history, and open a full definition page.",
  "mode": "view",
  "arguments": [
    {
      "name": "to", "type": "dropdown", "placeholder": "Definition language", "required": false,
      "data": [
        { "title": "English", "value": "en" },
        { "title": "Chinese (Traditional)", "value": "zh-TW" },
        { "title": "Chinese (Simplified)", "value": "zh-CN" },
        { "title": "Japanese", "value": "ja" },
        { "title": "Korean", "value": "ko" },
        { "title": "Spanish", "value": "es" },
        { "title": "French", "value": "fr" },
        { "title": "German", "value": "de" },
        { "title": "Italian", "value": "it" },
        { "title": "Portuguese", "value": "pt" }
      ]
    }
  ]
}
```

- The argument `data` list is necessarily a **hand-synced 10-language subset** — Raycast
  command arguments are static manifest data and can't read `lang list` at runtime. The
  *in-view* dropdown gets all 35 from `translate lang list --json`. Extend the existing
  comment at `src/lib/translate.ts:10-11` to say which list is static and which is runtime.
- No new extension-level preferences; debounce stays a code constant (180/300 ms) — a third
  debounce knob next to `liveDebounceMs` is over-configuration.
- `raycast-env.d.ts` is regenerated by `ray build` — do not hand-edit.

## Step 9 (optional) — `GET /v1/dict/search`

For transport parity, not needed by the extension: add `SearchDict` to the `Service`
interface (`internal/server/server.go:20-26`), route it at `:62-66` (not token-guarded —
dictionary data isn't personal), mirror `history`'s handler (empty `q` → **200 with `[]`**,
matching the CLI's exit-0 contract, deliberately not 422 like `/v1/define`), and add the
path + schemas to `openapi.json` plus the drift table in `openapi_test.go`.

**MCP is deliberately skipped**: an LLM host doesn't need a typeahead list; `define` already
returns `suggestions[]`. Record the rationale in the backlog doc.

---

## Tests

Repo style: stdlib `testing`, table-driven, hand-rolled fakes, no testify, co-located under
`internal/*`. `./cmd` stays untested. `just test` = `go test ./cmd/... ./internal/...`.

| File | Asserts |
|---|---|
| `internal/engine/dictsearch_test.go` (new) | `prefixUpper` bounds hold for ASCII/empty/CJK. `mergeCandidates`: exact first, prefix before fuzzy, dedupe keeps `prefix` over `fuzzy`, `limit` respected, fuzzy ordered (distance, frq-with-0-last, alpha). Empty query → **non-nil empty slice** (JSON `[]`, never `null`) and `err == nil`. `NewLocalDict{Dir: t.TempDir()}` → `Source:"none"`, non-empty `Notes`, **no error** (the exit-0 contract). Wordlist-only path via the new `Wordlist` field. |
| `internal/engine/ecdict_test.go` (new) | ~15-row SQLite fixture in `t.TempDir()` via `modernc.org/sqlite`. Ranking: exact first; `frq=0` last; lower `frq` first; multi-word after single-word; length then alpha tiebreak. `lookupMany` returns all requested, omits missing. Missing DB → error, not panic. |
| `internal/engine/cedict_test.go` (new) | 6-line CC-CEDICT fixture. Prefix search: exact key first, preview carries pinyin + first def, trad **and** simp keys resolve. `BuildCedictDB` round-trip agrees with `cedictIndex` on the same fixture — the guard that the SQLite path can't silently diverge. Missing file degrades to empty, no panic. |
| `internal/appcore/record_test.go` (new) | `Recordable`: suggestions-only → false; "not installed" note → false; dictionary hit with empty `Translation` → true; normal translation → true; `Truncated` → false; nil → false. |
| `internal/appcore/service_test.go` (extend) | `SearchDict` with a fake `Searcher`; nil searcher → `Source:"none"` + `Notes`, **not** an error. |
| `internal/server/{server,openapi}_test.go` (extend, only with Step 9) | `GET /v1/dict/search?q=test` → 200 + array; `q=` → **200 with `[]`**; `POST` → 405. Add `SearchDict` to `fakeService` and the two new schemas to the drift table. |

No TS tests — the extension has no test runner. `ray build` (strict) + `ray lint` are the gate.

## Manual verification

```sh
just check && just test && just build

# A — the new search surface
./translate dict search test --json | jq -r '.candidates[] | "\(.match)\t\(.word)\t\(.preview[:32])"'
./translate dict search "" --json; echo "exit=$?"                 # {"candidates":[]}, exit 0
./translate dict search zzzznotawor --json | jq '.candidates[0].match'   # "fuzzy", exit 0
time ./translate dict search a --limit 12 --json >/dev/null       # worst case, expect < 0.4s cold
./translate dict search 貓 --json | jq '{source, notes}'
./translate dict reindex && time ./translate dict search 貓 --json >/dev/null   # < 0.1s
time ./translate define 貓 --plain --json >/dev/null              # 1.7s → ~0.07s

# B — history is written on define, not on search
wc -l ~/.local/share/translate/history.jsonl
./translate define serendipity --to zh-TW --json | jq '{target, engine}'   # target no longer ""
tail -1 ~/.local/share/translate/history.jsonl | jq '{input,output,target_lang,engine}'
./translate define serendipity --no-history --json >/dev/null     # line count unchanged
./translate define helllo --plain --json >/dev/null                # suggestions-only → NOT recorded

# C — language surfaces
./translate lang list --json | jq 'length'                         # 35
./translate lang resolve chinees --json | jq '{code,score}'

just raycast-lint && just raycast-build && just raycast-dev
```

Then **from Raycast, not a terminal** (a terminal's PATH hides the launchd bug —
`pitfalls/raycast-launchd-path-translate-not-found.md`):

1. Open **Look Up Word** with no argument → recent history below an empty search bar.
2. Pick "Chinese (Traditional)" in the root-bar argument, then open → the in-view dropdown
   lists all 35 languages and starts on `zh-TW`.
3. Type `test` → candidate rows with definition subtitles, ranked
   `test, testing, testimony, testify, tester, …`; typing stays responsive.
4. Type `helllo` → a "Did you mean?" section. Type `zzzznotaword` → only the "Ask the LLM" row.
5. ⏎ on a row → full-page Detail. `⌘←` back → the new row is at the top of Recent.
6. ⏎ on a long past translation in Recent → full-page markdown, instantly, no new history row.
7. `wc -l history.jsonl` grew by exactly the number of ⏎s, not keystrokes. Speak adds no row.
8. Open **Translate** → the box is **empty**. If not, check the stored preference (cause 1)
   *before* touching code.
9. Open **Define** → unchanged, for side-by-side comparison.

## Docs & bookkeeping (per `AGENTS.md`)

**Update** — `README.md` (`dict search`, `dict reindex`, `lang list`; `define` now records
history and stamps a real `target`) · `docs/raycast-extension.md` (the new command under
"How ours is built"; a Gotchas bullet: *a Raycast preference `default` only applies when no
value is stored*) · `raycast/extension/README.md` · `raycast/extension/CHANGELOG.md`.

**Create** —
`pitfalls/raycast-still-prefills-clipboard-after-default-changed-to-nothing.md` (titled by
symptom; the three causes and the one-time reset) ·
`pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md` (`frq` is a *rank*, 0 = unknown,
so `ORDER BY frq DESC` is backwards; and `LIKE 'x%'` scans 770 k rows while a range
predicate uses `idx_word_lc`) · `backlog/look-up-word.md` (`Status: shipped`; the options
table: subcommand vs `define` flag, `cedict.db` vs lazy parse, new command vs rewriting
`define`, MCP yes/no).

**`TODO.md`** via `.agents/skills/project-knowledge-harness/scripts/add-todo.sh`, then
`todo-kanban.sh --validate-only`:

- `[P3/S] history --engine/--kind filter flags` — so front-ends stop filtering client-side.
- `[P3/M] Opportunistic translate serve fast path for the Raycast extension` (probe
  `127.0.0.1:4155/healthz` once per mount, route `dict search` through `/v1/dict/search`,
  fall back to spawning; never required, never prompted for).
- `[P3/S] Simplified→Traditional conversion for ECDICT glosses when the target is zh-TW`.
- `[P3/S] Covering index (word_lc, frq) in ecdict.db` — kills the 275 ms cold single-letter
  case; needs a `dict update ecdict` rebuild, so not worth forcing now.
- `[P3/S] Rank prefix candidates by lemma frequency via entries.exchange` — `tests`/`testifying`
  have `frq=0` and sort last; their `exchange` field points at the ranked lemma (`0:test`).

**Interaction with `backlog/suggestion-ranking.md`** (`TODO.md:46`, `[?/S]`) — do **not**
promote it to Done; this work shrinks it:

- Its open question *"does the bundled ECDICT populate `frq`/`bnc`?"* is answered: `frq`
  yes **and it is a rank**; `bnc` is **not imported at all** (`dictdata.go:153` reads only `rec[9]`).
- Option **B** (frequency re-rank) is implemented inside `dict search`'s fuzzy tier as a
  reusable pure comparator. What remains is wiring that comparator into
  `wordIndex.nearestN`'s consumers (`dict.go:91`, `localdict.go:115`) so `define`/`translate`/TUI/mcp
  `suggestions[]` benefit too. Re-tag the entry `[?/S]` → `[P2/S]` and note the helper exists.
- Option **C** (serialize `SuggestDistance`) is partly superseded: `candidates[].distance`
  gives Raycast the auto-correct signal without touching `TranslateResult`. Keep C open for
  the `define`/`translate` JSON paths only.

## Risks

| Risk | Mitigation |
|---|---|
| `frq DESC` inverted ranking — plausible-looking, silently wrong | Encoded in the ECDICT ranking test with `frq=0` fixtures + a pitfall doc |
| `LIKE` silently scanning 770 k rows (435 ms vs 12 ms) | Range predicate; `EXPLAIN QUERY PLAN` recorded in the pitfall doc and a code comment |
| Cold-cache 275 ms on a 1-char prefix | 2-char minimum in the extension; covering-index TODO filed |
| `cedict.db` absent on existing installs | `dict reindex` (no download) + graceful fallback + a `notes` hint surfaced in the list |
| **`define` recording history is a CLI behaviour change** | Minor version bump per `AGENTS.md`; `--no-history` documented; the empty-output guard lands as its own revertible commit |
| `define --json`'s `target` changes from `""` to a real code | `define.tsx:87`'s `data.target \|\| "en"` still works; call it out in the tag message |
| `dict search` must never exit 1 | Asserted in tests and by the `echo $?` checks above |
| `Candidates` serializing as `null` | Always initialize to `[]Candidate{}`; asserted in tests |
| Manifest/code drift recurring | Switch `translate.tsx` to the generated `Preferences.Translate`; delete the shadowing local interface |

**No `config.SchemaVersion` bump is required** — nothing here adds a `config.toml` field.
`--limit` is a flag, `cedict.db` is a derived path beside the existing `cedict_ts.u8`, the
debounce lives in the extension, and honoring the *existing* `[dict] wordlist` is a bug fix.
If a follow-up adds e.g. `[dict] search_limit`, bump `config.SchemaVersion` in
`internal/config/config.go` then.

## Commit sequence

1. `fix(engine): honor [dict] wordlist in the local dictionary engine`
2. `feat(engine): dictionary headword search (prefix + frequency + typo tiers)`
3. `feat(dict): cedict.db SQLite index — dict reindex, fast Chinese lookups`
4. `feat(cli): translate dict search — ranked headword candidates with previews`
5. `feat(cli): translate lang list --json; lang resolve honors --json`
6. `fix(history): never record empty-output results`
7. `feat(cli): define records history and stamps the real definition language`
8. `feat(serve): GET /v1/dict/search` *(optional)*
9. `fix(raycast): Translate honors the "Nothing" prefill default`
10. `feat(raycast): Look Up Word — candidate list, history, pushed detail, language picker`
11. `docs: look-up-word design record; pitfalls for ECDICT frq rank + Raycast preference defaults`
