# Raycast can't find `translate` (`spawn translate ENOENT` / "command not found") though it works in a terminal

**Symptoms** (grep this section): a Raycast Script Command prints `translate: command not found`; a Raycast TS extension throws `Error: spawn translate ENOENT`; the exact same command works fine in a terminal but not from Raycast root search; translation works from the shell but the Raycast command errors or shows nothing.
**First seen**: 2026-07 (anticipated — designed around, not yet hit in prod)
**Affects**: Raycast (macOS) Script Commands + TypeScript extensions on this host; any GUI/launchd-launched process that calls the `translate` binary by bare name.

## Symptom

Calling `translate` by bare name from Raycast fails, e.g. a script's
`translate "$2" --to "$1"` yields `command not found`, or an extension's
`execFile("translate", …)` / `spawn("translate")` throws:

```
Error: spawn translate ENOENT
```

The identical invocation succeeds in Terminal/iTerm, which makes it look like the
binary is "missing" only under Raycast.

## Root cause

Raycast is launched by the macOS GUI (launchd), and GUI-launched processes do
**not** source your interactive shell's rc files (`~/.zshrc`, `~/.zprofile`). So
`process.env.PATH` inside a Raycast extension/script is the minimal launchd PATH
and does **not** include `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, or
`~/go/bin` — exactly where `translate` gets installed (`go install` / `just install`
→ `~/.local/bin`; Homebrew → `/opt/homebrew/bin`). A bare-name lookup then fails.

## Workaround

Resolve an **absolute** path to the binary instead of trusting PATH.

- Script Commands ([`../raycast/script-commands/`](../raycast/script-commands/)) —
  probe known dirs (already baked into every script):

  ```sh
  find_translate() {
    for d in "$HOME/.local/bin" /opt/homebrew/bin /usr/local/bin "$HOME/go/bin"; do
      [ -x "$d/translate" ] && { printf '%s\n' "$d/translate"; return 0; }
    done
    command -v translate 2>/dev/null && return 0
    return 1
  }
  ```

- TS extension ([`../raycast/extension/src/lib/translate.ts`](../raycast/extension/src/lib/translate.ts))
  — a `binaryPath` preference first, then the same 4-dir probe (`resolveBinary()`).

Probe order must keep `~/.local/bin` **first** — the blessed install location
(see sibling below) — so a stray copy elsewhere can't win.

## Prevention

- Never call bare `translate` from any Raycast surface; always resolve an absolute
  path (preference or probe).
- **Test from Raycast, not a terminal.** A terminal inherits your full PATH and
  will happily find the binary, masking the bug entirely.

## Related

- Sibling: [duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md](duplicate-translate-on-path-dotfiles-bin-shadows-local-bin.md)
  (why `~/.local/bin` must be first in the probe)
- Reference: [../docs/raycast-extension.md](../docs/raycast-extension.md) "Gotchas"
- Backlog: `TODO.md` P? "Publish the Raycast extension to the store" ·
  [../backlog/raycast-extension.md](../backlog/raycast-extension.md)
