# Ship `translate` on every channel, then extract a CLI-release skill

## Context

You asked four things. Here are the verified answers, which are also the reason for this plan:

**1. 在 `dotfiles/` 之中是只有 brew install 嗎？** No — there are two channels already, split by OS:

| | macOS | Linux |
|---|---|---|
| Channel | Homebrew tap | `go install` |
| Where | `dot_config/homebrew/Brewfile.tmpl:10,26` (`tap "daviddwlee84/tap"` + `brew "daviddwlee84/tap/translate"`) | `dot_ansible/roles/go_tools/defaults/main.yml` (pinned `@v0.1.0`) |
| Upgrade | `brew upgrade` | `just upgrade-go` → `scripts/upgrade_tools.sh` `cat_go()` (which *skips* on Darwin by design) |

The split is deliberate: `go_tools/tasks/main.yml` excludes Darwin so `~/.local/bin` can't shadow the brew binary.

**2. `dotfiles-windows/` 會有安裝嗎？** It already does, and it is the weakest link. `.chezmoiscripts/run_onchange_after_10_packages.ps1.tmpl:619-658` installs Go via scoop *purely to build translate from source*, pinned at `v0.5.2`, with a multi-minute first build. `backlog/translate-windows-distribution.md` (P2) already specifies the fix — a scoop bucket — and blocks on work that must happen **in this repo**, not the dotfiles repo.

**3. 有 tab completion 嗎？會隨 brew 一起安裝嗎？** Completion **yes**, bundled-with-brew **no**.
- `translate completion bash|zsh|fish|powershell` exists purely as a cobra default. Zero customization: a repo-wide grep for `CompletionOptions|ValidArgsFunction|RegisterFlagCompletionFunc` returns **nothing**, and `./translate __complete --to ""` returns `:0` — i.e. `--to` falls back to *filename* completion despite `lang list --json` and `models --json` existing as purpose-built data sources.
- `/usr/local/Cellar/translate/0.5.0/` contains no `share/zsh/site-functions/_translate`. The only reason completion works on this machine is your **dotfiles** generating it: `scripts/generate_completions.sh` → `regen translate "completion zsh" "completion bash"` → `~/.zfunc/_translate`. On Windows the equivalent is `Import-CachedInit -Name 'translate'` in `dot_config/powershell/profile.d/10_tools.ps1`. **Anyone who installs from your tap without your dotfiles gets no completion at all.**

**4. 要 prebuilt binaries 是否需要維護 docker image 或實體機？** Neither — **GitHub Actions alone, on one `ubuntu-latest` runner.** The binary is pure Go with no cgo (`modernc.org/sqlite` is pure Go; `backlog/release-binaries.md:21-23` records a clean `GOOS=windows go build` on 2026-07-28), so `CGO_ENABLED=0` cross-compiles all six targets from a single Linux runner in one job. No docker images, no macOS/Windows runners, no physical hardware. The Go linker ad-hoc-signs darwin binaries even when cross-compiling, so they run on Apple Silicon.

Current state: **9 tags, 0 GitHub Releases, no `.github/` at all.** Every release step is manual (`AGENTS.md` "Releasing & versioning"), the tap formula is hand-bumped and pinned at v0.5.2 while `main` is 47 commits ahead.

**Outcome:** one tag push produces prebuilt binaries + checksums, auto-updates the Homebrew formula and a new Scoop bucket, and ships completions with the package on every OS. Then the proven recipe becomes a skill.

### On the Gatekeeper concern in `backlog/homebrew-distribution.md`

That backlog rejected prebuilt binaries because an unsigned binary triggers *"translate is damaged"*. **That reasoning is cask-specific and does not apply here.** Verified: `grep -rh quarantine /usr/local/Homebrew/Library/Homebrew/*.rb` returns only `require "cask/quarantine"` — Homebrew applies the quarantine attribute in the **cask** code path only. A *formula* that installs a prebuilt binary is never quarantined. So this plan keeps a **formula** (not a cask) and switches it to prebuilt. Update that backlog note rather than leaving the stale conclusion.

This also forces one design choice: GoReleaser **deprecated `brews:` in v2.10** in favour of `homebrew_casks:`, and casks are exactly what we must avoid. So GoReleaser publishes the Scoop manifest, and a small templated step publishes the formula.

---

## Phase 1 — `translate` repo: release automation

**New `.goreleaser.yaml`**

