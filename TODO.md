# TODO

Long-term backlog for translate. See AGENTS.md
for the maintenance workflow that agents should follow.

> **For agents**: when the user surfaces an idea explicitly **not** being
> implemented this session (signals: "maybe later", "nice to have",
> "工程量太大需要再評估", "先記下來"), add it here with priority + effort tags.
> Do not create new `ROADMAP.md` / `IDEAS.md` / `BACKLOG.md` files —
> `TODO.md` is the single backlog index. Long-form research goes in
> [`backlog/<slug>.md`](backlog/).

<!-- Use the exact section order: P1, P2, P3, P?, Done.
     The bundled scripts/todo-kanban.sh validator only inspects top-level
     `- [ ]` and `- ✅` items inside these sections. Prose paragraphs,
     blockquotes, indented sub-bullets, HTML comments, and `---` rules are
     ignored — feel free to add inline guidance like this without breaking
     machine readability. -->

## P1

Likely next batch — items you'd reach for if you sat down to work today.

## P2

Worth doing, no rush.

## P3

Someday / nice-to-have.

- [ ] **[S] Rotate `TAP_GITHUB_TOKEN` before 2026-11-26** — the fine-grained PAT that lets the release workflow push to `daviddwlee84/homebrew-tap` and `daviddwlee84/scoop-bucket` expires that day. The failure is quiet in the worst way: the GitHub Release is created *first*, so a lapsed token means the release looks successful while the tap formula and scoop manifest silently stay on the previous version. Regenerate the existing "Homebrew + Scoop" token (keeps name/repos/permissions), then `gh secret set TAP_GITHUB_TOKEN --repo daviddwlee84/translate`. Setup steps live in AGENTS.md § One-time setup.
- [ ] **[M] Table/wrapped-prose-aware bilingual mode** — the `--bilingual`/`-2` reading view interleaves a translation per blank-line-delimited prose block, which breaks on tabular output (`ls -l`, `kubectl get`, `git status`) and on hard-wrapped prose split by blank lines. Add column/table detection (skip or column-align) and soft-wrap coalescing; consider per-span parsing via the `charmbracelet/x/ansi` tokenizer already imported. See [backlog/bilingual-immersive-mode.md](backlog/bilingual-immersive-mode.md).
- [ ] **[M] lingua-go detection upgrade behind the `Detector` seam** — whatlanggo is light/fast but weak on short and mixed-script text; lingua-go is more accurate but heavy (embedded n-gram models → bigger binary, slower init). Swap behind the existing `internal/lang` interface only if short-text detection proves insufficient. Detection is mostly a fallback (Google returns the source, LLM returns `DetectedSource`), so keeping it light is defensible.
- [ ] **[S] MyMemory fallback engine** — trivial flat JSON (`responseData.translatedText`, `matches[]`); wire as an easy secondary free path after Google. Limits: 5k chars/day anon (50k with `&de=email`), 500 B max per `q`, no source auto-detect.
- [ ] **[M] Azure Translator engine behind the engine seam** — the only supported Microsoft path (needs an Azure subscription key). Keyless Bing scraping (`ttranslatev3` + transient `IG`/`IID`/token) is too fragile/ToS-risky for v1. Leave the engine interface ready; add when a key is available.
- [ ] **[S] history --engine/--kind filter flags** — Raycast Look up Word and History both pull `history --json` and filter client-side (there is no way to ask for "only dictionary lookups" or "only single-word inputs"). Add filter flags to `translate history` and the `/v1/history` query so front-ends stop re-implementing the same predicate. See `internal/store/jsonl.go`, whose Recent/Search already read the whole file per call.
- [ ] **[M] Opportunistic translate serve fast path for the Raycast extension** — Probe `127.0.0.1:4155/healthz` once per command mount and route `dict search` / `define` through the running server when it answers, falling back to spawning otherwise. Saves the ~55 ms process start per keystroke. Never required and never prompted for — the cost is a second code path to keep in sync with the CLI. See `backlog/look-up-word.md`.
- [ ] **[S] Simplified to Traditional conversion for ECDICT glosses** — ECDICT defines English words in Simplified Chinese, so a zh-TW user sees Simplified glosses on every dictionary hit (the result now honestly reports `target: zh-CN` rather than echoing --to). Add an s2t conversion when the resolved target is zh-TW/zh-Hant. There is no converter in the repo yet — needs a dependency or a mapping table.
- [ ] **[S] Covering (word_lc, frq) index in ecdict.db** — A one-letter prefix in `dict search` covers ~60k rows and costs 275 ms with a cold page cache (33 ms warm) because the ranking sort reads the table. A covering index over (word_lc, frq) would let the ORDER BY run off the index. Needs a `dict update ecdict` rebuild, so it is not worth forcing now; the extension avoids the case with a 2-character minimum query.
- [ ] **[S] Rank prefix candidates by lemma frequency via entries.exchange** — Inflected forms have `frq = 0` in ECDICT, so `tests` / `testifying` sort below rare-but-ranked words in `dict search`. Their lemma is in the `exchange` column (`test` -> `s:tests/d:tested/i:testing`), so an unranked row could inherit its lemma rank plus a penalty. See `internal/engine/dictsearch.go` and `pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md`.

