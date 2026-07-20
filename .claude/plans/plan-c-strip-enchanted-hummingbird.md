# Plan: switch dotfiles `translate` install go → Homebrew (hybrid: brew on macOS, go on Linux)

## Context

`translate` now ships a public Homebrew tap (`daviddwlee84/tap`, verified: `brew install daviddwlee84/tap/translate` builds from source, `v0.3.1`, `brew test` green). The user wants the **chezmoi dotfiles** to install it via brew instead of `go install`.

The dotfiles currently install it via the `go_tools` ansible role (`go install …@v0.1.0` → `~/.local/bin/translate`), which sits **earlier on PATH** than brew's `/usr/local/bin` (so `brew` already warns the brew copy is shadowed). Straight "brew everywhere" would **regress Linux** (Homebrew on Linux is gated by `installBrewApps`+arch+non-noRoot; `Brewfile.linux.tmpl` is empty), whereas `go install` works on every host via mise Go. The repo already handles such tools per-OS (starship/atuin/pueue = "macOS brew, Linux apt/cargo"), so the chosen approach is a **hybrid**: macOS → Homebrew tap, Linux → keep `go install`.

Outcome: on macOS `translate` comes from brew (single authoritative copy, `brew upgrade`); on Linux it stays on `go install` (`just upgrade-go`); no host loses translate; CLAUDE.md cross-file mirrors stay consistent.

This plan modifies the **chezmoi repo** (`$(chezmoi source-path)` = `/Users/david/.local/share/chezmoi`), not the translate repo. It is a separate commit there.

## Key facts established

- `trust_bundle_taps()` in `.chezmoiscripts/global/run_onchange_after_30_brew_bundle.sh.tmpl` auto-runs `brew trust` on every `tap "…"` in a Brewfile before `brew bundle` → the Homebrew-6 untrusted-tap gate (see `pitfalls/homebrew-6-refuses-untrusted-tap-formula.md`) is handled just by adding the `tap` line.
- `Brewfile.tmpl` is the shared CLI-formula Brewfile (currently all `installBrewApps`-gated); the run-script bundles it on macOS and is hash-gated (edits trigger re-run).
- `translate` is the **only** entry in `go_tools/defaults/main.yml`.
- `just upgrade-go` → `scripts/upgrade_tools.sh::cat_go()` parses that **static** defaults YAML and `go install …@latest` **regardless of OS** — so it must also be OS-gated, else it re-installs the go copy on macOS and re-shadows brew.

## Changes (chezmoi source)

### 1. macOS install — `dot_config/homebrew/Brewfile.tmpl`
Add, **darwin-gated but NOT `installBrewApps`-gated** (it's a CLI wanted on every mac):
- Taps section: `{{ if eq .chezmoi.os "darwin" -}}tap "daviddwlee84/tap"{{ end -}}`
- Formulas section: `{{ if eq .chezmoi.os "darwin" -}}brew "daviddwlee84/tap/translate"  # terminal translator; Linux uses go install (go_tools){{ end -}}`

### 2. Linux install — `dot_ansible/roles/go_tools/`
- `tasks/main.yml`: add `and ansible_facts['os_family'] != 'Darwin'` to the "Install Go CLI tools" task `when:`, with a comment that macOS installs these via Homebrew. Reframes `go_tools` as "Linux go CLI tools" (translate is its only entry).
- `defaults/main.yml`: keep the `translate` entry (source of truth for the Linux go install + cat_go); update its comment to note macOS→brew. Leave the `@v0.1.0` floor (per the role's "don't bump the pin for upgrades" rule).

### 3. Upgrade path — `scripts/upgrade_tools.sh` `cat_go()`
Add an early **macOS skip** (`return $SKIP_RC` when `os_family`/`uname` is Darwin) with a comment: on macOS translate upgrades via `brew upgrade`; `go_tools` is Linux-only. Prevents `just upgrade-go` from re-creating `~/.local/bin/translate`.

### 4. Doc mirrors (CLAUDE.md cross-file rule, line 15 — mechanism switch)
- `docs/this_repo/tool-managers.md`:
  - § Per-manager summary **go** row (~L38): note translate is macOS→brew / Linux→go.
  - The `go_tools` formula/tool list (~L597): annotate translate as Linux-only.
  - The A–Z / routing row for **translate** (~L1163): `brew (macOS) / go install (Linux)`.
  - § Homebrew formulae catalog (§2.1/2.2): add the `daviddwlee84/tap` translate formula.
- `docs/this_repo/upgrades.md`: the `go` row (~L53) — note macOS is excluded (brew); mention translate upgrades via `brew upgrade` on macOS.
- `README.md` (dotfiles): the `just upgrade-go` example (~L373) currently cites translate — update wording since translate is now Linux-only for go.
- `site/this_repo/…` is **not** git-tracked (build artifact) — do not hand-edit; it regenerates from docs.

## Runtime steps (after source changes; per host)

- **This mac**: `chezmoi diff` → review → `chezmoi apply` (deploys Brewfile → run-script trusts `daviddwlee84/tap` + `brew bundle` installs translate). Then remove the stale go copy: `rm ~/.local/bin/translate` (install-only tooling won't remove it). Verify `which -a translate` → `/usr/local/bin/translate`, `translate --version` → `v0.3.1`.
- **Fleet**: this is a Brewfile change → needs **full `fleet-apply`** (not `fleet-apply-file`, which skips run-scripts); `just upgrade-*` per host as usual. Linux hosts keep go install (unchanged behavior).

## Verification

1. `chezmoi diff` shows exactly: Brewfile.tmpl (+tap/+brew), go_tools task `when` + defaults comment, `cat_go` macOS skip, the doc mirrors.
2. Dry check the formula set: `brew bundle check --file ~/.config/homebrew/Brewfile` (after apply) → "dependencies are satisfied".
3. `brew tap-info daviddwlee84/tap` shows **not** `Untrusted` after apply (run-script trusted it).
4. After `rm ~/.local/bin/translate`: `command -v translate` → brew path; `translate --version` → `v0.3.1`; a quick `echo hola | translate --to en --engine google` works.
5. `just upgrade-go` on macOS → SKIPPED (no re-shadow); on a Linux host → still `go install`s translate.
6. Dotfiles lint/format gate if present (`just check` / pre-commit) green. Commit in the chezmoi repo (`feat`/`chore` per its convention), honoring the same-commit mirror rule.

## Notes / trade-offs

- This touches 3 OS-gates (ansible task, `cat_go`, Brewfile) + doc mirrors for one tool — more moving parts than a one-line change, but it's the idiomatic per-OS split the repo already uses and avoids the Linux regression.
- `go_tools` effectively becomes "Linux go tools"; if a future go tool needs macOS too, revisit the blanket Darwin gate.