- `version: 2`, `project_name: translate`.
- `before.hooks`: generate `completions/translate.{bash,zsh,fish,ps1}` via `go run . completion <shell>`.
- One `builds` entry: `env: [CGO_ENABLED=0]`, `goos: [darwin, linux, windows]`, `goarch: [amd64, arm64]`, `ldflags: -s -w -X github.com/daviddwlee84/translate/cmd.version={{.Tag}}`.
  - That `-X` target is the existing `var version string` in `cmd/version.go` — keep it. Its comment already names goreleaser as an intended consumer, and `backlog/homebrew-distribution.md` records *why* it exists: `debug.ReadBuildInfo().Main.Version` is not a linker symbol, so `-X` on it is a silent no-op.
  - `{{.Tag}}` yields `v0.6.0`, which is what the formula's `assert_match "v#{version}"` test expects.
- `archives`: tar.gz, zip override for windows, `files: [LICENSE, README.md, completions/*]`.
- `checksum: name_template: checksums.txt`.
- `scoops:` (plural — the v2 key) pointing at `owner: daviddwlee84, name: scoop-bucket`, token `{{ .Env.TAP_GITHUB_TOKEN }}`.

Run `goreleaser check` and `goreleaser release --snapshot --clean` locally first; the `archives.formats` vs `format` key changed across v2 minors, so let the validator settle it rather than trusting the snippet.

**New `.github/workflows/release.yml`** — on `push: tags: ['v*.*.*']`, single ubuntu job:
1. `actions/checkout` with `fetch-depth: 0` (goreleaser needs full history).
2. `actions/setup-go` at the `go.mod` version.
3. `goreleaser/goreleaser-action@v6`, `args: release --clean`, env `GITHUB_TOKEN` + `TAP_GITHUB_TOKEN`.
4. Formula bump step (below).

**New `.github/workflows/ci.yml`** — there is no CI at all today. On push/PR: `go vet ./...` + `go test ./cmd/... ./internal/...`, mirroring the `just check` / `just test` recipes.

**New `packaging/translate.rb.tmpl` + `scripts/bump-formula.sh`** — the formula becomes a *generated* artifact of this repo, so the tap repo holds no hand-edited logic. The script reads `dist/checksums.txt`, substitutes `__VERSION__` and the four platform SHA256s, then commits and pushes to `daviddwlee84/homebrew-tap` with the PAT.

**Manual prerequisite (yours, one-time):** a fine-grained PAT with `contents: write` on `daviddwlee84/homebrew-tap` **and** `daviddwlee84/scoop-bucket`, stored as the `TAP_GITHUB_TOKEN` secret on `daviddwlee84/translate`. The default `GITHUB_TOKEN` cannot push cross-repo.

---

## Phase 2 — Homebrew formula: prebuilt + completions

Rewrite `Formula/translate.rb` in `daviddwlee84/homebrew-tap` from the template shape:

```ruby
on_macos do
  on_arm  { url ".../translate_<ver>_darwin_arm64.tar.gz";  sha256 "…" }
  on_intel{ url ".../translate_<ver>_darwin_amd64.tar.gz";  sha256 "…" }
end
on_linux do … end

head do
  url "https://github.com/daviddwlee84/translate.git", branch: "main"
  depends_on "go" => :build
end

def install
  if build.head?
    system "go", "build", *std_go_args(ldflags: "-s -w")
  else
    bin.install "translate"
  end
  generate_completions_from_executable(bin/"translate", shell_parameter_format: :cobra)
end
```

`shell_parameter_format: :cobra` is the exact idiom (verified against `homebrew-core/Formula/r/rosa.rb`, also a cobra Go CLI) — it runs `translate completion <shell>` and installs into brew's `site-functions`, which your `dot_zshrc.tmpl:106` `brew shellenv` FPATH already picks up. This closes the "no completion without your dotfiles" gap and drops `depends_on "go" => :build` from normal installs.

---

## Phase 3 — Windows: Scoop bucket

