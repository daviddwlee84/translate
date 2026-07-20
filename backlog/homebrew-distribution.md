# Homebrew distribution — installability via `brew`

**Status**: researched (2026-07-20), not yet implemented — spike done
**Effort**: M
**Related**: `TODO.md` P3 · `cmd/version.go` (version prerequisite) · [release-binaries.md](release-binaries.md) (goreleaser/prebuilt path) · dotfiles `go_tools` role + `.chezmoiexternal`

## Context

`translate` installs today via `go install github.com/daviddwlee84/translate@latest`
(into `~/.local/bin`, also driven by the dotfiles `go_tools` ansible role). The ask:
also make it `brew install`-able, and document the whole path as the basis for
shipping.

**Verdict: feasible and low-effort** — but two in-repo prerequisites must land first
(both verified empirically on this repo's go1.26.4), and the "modern" prebuilt-cask
path has a new macOS Gatekeeper trap that makes the *simplest* path also the *best*
one for a single-maintainer CLI.

## Prerequisites (do these first — they block every option)

### 1. Add a linker-injectable `version` var (REQUIRED)

`cmd/version.go` derives the version **only** from `debug.ReadBuildInfo()`
(`info.Main.Version`). That value is synthesized by the Go toolchain **only** for
`go install path@version`; it is **not** a linker symbol. Empirically confirmed here:

```
# extract the tag tarball (exactly what Homebrew builds from) and build:
git archive v0.3.0 | tar -x -C /tmp/t && cd /tmp/t && go build -o tb .
./tb --version                     # → "translate version (devel)"
# and -X is a SILENT no-op today, because there is no symbol to set:
go build -ldflags "-X github.com/daviddwlee84/translate/cmd.version=v0.3.0" -o tb2 .
./tb2 --version                    # → STILL "translate version (devel)"
```

So any build that is **not** `go install …@version` (Homebrew tarball build,
goreleaser, plain `go build`) reports `(devel)`. Fix — add a package-level var and
prefer it, keeping the existing ReadBuildInfo logic as the fallback:

```go
// cmd/version.go  (package cmd)
// version is injected at package/release time via
//   -ldflags "-X github.com/daviddwlee84/translate/cmd.version=v0.3.0"
// Empty for a plain `go build`/`go install`, where we fall back to build info.
var version string

func buildVersion() string {
	if version != "" {
		return version // Homebrew/goreleaser build → the injected tag
	}
	info, ok := debug.ReadBuildInfo()
	// … existing logic unchanged: info.Main.Version + vcs.revision/time/modified …
}
```

Note the `-X` path is the **full package path** `github.com/daviddwlee84/translate/cmd.version`
(the var lives in package `cmd`, not `main`); a wrong path fails silently.
`go install …@vX.Y.Z` keeps working unchanged (var empty → ReadBuildInfo → correct).

### 2. Add a `LICENSE` file (recommended)

The repo has **no** `LICENSE` today (README only cites ECDICT's MIT for the bundled
*dictionary data*). A personal tap will install without one, but `brew audit --strict`
complains and homebrew-core hard-requires it. Add a real license (e.g. MIT) so the
formula's `license "MIT"` stanza is truthful.

## Options

| Option | What | For this project |
|---|---|---|
| **A. Personal build-from-source tap** | a `homebrew-tap` repo with `Formula/translate.rb` → tag tarball + sha256, `depends_on "go" => :build`, `go build` | **Recommended now.** Lowest infra (no CI/PAT), and the source-built binary carries **no macOS quarantine bit** (see B's trap). |
| **B. goreleaser + prebuilt archives + auto-published cask** | cross-compiled binaries on GitHub Releases; goreleaser pushes a cask to the tap on each tag | **Defer.** Adds CI + a cross-repo PAT + the quarantine hook. Only worth it alongside the *other* driver (chezmoi Go-less hosts / ship dict DB) — see [release-binaries.md](release-binaries.md). |
| **C. Submit to homebrew-core** | the central tap; `brew install translate` | **Skip.** Notability bar (stars/usage), cedes formula control (autobumps), same prereqs, LICENSE hard-required. |

### Why not the "modern" prebuilt path (B) yet — the Gatekeeper trap

goreleaser **deprecated `brews:` (formulae) in v2.10**; the current key is
`homebrew_casks:`. But Homebrew **removed the `--no-quarantine` bypass (~Nov 2025)**,
so an **unsigned** prebuilt cask binary triggers *"translate is damaged and cannot be
opened"* on macOS unless you either add a post-install `xattr -dr com.apple.quarantine`
hook or notarize with a paid Apple Developer ID ($99/yr). Option A sidesteps this
entirely — the binary is compiled **locally**, so no quarantine bit is ever set. For a
low-star CLI where Go is a build dep either way, prebuilt bottles buy only ~30–60 s of
first-install build time and are not worth the pipeline.

## Option A — concrete setup (recommended)

1. **Prereqs above** land in a tagged release (e.g. cut `v0.3.1` after adding
   `var version` + `LICENSE`).
2. **Create repo** `github.com/daviddwlee84/homebrew-tap` (the `homebrew-` prefix is
   mandatory for the `brew tap daviddwlee84/tap` shorthand).
3. **Add `Formula/translate.rb`** (filename → class `Translate`):

```ruby
class Translate < Formula
  desc "Fast terminal translation tool (CLI + TUI)"
  homepage "https://github.com/daviddwlee84/translate"
  url "https://github.com/daviddwlee84/translate/archive/refs/tags/v0.3.0.tar.gz"
  sha256 "479b6d6059948ef3bb2a6f2a861f785438926437019ea5f7539abcb4246c9f2c"
  license "MIT" # only once a LICENSE file exists; else drop this line
  head "https://github.com/daviddwlee84/translate.git", branch: "main"

  depends_on "go" => :build

  def install
    # ReadBuildInfo yields "(devel)" from a tarball, so inject the tag via -X.
    # Homebrew's #{version} strips the leading v (0.3.0), so re-add it: v#{version}.
    ldflags = "-s -w -X github.com/daviddwlee84/translate/cmd.version=v#{version}"
    system "go", "build", *std_go_args(ldflags:)   # std_go_args adds -trimpath + -o bin/translate
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/translate --version")
  end
end
```

4. **Compute the sha256** for a new tag:
   `curl -fsSL https://github.com/daviddwlee84/translate/archive/refs/tags/vX.Y.Z.tar.gz | shasum -a 256`
   (GitHub auto-tarballs are stable per tag, but always pin the value you computed).
5. **Validate:** `brew install --build-from-source daviddwlee84/tap/translate`,
   `brew test …`, `brew audit --strict --online daviddwlee84/tap/translate`.
6. **Users install:** `brew install daviddwlee84/tap/translate` (auto-taps).

Notes: no `vendor/` needed — Homebrew's sandbox blocks filesystem writes, **not**
network, so `go build` fetches modules from the proxy at install time (same as
homebrew-core `fzf`/`gh`). `go.mod` pins `go 1.26.4`, so brew's `go` must be ≥ that.

### Maintenance / upgrade flow (how a new tag reaches brew users)

Per release: bump `url` to the new tag + replace `sha256` in `Formula/translate.rb`,
push the tap repo. `version` (and the injected `-X`) derive from the url automatically.
Users get it via `brew update && brew upgrade translate`.

**Automation sweet spot ("A + Action"):** an on-tag GitHub Action in the *translate*
repo that rewrites url+sha256 in the tap — keeps build-from-source (no goreleaser, no
cask, no quarantine hook) while making the manual bump zero-touch:

```yaml
# .github/workflows/brew-bump.yml (in the translate repo), on push tags: ['v*']
- uses: dawidd6/action-homebrew-bump-formula@v3
  with:
    token: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}  # PAT with repo scope on homebrew-tap
    tap: daviddwlee84/homebrew-tap
    formula: translate
    tag: ${{ github.ref_name }}
```

(The default `GITHUB_TOKEN` can't push to a second repo, hence the PAT.)

## Option B — sketch (only when the chezmoi/Go-less/dict-asset driver appears)

goreleaser `version: 2`, `builds` with `CGO_ENABLED=0`, `goos: [darwin, linux]` ×
`goarch: [amd64, arm64]`, `ldflags: -s -w -X github.com/daviddwlee84/translate/cmd.version={{ .Tag }}`,
`archives.formats: [tar.gz]` (v2 renamed `format`→`formats`), and `homebrew_casks:`
with a `repository.token` PAT and a **post-install `xattr -dr com.apple.quarantine`
hook**. Release via `goreleaser/goreleaser-action@v7` (`version: "~> v2"`) on tag push
with `contents: write`. The default archive names (`darwin`/`linux`, `amd64`/`arm64`)
match chezmoi `.chezmoi.os`/`.chezmoi.arch` exactly, so a `.chezmoiexternal`
archive-file external needs no arch mapping — which is the real reason to adopt B (see
[release-binaries.md](release-binaries.md)). Full config in that doc when pursued.

## References

- Homebrew: [Taps](https://docs.brew.sh/Taps) · [Formula Cookbook](https://docs.brew.sh/Formula-Cookbook) · [`std_go_args`](https://rubydoc.brew.sh/Formula.html#std_go_args-instance_method)
- Quarantine change: Homebrew removed `--no-quarantine` (Nov 2025) — brew discussion #6537 / issue #20755
- goreleaser: [`homebrew_casks`](https://goreleaser.com/customization/homebrew_casks/) · [`brews` deprecation (v2.10)](https://goreleaser.com/deprecations/#brews) · [main.version cookbook](https://goreleaser.com/resources/cookbooks/using-main.version/)
- Go: [`runtime/debug.ReadBuildInfo`](https://pkg.go.dev/runtime/debug#ReadBuildInfo) — `Main.Version` is not linker-settable
- Auto-bump Action: [dawidd6/action-homebrew-bump-formula](https://github.com/dawidd6/action-homebrew-bump-formula)