## P?

Needs a spike before committing to a real priority. Tag as `[?/Effort]`.

- [ ] **[?/M] Bundle or prebuild the dictionary vs the 67 MB runtime `dict update`** — evaluate embedding a trimmed DB via `go:embed`, or shipping the built ECDICT sqlite + CC-CEDICT as release assets, so first run isn't a big download/build. Weigh binary-size blowup vs first-run friction. → [research](backlog/dict-bundling.md)
- [ ] **[?/L] Publish the Raycast extension to the store** — the local-dev extension (`raycast/extension`) works via `npm run dev`; publishing publicly hits the "avoid requiring manual installs" review guideline (it depends on the `translate` binary). Evaluate private org store vs public, icon/screenshots/CHANGELOG, and graceful binary-not-found onboarding. → [research](backlog/raycast-extension.md)
- [ ] **[?/S] Frequency-aware "did you mean" suggestion ranking** — partially shipped 2026-07-26: `translate dict search` re-ranks its typo candidates by (distance, ECDICT `frq` ascending with unranked last), so `recieve` → `receive` is 3rd not 6th. What remains is wiring the same comparator (`engine.sortByDistanceThenRank`) into `wordIndex.nearestN`'s other consumers (`internal/engine/dict.go`, `localdict.go`) so `define`/`translate`/TUI/MCP `suggestions[]` benefit too. Note `frq` is a **rank**, not a count, and `bnc` is not imported at all. → [research](backlog/suggestion-ranking.md)

## Done

- ✅ [2026-07-26] [P3/S] Drop the whole-request `http.Client{Timeout: 60s}` on the streaming path — long translations no longer truncate — max_tokens now scales with the input (flat 4096 cut a 10 KB document at 5368 chars) and the whole-request http.Client{Timeout: 60s} is gone, replaced by a per-request context deadline (default 10 min). Transport.ResponseHeaderTimeout was tried first and is wrong: copilot-proxy buffers Claude /v1/messages and sends no headers until generation ends, so it failed exactly the long inputs. Raycast scales its exec timeout to match. 20 KB now translates clean in 96s. See [pitfalls/llm-stream-truncation-silently-rendered-as-complete.md](pitfalls/llm-stream-truncation-silently-rendered-as-complete.md).

- ✅ [2026-08-27] [P?/L] Full release automation: prebuilt binaries, Homebrew, Scoop, and real tab completion — pushing a `vX.Y.Z` tag now does everything from one `ubuntu-latest` runner. New `.goreleaser.yaml` cross-compiles 6 targets (`CGO_ENABLED=0`; pure Go needs no docker/mac hardware), attaches archives + `checksums.txt` to a GitHub Release, and pushes the Scoop manifest to `daviddwlee84/scoop-bucket`. New `scripts/bump-formula.sh` renders `packaging/translate.rb.tmpl` and pushes the tap formula — GoReleaser can no longer do formulae (`brews:` deprecated in v2.10 → `homebrew_casks:`), and a cask *would* hit the Gatekeeper quarantine problem, but a **formula never does**: Homebrew only applies `com.apple.quarantine` in the cask code path, which retires the objection recorded in [backlog/homebrew-distribution.md](backlog/homebrew-distribution.md). Formula switched from build-from-source to prebuilt and now runs `generate_completions_from_executable(…, shell_parameter_format: :cobra)`, so brew/scoop finally ship completions instead of leaving it to the user's dotfiles. New `cmd/completion.go` gives `--to`/`--from`/`--provider`/`--model`/`--engine`/`--tier`/`--preset`/`--learn-mode`/`--bilingual-mode` real value completion off `lang.List()` and the config (previously every one of them fell back to *filename* completion), via a side-effect-free `config.LoadForRead` so a TAB press can't write a config file. Also first-ever CI (`vet`/`test`/`gofmt`/`goreleaser check`). Requires a `TAP_GITHUB_TOKEN` secret (fine-grained PAT, `contents: write` on the tap **and** bucket repos). → [backlog/release-binaries.md](backlog/release-binaries.md), [backlog/homebrew-distribution.md](backlog/homebrew-distribution.md).

- ✅ [2026-07-26] [P?/L] Raycast **Look up Word** — a Define Word-style dictionary picker, plus the CLI surface behind it — empty search bar lists recent history, typing shows ranked headword candidates with definition previews, ↵ pushes a full-page `Detail` (and *that* is what records the lookup). New `translate dict search <prefix> --json` (`internal/engine/dictsearch.go`: exact → frequency-ranked prefix → edit-distance tiers; exit 0 with `[]` on a miss; opens no history store, so typing cannot record) and `translate lang list --json`. New `cedict.db` SQLite index + `translate dict reindex` takes Chinese lookups from ~1.7 s to ~30 ms for *every* front-end. `define` now records history (`--no-history` to suppress) and stamps the language its glosses are actually in (`zh-CN`/`en`) instead of echoing `--to`; `appcore.Recordable` stops suggestion-only misses landing as empty-output rows. Also `GET /v1/dict/search` on `translate serve`, and a fix for Translate's prefill default drifting from the manifest. → [backlog/look-up-word.md](backlog/look-up-word.md), [pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md](pitfalls/ecdict-prefix-search-ranks-obscure-words-first.md), [pitfalls/raycast-still-prefills-clipboard-after-default-changed-to-nothing.md](pitfalls/raycast-still-prefills-clipboard-after-default-changed-to-nothing.md).

