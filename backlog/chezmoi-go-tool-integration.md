# Wire `translate` into chezmoi dotfiles as an auto-installed go tool

**Status**: shipped (2026-07, dotfiles commit 306bfb0) — pending `chezmoi apply`
**Effort**: L (idiomatic route, chosen)
**Related**: `TODO.md` P? · dotfiles repo at `chezmoi source-path` (`~/.local/share/chezmoi`) · [release-binaries.md](release-binaries.md) · pitfalls/gobin-points-at-mise-toolchain-dir.md

## Context

2026-07. `translate` is now `go install`-able (`go install
github.com/daviddwlee84/translate@latest` → `~/.local/bin`, GOBIN-pinned). Goal:
make it install automatically on `chezmoi apply` / on every host, the "resident
binary" ask. A subagent researched how the chezmoi+ansible dotfiles repo wants a
`go install` tool added. **Key finding: there is NO existing go-install mechanism
in the repo** — introducing one is a new *mechanism*, which the repo's CLAUDE.md
treats as a heavier change than adding a tool to an existing mechanism.

## Investigation (full research preserved)

**Blessed install dir:** `~/.local/bin` (already on PATH via
`dot_config/shell/00_exports.sh.tmpl`; where cargo/uv/npm/dotnet fallbacks land).
`GOBIN` is set nowhere in the repo → introduce `GOBIN=$HOME/.local/bin` at install
time. `GOPATH=~/go` is set (`02_legacy_tools.sh`), so without GOBIN a tool would
land in `~/go/bin` (also on PATH but not "blessed").

**Idiomatic home = a new dedicated ansible role `go_tools`**, mirroring
`rust_cargo_tools` (every language package manager here is a `*_tools` role with a
`defaults/main.yml` list consumed by a `tasks/main.yml` loop). chezmoi
`run_onchange` scripts are reserved for orchestration/compile steps, not
package-manager tool lists — so a standalone go-install onchange script is
*against* convention (though it's the lighter option if only ever one tool).

Go-runtime resolution idiom already in repo (`devtools/tasks/main.yml:4385-4404`,
herdr build): `gobin="$(mise which go 2>/dev/null)" || exit 0`; role must **no-op
gracefully** when Go absent (Go is gated on `installExtraRuntimes` in
`dot_config/mise/config.toml.tmpl`). Use `creates: ~/.local/bin/translate` for
install-only idempotency (mirror `rust_cargo_tools/tasks/main.yml:82-83`). No
sudo (writes to `~/.local/bin`).

## Options considered

| Option | Pros | Cons |
|---|---|---|
| A. New `go_tools` ansible role (idiomatic) | scales to more go tools; clean upgrade category; matches cargo/uv/npm exactly | ~8 wiring points + 4 mandated doc updates; touches the strict repo broadly |
| B. Add `translate` to existing `devtools` role | lighter; devtools already resolves mise Go | doesn't scale; no clean upgrade category; still needs a tool-managers row |
| C. Standalone `run_onchange_after_35_go_install.sh.tmpl` | fewest files; self-contained | against the repo's package-manager convention; still a new decision-tree branch |
| D. Do nothing in dotfiles — keep `go install` manual | zero repo churn; primary goal already met | not auto-installed on new hosts / apply |

## Idiomatic-route (A) checklist — 8 steps

1. `dot_ansible/roles/go_tools/defaults/main.yml` — `go_tools: []`, seed
   `{name: github.com/daviddwlee84/translate@v0.1.0, binary: translate}`.
2. `dot_ansible/roles/go_tools/tasks/main.yml` — resolve go via `mise which go`;
   `go install {{ item.name }}` loop with `environment: {GOBIN: "{{ HOME }}/.local/bin", PATH: <gobindir>:...}`,
   `args.creates: "{{ HOME }}/.local/bin/{{ item.binary }}"`; graceful no-op if Go absent.
3. Register role in `dot_ansible/playbooks/macos.yml` + `linux.yml` (`tags: [go_tools]`).
4. Add tasks+defaults `sha256sum` hash lines to
   `.chezmoiscripts/global/run_onchange_after_20_ansible_roles.sh.tmpl`.
5. `scripts/upgrade_tools.sh`: add `cat_go()` (model on `cat_dotnet` ~L502 — parse
   the defaults list, `go install …@latest` per entry) + register in
   `ALL_CATEGORIES` (L90), arg whitelist (L140), dispatch case (~L902).
6. `justfile`: add `upgrade-go:` recipe (mirror `upgrade-cargo` L442).
7. Docs same commit (CLAUDE.md L15): `tool-managers.md` new A–Z row + new
   Per-manager subsection + new Decision-tree branch; `upgrades.md` category-matrix
   row + run-order + Extending; `README.md` manager listing.
8. Verify: `ansible-playbook … --tags go_tools`; `uv run mkdocs build --strict` if
   doc nav changed. Conditional: if `translate` ships `completion`, add a `regen`
   row to `scripts/generate_completions.sh` (CLAUDE.md L26).

CLAUDE.md hard invariant: **install vs upgrade is split** — install pins
`@v0.1.0` (via `creates:` idempotency), version moves forward only in `cat_go`.
Do NOT install `@latest` implicitly.

## Current blocker / open questions

- **Scope decision (user's call):** idiomatic role (A, ~L) vs lightweight
  standalone script (C, ~S) vs manual-only (D). A is "correct" per repo
  conventions but is a broad change to a strict repo for a single tool.
- Does `translate` ship shell completions? (cobra can generate them — it has a
  `completion` command.) If wired into the install set, CLAUDE.md L26 fires.

## Decision (if any)

`2026-07` — chose the idiomatic route (A). Shipped as the `go_tools` ansible
role in the dotfiles repo (commit 306bfb0): all 8 checklist steps done + the
completion regen row. Verified via ansible `--syntax-check`, template render
(go_tools in TAGS), and the dotfiles pre-commit suite. Not yet `chezmoi
apply`'d — will land on the next normal apply cadence (no-op on this host,
translate already in `~/.local/bin`).

## References

- Research: subagent report, 2026-07 (this session).
- dotfiles CLAUDE.md cross-file rules; `docs/this_repo/tool-managers.md`,
  `docs/this_repo/upgrades.md`; `dot_ansible/roles/rust_cargo_tools/`.