1. **Create `daviddwlee84/scoop-bucket`** (public, `bucket/` dir, README). GoReleaser pushes `bucket/translate.json` on every tag — that supersedes the `checkver`/`autoupdate` design in `backlog/translate-windows-distribution.md`, which was written for a hand-maintained manifest.
2. **`dotfiles-windows` `.chezmoiscripts/run_onchange_after_10_packages.ps1.tmpl`:**
   - Add the bucket in `Ensure-Scoop` alongside `main, extras, versions, nerd-fonts`.
   - Replace the whole `:619-658` go-install block with `Scoop-Install @('daviddwlee84/translate')`. `Scoop-Install` already handles bucket-qualified names (`($_ -split '/')[-1]`, as used for `extras/opencode-desktop`).
   - **Migration guard — do not skip this.** The old build put `translate.exe` in `~\.local\bin`, which is on PATH *ahead of* `~\scoop\shims`. Leaving it shadows the scoop install forever. Remove the stale exe when the scoop version is present. This is the Windows twin of `pitfalls/duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md` in this repo; write it up as a pitfall in `dotfiles-windows/pitfalls/`.
   - Drop the now-unneeded `Scoop-Install @('go')` line *only if* nothing else in the block needs it (herdr-plus and specstory both use `go build` — check before removing).
3. `dot_config/powershell/profile.d/10_tools.ps1` needs **no change**: `Import-CachedInit` resolves via `Get-Command` and invalidates on binary mtime, so it picks up the scoop shim and regenerates on every `scoop update`.
4. `justfile:45-47` `upgrade-translate` → `scoop update translate`.
5. Docs: `docs/translate.md` + `docs/translate.zh-TW.md`, `docs/tools.md:216` (it currently frames translate as "the one tool built from source" — no longer true), `TODO.md:31` → done, `backlog/translate-windows-distribution.md` → shipped.
6. Add `tests/Translate.Tests.ps1` modelled on the existing `tests/PackageSources.Tests.ps1` / `tests/Pueue.Tests.ps1`.

---

## Phase 4 — `dotfiles` (macOS/Linux) touch-ups

- `dot_ansible/roles/go_tools/defaults/main.yml`: bump the `@v0.1.0` pin (a fresh Linux box currently installs v0.1.0 until `just upgrade-go` runs).
- Keep the `regen translate …` row in `scripts/generate_completions.sh`. On macOS it is now redundant with the formula's completions but harmless (`~/.zfunc` precedes brew's `site-functions` in fpath); on Linux `go install` still ships nothing, so the row is the only source there.
- Fix the doc drift the exploration surfaced: `docs/zsh/zsh-completions.md:69-86` § A table omits `translate`, and line 205 still says "the 14 self-generating tools" (it is 15). Same stale list in `.chezmoiscripts/global/run_after_50_generate_completions.sh.tmpl:12-14` and `justfile:409-410`.
- Per `dotfiles/CLAUDE.md:16`, any channel change needs a `docs/this_repo/tool-managers.md` § Tool index row update.

---

## Phase 5 — Custom cobra completions (the real UX win)

New `cmd/completion.go`, wired from `NewRootCmd()` in `cmd/root.go:73-98` after the flags are declared:

| Flag | Source |
|---|---|
| `--to`, `--from`, `--pair-with`, `--speak-lang` | `lang.List()` → `code\tName` pairs (cobra renders the tab as a description) |
| `--provider` | `config.Load()` → `cfg.Providers[].Name` |
| `--model` | provider's models, honouring an already-set `--provider` |
| `--engine` | `auto`, `smartauto`, `google`, `dict` + provider names |
| `--tier` | `default`, `fast`, `max` |
| `--preset` | `concise`, `contextual`, `dictionary` |
| `--learn-mode` | `auto`, `teach`, `correct`, `explain` |
| `--bilingual-mode` | `doc`, `blocks` |

Also set `ValidArgsFunction: cobra.NoFileCompletions` on the root — positional args are free text, so offering filenames is noise. Note `--from` should additionally offer `auto`.

Add `cmd/completion_test.go` driving the `__complete` hidden command, asserting `--to ""` returns language codes rather than `:0`.

Document `translate completion <shell>` in README's Install section — it is currently undocumented for end users everywhere.

---

## Phase 6 — Extract the skill

