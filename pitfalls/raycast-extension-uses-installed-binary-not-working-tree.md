# Raycast extension shows no models / "unknown flag" after adding a CLI feature

**Symptoms** (grep this section): a new dropdown in the Raycast extension is empty; the Model picker offers only "Default (engine + tier)"; a target/language choice fails with `translate: unknown flag: --no-pair`; `translate models` prints a *translation of the word "models"* instead of a model list; a CLI subcommand you just added behaves as if it doesn't exist; the extension works but ignores a flag you can see in `cmd/`.
**First seen**: 2026-07-26
**Affects**: the Raycast extension (and any front-end using `resolveBinary()`) whenever the repo is ahead of the installed `translate` binary.

## Symptom

After adding `translate models` and `--no-pair` to the CLI and running
`just raycast-dev`, the extension's Model dropdown contained only its own
placeholder, and picking a specific target language failed.

The confusing part is *how* the old binary fails. `translate` takes free text as
its first argument, so an unknown subcommand is not an error — it gets
**translated**:

```console
$ ~/.local/bin/translate models          # v0.5.2, no `models` command
n. 模型( model的名词复数 ); 模特儿; 模式; 典型
```

Exit status 0, plausible-looking output, no JSON. `runModels()` parses it, fails,
and returns `[]` — an empty dropdown with no error anywhere. A naive capability
probe (`translate models --json >/dev/null && echo yes`) reports **yes**.

Flags fail loudly, at least:

```console
$ ~/.local/bin/translate "hi" --to ja --no-pair
translate: unknown flag: --no-pair
```

## Root cause

`just raycast-dev` rebuilds the **extension**, not the **binary**. The extension
never runs `./translate` from the working tree — `resolveBinary()`
(`raycast/extension/src/lib/translate.ts`) deliberately probes absolute install
directories, because Raycast runs under launchd with a restricted PATH (see
[`raycast-launchd-path-translate-not-found.md`](raycast-launchd-path-translate-not-found.md)):

```
~/.local/bin → /opt/homebrew/bin → /usr/local/bin → ~/go/bin
```

So the extension talks to whatever was last installed:

```console
$ ~/.local/bin/translate --version
translate version v0.5.2                 # 4 days old
$ ./translate --version
translate version v0.5.3-0.20260726…+dirty
```

## Workaround

Install the working tree before testing extension features that depend on new CLI
surface:

```sh
just install            # → ~/.local/bin/translate, first in the probe order
translate --version     # confirm it is the dev build, not the tag
```

Beware a second copy shadowing it — `/usr/local/bin/translate` is the Homebrew
install and is also on the probe list, just later. See
[`duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md`](duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md).

## Prevention

- Treat `just install` as part of the loop when a change spans CLI **and**
  extension: `just check && just install && just raycast-dev`.
- Don't probe capability by exit status. `translate <unknown-subcommand>` exits 0
  because it translated the word. Check for the *shape* of the output (this is why
  `runModels`/`runLangs` parse and fall back rather than trusting the exit code).
- Degrade visibly. An empty list is indistinguishable from "nothing configured",
  so `translate-text.tsx` renders an explicit "the installed CLI is probably
  older" note instead of an empty dropdown.
- A published extension faces the same skew with users' installed binaries, only
  permanently — new CLI surface needs a fallback path, not just a newer README.

## Related

- [`raycast-launchd-path-translate-not-found.md`](raycast-launchd-path-translate-not-found.md) — why the extension resolves an absolute install path at all.
- [`raycast-still-prefills-clipboard-after-default-changed-to-nothing.md`](raycast-still-prefills-clipboard-after-default-changed-to-nothing.md) — the sibling "you rebuilt the wrong thing" trap (stale `ray develop` bundle).
- `AGENTS.md` — the release/versioning flow that makes the installed binary a tag, not `main`.
