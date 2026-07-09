# Two `translate` binaries: ~/.dotfiles/bin shadows ~/.local/bin (edits/reinstalls don't take effect)

**Symptoms** (grep this section): reinstalled or rebuilt `translate` but the
old behaviour persists; `command -v -a translate` prints two paths; a stale
`~/.dotfiles/bin/translate` wins over a fresh `~/.local/bin/translate`; version
mismatch between `which translate` and the just-installed binary.
**First seen**: 2026-07 (anticipated — no live collision yet)
**Affects**: this host's PATH order, where `~/.dotfiles/bin` precedes
`~/.local/bin`; triggered by the Justfile `install` recipe.
**Status**: workaround documented; real fix tracked in `TODO.md` P2.

## Symptom

The blessed install path is `GOBIN=~/.local/bin go install …`. But the
project `Justfile` also has:

```make
install: build
    install -m 0755 translate ~/.dotfiles/bin/translate
```

On this host PATH is ordered `… ~/.dotfiles/bin (pos 11) … ~/.local/bin (pos 13)`,
so if `just install` has ever run, the copy in `~/.dotfiles/bin` **shadows** the
`go install` copy:

```
$ command -v -a translate
/Users/david/.dotfiles/bin/translate     # stale, wins
/Users/david/.local/bin/translate        # fresh, ignored
```

You then rebuild / `go install` a new version, run `translate`, and see no
change — because a different, older binary earlier on PATH is what runs.

## Root cause

Two independent install mechanisms target two different PATH dirs, and
`~/.dotfiles/bin` (chezmoi-managed scripts dir) sorts before `~/.local/bin`.
Shell PATH resolution takes the first match, so whichever dir is earlier wins
regardless of which binary is newer.

## Workaround

Standardize on one location (`~/.local/bin` via `go install`) and remove the
stray copy:

```sh
rm -f ~/.dotfiles/bin/translate
GOBIN="$HOME/.local/bin" go install github.com/daviddwlee84/translate@latest
command -v -a translate   # should print ONLY ~/.local/bin/translate
```

## Prevention

- Fix the `Justfile` `install` recipe to `GOBIN=$HOME/.local/bin go install .`
  (or delete it) so it can't create a shadowing copy — tracked in `TODO.md` P2.
- After any install, verify with `command -v -a translate` that exactly one path
  resolves.

## Related

- Sibling: [gobin-points-at-mise-toolchain-dir.md](gobin-points-at-mise-toolchain-dir.md)
- Real fix: `TODO.md` P2 "Align `just install` with `go install` / `~/.local/bin`"
