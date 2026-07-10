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

- [ ] **[S] Align `just install` with `go install` / `~/.local/bin`** — the `install` recipe copies the binary to `~/.dotfiles/bin` (PATH position 11), which **shadows** the `go install` location `~/.local/bin` (position 13); a stale `just install` copy would win silently. Point the recipe at `GOBIN=$HOME/.local/bin go install .` (or drop it) so both paths agree. See [pitfalls/duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md](pitfalls/duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md).

## P3

Someday / nice-to-have.

- [ ] **[M] lingua-go detection upgrade behind the `Detector` seam** — whatlanggo is light/fast but weak on short and mixed-script text; lingua-go is more accurate but heavy (embedded n-gram models → bigger binary, slower init). Swap behind the existing `internal/lang` interface only if short-text detection proves insufficient. Detection is mostly a fallback (Google returns the source, LLM returns `DetectedSource`), so keeping it light is defensible.
- [ ] **[S] MyMemory fallback engine** — trivial flat JSON (`responseData.translatedText`, `matches[]`); wire as an easy secondary free path after Google. Limits: 5k chars/day anon (50k with `&de=email`), 500 B max per `q`, no source auto-detect.
- [ ] **[M] Azure Translator engine behind the engine seam** — the only supported Microsoft path (needs an Azure subscription key). Keyless Bing scraping (`ttranslatev3` + transient `IG`/`IID`/token) is too fragile/ToS-risky for v1. Leave the engine interface ready; add when a key is available.
- [ ] **[S] Drop the whole-request `http.Client{Timeout: 60s}` on the streaming path** — in `internal/engine/llm.go`, `NewLLM` sets a client-wide `Timeout` that Go applies to the *entire* request including reading the streamed body, so a long translation is cut at 60s mid-stream. Now surfaced as `⚠ truncated` (see [pitfalls/llm-stream-truncation-silently-rendered-as-complete.md](pitfalls/llm-stream-truncation-silently-rendered-as-complete.md)) instead of silently accepted, but the long output still can't finish. Fix by relying on `ctx` deadlines for the streaming `Do()` (keep the short timeout only for `Available`/`Models` probes).

## P?

Needs a spike before committing to a real priority. Tag as `[?/Effort]`.

- [ ] **[?/L] Ship prebuilt release binaries (goreleaser + GitHub Releases)** — cross-compile per OS/arch (pure-Go, no cgo — trivial) so hosts without a Go toolchain install via chezmoi `.chezmoiexternal` with a templated `{{ .chezmoi.os }}/{{ .chezmoi.arch }}` URL instead of `go install`. Also unlocks shipping the dictionary DB as a release asset. → [research](backlog/release-binaries.md)
- [ ] **[?/M] Bundle or prebuild the dictionary vs the 67 MB runtime `dict update`** — evaluate embedding a trimmed DB via `go:embed`, or shipping the built ECDICT sqlite + CC-CEDICT as release assets, so first run isn't a big download/build. Weigh binary-size blowup vs first-run friction. → [research](backlog/dict-bundling.md)

## Done

- ✅ [2026-07-09] [P?/L] Wire `translate` into chezmoi dotfiles as an auto-installed go tool — go_tools ansible role added to dotfiles (commit 306bfb0): go install → ~/.local/bin, mise-gated, + cat_go upgrade path. Pending chezmoi apply on normal cadence.

Recently shipped. When implementing an active item, in the same commit run:

```
scripts/promote-todo.sh --title "<substring>" --summary "<one-line shipped summary>"
```

This moves the entry here using the dated `Done` syntax and re-validates.

- ✅ [2026-07-10] [P1/M] Publish as a public repo + `go install` — renamed the module `translate` → `github.com/daviddwlee84/translate`, rewrote 22 internal imports, tagged `v0.1.0`, and confirmed `go install github.com/daviddwlee84/translate@latest` installs into `~/.local/bin` (GOBIN-pinned, XDG-clean, stable across mise Go upgrades).

<!-- Prune older entries into CHANGELOG.md once prior-year items appear here
     or this section grows past ~20 entries. -->