- ✅ [2026-07-22] [P?/L] Local HTTP API server (`translate serve`) + MCP (`translate mcp`) — shipped both as thin adapters over a new `internal/appcore.Service` that lifts the engine builders + warm engine + history glue out of `cmd`. `serve`: stdlib `net/http` JSON (`POST /v1/translate|define`, `GET /v1/history`), SSE `/v1/translate/stream`, embedded OpenAPI 3.1 + Swagger UI at `/docs`, loopback-only bind with bearer-token `/v1/history`, graceful shutdown, flock-hardened history. `mcp`: MCP over stdio via the official Go SDK v1.6.1 (translate/define/history tools). New pkgs `internal/{appcore,server,mcpserver}`; config `[server]` table = schema v2. → [backlog/api-server.md](backlog/api-server.md).

- ✅ [2026-07-22] [P3/S] `--stream` flag to force token streaming on non-TTY — piped consumers (`foo | translate`, the Raycast extension) can now force streaming; `cmd/root.go` ORs `--stream` into the TTY gate. Visible progressive output still depends on the provider (copilot-proxy buffers its claude `/v1/messages` responses; ollama streams). Part of the Raycast integration ([backlog/raycast-extension.md](backlog/raycast-extension.md), [docs/raycast-extension.md](docs/raycast-extension.md)).
- ✅ [2026-07-22] [P?/S] TUI result pane: collapse excess blank lines between paragraphs — multi-paragraph translations (esp. terminal-scrape input) showed 2–4 blank rows per paragraph break while `^y` copy stayed clean. Root cause was `lipgloss.Style.Render` block-padding a multi-line block's blank lines out to the widest line's width, which the `SoftWrap` viewport then re-wrapped into several blank rows. Fixed by styling per line (`styles.renderTranslation`) + Unicode-aware blank-run collapsing (`collapseBlankLines`, handles `U+3000`/NBSP/CRLF). End-to-end guard drives the real Model with long wrapping paragraphs. See [pitfalls/tui-lipgloss-block-padding-inflates-blank-lines-in-viewport.md](pitfalls/tui-lipgloss-block-padding-inflates-blank-lines-in-viewport.md).
- ✅ [2026-07-22] [P2/S] Align `just install` with `~/.local/bin` — the `install` recipe now installs to `~/.local/bin` (first on PATH, where `go install` lands), with a `DIR=` override, instead of `~/.dotfiles/bin`; both paths agree so a stale copy can't shadow. See [pitfalls/duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md](pitfalls/duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md).
- ✅ [2026-07-20] [P3/M] Homebrew tap distribution — `brew install daviddwlee84/tap/translate` is live (build-from-source personal tap). Landed the two prereqs in v0.3.1 (injectable `var version` in `cmd/version.go` + MIT `LICENSE`) and published `Formula/translate.rb` to `daviddwlee84/homebrew-tap`. Verified install/`brew test` on macos_intel; no Gatekeeper/quarantine (source build). → [backlog/homebrew-distribution.md](backlog/homebrew-distribution.md).
- ✅ [2026-07-20] [P2/M] Bilingual `--bilingual`/`-2` immersive pipe mode + ANSI-strip-on-input — piped colored input is now stripped (`bitext.Strip`) before reaching the LLM; `--bilingual` keeps each original block (color intact) and prints the translation beneath prose blocks, dimmed on a TTY, echoing indented command/code blocks untranslated. New pure `internal/bitext` pkg (Split/Render). Per-word recoloring (approaches A/B) rejected as over-engineering. → [backlog/bilingual-immersive-mode.md](backlog/bilingual-immersive-mode.md).
- ✅ [2026-07-09] [P?/L] Wire `translate` into chezmoi dotfiles as an auto-installed go tool — go_tools ansible role added to dotfiles (commit 306bfb0): go install → ~/.local/bin, mise-gated, + cat_go upgrade path. Pending chezmoi apply on normal cadence.

Recently shipped. When implementing an active item, in the same commit run:

```
scripts/promote-todo.sh --title "<substring>" --summary "<one-line shipped summary>"
```

This moves the entry here using the dated `Done` syntax and re-validates.

- ✅ [2026-07-10] [P1/M] Publish as a public repo + `go install` — renamed the module `translate` → `github.com/daviddwlee84/translate`, rewrote 22 internal imports, tagged `v0.1.0`, and confirmed `go install github.com/daviddwlee84/translate@latest` installs into `~/.local/bin` (GOBIN-pinned, XDG-clean, stable across mise Go upgrades).

<!-- Prune older entries into CHANGELOG.md once prior-year items appear here
     or this section grows past ~20 entries. -->
