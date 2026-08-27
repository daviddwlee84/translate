# GoReleaser can't publish a Homebrew *formula* any more — and the cask it offers instead is quarantined

**Symptoms** (grep this section): `brews:` rejected or warned as deprecated by
`goreleaser check`; goreleaser docs only mention `homebrew_casks:`; a prebuilt
binary installed from a personal tap dies with *"translate is damaged and cannot
be opened"* or *"cannot be opened because the developer cannot be verified"*;
`xattr -l` on the installed binary shows `com.apple.quarantine`; unsure whether
prebuilt binaries need Apple notarization.
**First seen**: 2026-08-27, wiring up release automation.
**Affects**: any single-maintainer CLI publishing prebuilt binaries to its own
Homebrew tap via GoReleaser v2.10+.
**Status**: worked around — GoReleaser owns the Scoop bucket only; the formula is
templated by `packaging/translate.rb.tmpl` + `scripts/bump-formula.sh`.

## Symptom

Two facts collide:

1. **GoReleaser deprecated `brews:` (formulae) in v2.10** in favour of
   `homebrew_casks:`.
2. **Homebrew removed the `--no-quarantine` bypass (~Nov 2025)**, so an unsigned,
   un-notarized binary installed *as a cask* gets `com.apple.quarantine` and
   Gatekeeper refuses to run it.

Follow GoReleaser's migration and you land exactly on the broken path. This is
why `backlog/homebrew-distribution.md` originally concluded that prebuilt
binaries were off the table for this project and chose build-from-source.

## Why it happens

The quarantine attribute is **not** applied by Homebrew generally — only by the
cask code path:

```console
$ grep -rh quarantine /usr/local/Homebrew/Library/Homebrew/*.rb
require "cask/quarantine"
require "cask/quarantine"
        # If quarantine is not available, a warning is already shown by check_cask_quarantine_support so just return
        require "cask/quarantine"
```

Every hit is under `cask/`. A **formula** that installs a prebuilt binary is
never quarantined, so it needs no signing or notarization on the Homebrew side.

Separately, a Go binary cross-compiled for darwin is **already ad-hoc signed** by
the Go linker, which is what arm64 requires in order to execute at all:

```console
$ codesign -dv ./translate     # cross-built from linux/amd64 for darwin/arm64
CodeDirectory v=20400 ... flags=0x20002(adhoc,linker-signed)
Signature=adhoc
```

## Fix

Keep publishing a **formula**, and do it yourself:

- `.goreleaser.yaml` declares `scoops:` (which GoReleaser still fully supports)
  and nothing Homebrew-related.
- `packaging/translate.rb.tmpl` holds the formula with `__VERSION__` /
  `__SHA256_<OS>_<ARCH>__` placeholders.
- `scripts/bump-formula.sh --version vX.Y.Z` fills them from
  `dist/checksums.txt` and pushes to the tap. `--dry-run` prints the rendered
  formula instead.

Do **not** reach for `homebrew_casks:` "because that's what the docs say now" —
the docs are steering toward a distribution shape (GUI apps, signed binaries)
this project does not have.

## Related

- `backlog/homebrew-distribution.md` — the original (now corrected) analysis.
- Cross-repo pushes need a PAT: a workflow's default `GITHUB_TOKEN` is scoped to
  its own repository, so pushing to the tap or the scoop bucket requires a
  fine-grained token with `contents: write` on both.
