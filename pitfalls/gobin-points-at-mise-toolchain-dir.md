# go install drops the binary in ~/.local/share/mise/installs/go/<ver>/bin (orphaned after mise Go upgrade)

**Symptoms** (grep this section): installed CLI "disappears" after a Go
upgrade; `go env GOBIN` shows `.../.local/share/mise/installs/go/1.26.4/bin`;
`go install` puts binaries next to the mise toolchain instead of `~/go/bin` or
`~/.local/bin`; a tool works today and is "not found" after `mise upgrade go`.
**First seen**: 2026-07
**Affects**: macOS/Linux with Go managed by **mise** and `GOBIN` inherited from
the mise env; any `go install`ed CLI meant to be persistent.
**Status**: workaround documented (pin `GOBIN` per install).

## Symptom

```
$ go env GOPATH GOBIN
/Users/david/go
/Users/david/.local/share/mise/installs/go/1.26.4/bin
```

`GOBIN` points into the **version-specific** mise install dir. A plain
`go install github.com/daviddwlee84/translate@latest` lands the binary at
`~/.local/share/mise/installs/go/1.26.4/bin/translate`. The next time mise bumps
Go (e.g. to `1.26.5`), that directory changes / is pruned and the binary is gone
from PATH — the "resident" tool silently vanishes.

## Root cause

The mise Go environment exports `GOBIN` set to the active toolchain's own
`bin` directory. `go install` honors `GOBIN` above `GOPATH/bin`, so installs
follow the toolchain version instead of landing in a stable, version-independent
location. `~/go/bin` (GOPATH/bin) *is* on PATH via the dotfiles
(`dot_config/shell/02_legacy_tools.sh`), but `GOBIN` overrides it.

## Workaround

Pin `GOBIN` to a stable, XDG-clean, already-on-PATH dir for the install:

```sh
GOBIN="$HOME/.local/bin" go install github.com/daviddwlee84/translate@latest
```

`~/.local/bin` is on PATH, survives Go upgrades, and keeps `~/go/bin` (which
already exists for the module cache) uncluttered. Bake this `GOBIN=` into any
chezmoi `run_onchange` / Justfile install recipe so it's deterministic rather
than dependent on the ambient mise env.

## Prevention

- Always set `GOBIN=$HOME/.local/bin` explicitly in install recipes for
  persistent tools; never rely on the ambient `GOBIN`.
- Sanity check after install: `command -v translate` should resolve to
  `~/.local/bin/translate`, not a `.../mise/installs/...` path.

## Related

- Sibling: [go-install-module-path-mismatch.md](go-install-module-path-mismatch.md)
- Sibling: [duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md](duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md)
- Shipped fix: `TODO.md` Done "Publish as a public repo + `go install`"