New `skills/local/cli-release-distribution/` in `/Users/david/Documents/Program/agent-skills`, following the `pueue-job-queue` / `raycast-extension-dev` body shape (framing → When to use / When NOT to use → Authoritative sources → Mental model table → Workflows → Available scripts with **Flags**/**Exit codes** → Reference files with "Read *when* …" triggers → See also → Gotchas).

**The framing that earns it:** most repology rows are *not* yours. Tier the channels by who acts:

- **Tier 1 — you control, automate on tag:** GitHub Releases + checksums, your own Homebrew tap, your own Scoop bucket, `go install` / `crates.io`.
- **Tier 2 — you submit, they review:** winget-pkgs PR, AUR, nixpkgs, homebrew-core (needs notability thresholds).
- **Tier 3 — they come to you:** Alpine, Debian, Fedora, Void, MacPorts, Guix, pkgsrc… pueue has ~20 rows at **6.3k stars**, and its own `.github/workflows/package-binary.yml` only does Tier 1 — a tag-triggered cross-compile matrix uploading raw binaries. Everything else is downstream volunteers. Do not plan for Tier 3; make Tier 1 excellent so Tier 3 is easy for them.

Layout:
- `references/`: `goreleaser-config.md`, `homebrew-tap.md`, `scoop-bucket.md`, `shell-completions.md`, `channel-tiers.md`.
- `scripts/`: `check-release-readiness.sh` (tags vs Releases, formula/manifest version drift, checksums present, completions actually installed), `bump-formula.sh` (generalized from Phase 1).
- `assets/`: `.goreleaser.yaml.template`, `release.yml.template`, `formula.rb.template`, `scoop.json.template`.

**Gotchas section** — every one of these was paid for in this session or is recorded in this repo's backlog:
- Cask quarantine ≠ formula. Casks are quarantined and break unsigned binaries; formulae are not. Prebuilt-in-a-formula is fine.
- GoReleaser `brews:` is deprecated (v2.10) → `homebrew_casks:`, which is the wrong tool. Template + push the formula yourself.
- `debug.ReadBuildInfo().Main.Version` is not a linker symbol — `-X` on it silently no-ops. Inject into your own `var version`.
- Cross-repo publishing needs a PAT; `GITHUB_TOKEN` is scoped to the one repo.
- Pure-Go (`CGO_ENABLED=0`) means one Linux runner covers all targets — no docker, no mac hardware. cgo is the thing that would force a matrix.
- Package managers do **not** install completions for you; the formula/manifest must generate them. `go install` never will — that's a permanent gap the dotfiles side has to cover.
- Switching a tool from `go install` to a package manager leaves a shadowing binary on PATH.
- Cobra gives you `completion` free but zero flag-value completion; wire `RegisterFlagCompletionFunc` and debug with the hidden `__complete`.

Then: append `./local/cli-release-distribution` to the right plugin's `skills[]` in `skills/.claude-plugin/marketplace.json`, run `make marketplace`, pass `make validate` (`lint-frontmatter.sh`, `validate-marketplace.sh`) and `scripts/lint-skill.sh`. Quote the `description` if it contains `:`. **Do not** create `.agents/skills` / `.claude/skills` discovery symlinks — per `agent-skills/CLAUDE.md`, skills authored for downstream use only would just pollute in-repo sessions.

---

## Verification

1. `goreleaser check` && `goreleaser release --snapshot --clean` → inspect `dist/` for 6 archives + `checksums.txt`, and confirm `completions/` is inside each archive.
2. `go test ./cmd/... ./internal/...` including the new completion test; `./translate __complete --to ""` must list language codes, not `:0`.
3. Tag `v0.6.0` and push → Actions produces the Release, the tap commit, and the bucket commit. `gh release view v0.6.0`.
4. macOS: `brew uninstall translate && brew update && brew install daviddwlee84/tap/translate`; then `ls $(brew --prefix)/share/zsh/site-functions/_translate`, `exec zsh`, `translate --to <TAB>` shows languages. Confirm no Go toolchain was pulled.
5. `brew audit --strict --online daviddwlee84/tap/translate`.
6. Windows: `chezmoi apply`, confirm `scoop list translate`, `where.exe translate` resolves to the scoop shim (**and only that**), new pwsh session → `translate --to <TAB>`. Run `Invoke-Pester tests/Translate.Tests.ps1`.
7. Linux: `just upgrade-go` then `just completions-refresh`, check `~/.zfunc/_translate` regenerated.
8. Skill: `make validate` + `scripts/lint-skill.sh skills/local/cli-release-distribution`.

## Non-goals

- **`internal/xdgpath` staying `~/.config` on Windows.** It ignores `%APPDATA%` by deliberate design (the package comment explains the `~/.config` convention), your Windows dotfiles set the `XDG_*` vars anyway, and changing it would strand existing Windows configs. Flagged, not touched.
- winget / AUR / nixpkgs submissions (Tier 2) — documented in the skill, not executed.
- Man pages, and the 67 MB dict DB as a release asset (`backlog/dict-bundling.md`) — separate decisions.
- Signing/notarizing macOS binaries — unnecessary for a formula.
