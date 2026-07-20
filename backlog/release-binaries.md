# Ship prebuilt release binaries (goreleaser + GitHub Releases)

**Status**: P? — needs a spike
**Effort**: L
**Related**: `TODO.md` P? · `.goreleaser.yaml` (none yet) · chezmoi `.chezmoiexternal.toml.tmpl` in the dotfiles repo · [dict-bundling.md](dict-bundling.md)

## Context

2026-07, surfaced while making translate a "resident binary" in the chezmoi
dotfiles. Chosen install path for now is **`go install
github.com/daviddwlee84/translate@latest`** into `~/.local/bin` (GOBIN-pinned).
That requires a Go toolchain on every host. The dotfiles fleet is multi-host /
multi-arch (this host is `macos_intel`/amd64; others differ). Prebuilt release
binaries would let hosts **without** Go install via chezmoi `.chezmoiexternal`
(archive-file, templated per OS/arch) — the same mechanism the dotfiles already
use for oh-my-zsh etc.

## Investigation

- Binary is **pure Go, no cgo** (`modernc.org/sqlite` is pure Go), so
  `GOOS`/`GOARCH` cross-compilation is trivial — a single goreleaser run can emit
  darwin/{amd64,arm64} + linux/{amd64,arm64} archives.
- Current binary size ~22 MB (dictionary is NOT embedded — see dict-bundling).
- chezmoi external pattern (from the dotfiles `.chezmoiexternal.toml.tmpl`):
  `type = "archive-file"` with a URL templated on `{{ .chezmoi.os }}` /
  `{{ .chezmoi.arch }}`, `refreshPeriod`, extract the single binary to
  `~/.local/bin`.

## Options considered

| Option | Pros | Cons |
|---|---|---|
| A. `go install @version` (current) | zero release infra; always builds for the host | needs Go toolchain per host; version pin is a git tag |
| B. goreleaser + Releases + `.chezmoiexternal` archive | no toolchain on target; multi-arch; can attach dict DB | release pipeline to maintain; must tag + run CI on each version |
| C. Homebrew tap | `brew`-native upgrade story | tap repo to maintain; macOS/Linuxbrew only |

## Current blocker / open questions

- Is `go install` "good enough"? Go is present on all current hosts (mise), so B
  is only needed if a Go-less host joins the fleet.
- If we do B, should the dictionary DB ship as a release asset (ties this doc to
  [dict-bundling.md](dict-bundling.md))?

## Decision (if any)

`2026-07 deferred` — `go install` covers all current hosts. Revisit B if a
Go-less host needs translate, or once versioned releases are wanted for their own
sake.

## References

- goreleaser: https://goreleaser.com/
- chezmoi externals: https://www.chezmoi.io/reference/special-files-and-directories/chezmoiexternal-format/
- Homebrew tap (option C, now researched separately): [homebrew-distribution.md](homebrew-distribution.md) — build-from-source tap is viable independently of this goreleaser work.
