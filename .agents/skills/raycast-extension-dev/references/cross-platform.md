# Cross-platform: macOS and Windows

Read this when targeting Windows, when a shortcut does nothing on one OS, when
shelling out to a platform-specific script, or when an extension you can install
on your Mac does not appear in the Windows store.

## Table of contents

- [The platforms field](#the-platforms-field)
- [What actually differs](#what-actually-differs)
- [The three layers](#the-three-layers)
- [Scripts: AppleScript and PowerShell](#scripts-applescript-and-powershell)
- [Shortcuts](#shortcuts)
- [Per-platform manifest values](#per-platform-manifest-values)
- [Applications and paths](#applications-and-paths)
- [Which existing gotchas are macOS-only](#which-existing-gotchas-are-macos-only)
- [Developing on Windows](#developing-on-windows)

---

## The platforms field

`platforms` arrived in **1.103.0 (2025-09-15)**. It is the field that makes an
extension visible on Windows:

```json
"platforms": ["macOS", "Windows"]
```

Three facts that decide everything else:

- **Always write the field. The two authorities disagree about the default.**
  The changelog (1.103.0) says *"By default, if not specified, the field's value
  is `["macOS"]`."* The live JSON schema says the opposite — *"If not present,
  the extension is assumed to be available on all platforms."* Both were true as
  written on 2026-07-28; `platforms` is **not** in the schema's top-level
  `required` list, so nothing forces you to resolve it. Resolve it anyway: an
  omitted `platforms` either hides a portable extension from every Windows user
  or advertises an untested one to them, and you cannot tell which from the
  manifest. This is also why so much of the store is Windows-capable code that
  simply never declared itself.
- **It is extension-level. There is no per-command `platforms`.** One
  `mode: "menu-bar"` command therefore pins the *entire* extension to macOS,
  including the six view commands next to it. The schema says so in the `mode`
  enum itself — *"'menu-bar' renders an extra item in the macOS system menu bar"*
  — so this is not a policy you can argue with. If you want both, the menu bar
  has to move to a separate extension.
- **The store shows only what matches the current OS.** There is no "unavailable
  on your platform" row, so a wrong `platforms` value looks exactly like the
  extension not existing.

The store guidance is the inverse framing, and it is the rule to follow:
*"if you use platform-specific APIs, restrict the `platforms` field to the
corresponding platform"*. Declare both, then subtract.

## What actually differs

Raycast renders the UI. You ship React, TypeScript, and Node — none of which
cares about the OS. From raycast.com/windows: *"All extensions that do not
require native code will work out of the box on both platforms."*

| Surface | macOS | Windows |
| --- | --- | --- |
| `List` / `Detail` / `Form` / `ActionPanel` | ✅ | ✅ |
| `useCachedPromise`, `Cache`, `LocalStorage` | ✅ | ✅ |
| REST/GraphQL, any pure-JS npm package | ✅ | ✅ |
| `execFile` of a bundled or user-installed binary | ✅ | ✅ (different path shape) |
| `mode: "menu-bar"` / `MenuBarExtra` | ✅ | ❌ *"Menubar commands aren't available on Windows."* |
| `runAppleScript` | ✅ | ❌ |
| `runPowerShellScript` | ❌ | ✅ *"Only available on Windows"* |
| `Application.bundleId` | ✅ | — use `windowsAppId` |
| AI Extensions (`tools[]`) | ✅ | **undocumented — but shipped** (see below) |
| Browser Extension API | ✅ | **undocumented** |
| Window Management API | ✅ | **undocumented** |

The last three rows are honest gaps, not omissions. Neither the changelog nor
raycast.com/windows states whether those APIs work on Windows. **Do not assert
either way** — verify on a Windows install before declaring `Windows` for an
extension that depends on them.

For AI Extensions there is at least precedent:
`raycast/extensions/extensions/emoji-kitchen` ships
`"platforms": ["macOS", "Windows"]` with three `tools[]` entries. That proves the
combination passes store review; it does not prove the tools execute correctly
there.

One trap worth naming: Raycast for Windows *has* window management as a built-in
feature. That says nothing about whether the `WindowManagement` API is exposed to
third-party extensions there. A shipped feature is not a shipped API.

## The three layers

Design for the split before you need it. Only the bottom layer is ever
platform-aware:

```text
Raycast UI          List / Detail / Form / ActionPanel     100% shared
Domain logic        parsing, models, argv builders          ~100% shared
OS adapter          AppleScript / PowerShell, paths         split
```

```text
src/
├── tasks.tsx              UI — imports from platform/, never branches itself
├── lib/
│   ├── transport.ts       the Mutation data-union seam (see SKILL.md Workflow B)
│   └── platform/
│       ├── index.ts       picks an implementation once, at import time
│       ├── macos.ts       runAppleScript, bundleId, /opt/homebrew probing
│       └── windows.ts     runPowerShellScript, windowsAppId, %LOCALAPPDATA%
```

```ts
// lib/platform/index.ts
import * as macos from "./macos";
import * as windows from "./windows";

export const platform = process.platform === "win32" ? windows : macos;
```

**Branch once, at the seam — never in a component.** A `process.platform` check
inside a render function is a bug in waiting: it makes every surface responsible
for knowing the OS, and it defeats the point of the transport seam, which is that
the UI does not know how anything is fetched.

Realistic sharing, by extension shape:

| Shape | Shared |
| --- | --- |
| SaaS / REST API wrapper | ~100% — usually just declare `Windows` and test |
| Local CLI wrapper | 70–90% — binary resolution and paths differ, argv does not |
| Deep OS automation | 30–70% |
| Menu bar, Finder, AppleScript-first | macOS only, or a genuine second implementation |

## Scripts: AppleScript and PowerShell

```ts
import { runAppleScript, runPowerShellScript } from "@raycast/utils";
```

Both live in `@raycast/utils`, and each throws on the wrong OS —
`runPowerShellScript` is documented *"Only available on Windows"*. Importing both
in one module is fine; **calling** the wrong one is not. Keep each behind its own
file so the platform picker decides, and so a reader can see at a glance which
half of the behaviour they are reading.

Nothing checks this. `ray lint` will not tell you that a `platforms` list
including `Windows` sits above a `runAppleScript` call —
`scripts/check-store-readiness.sh` will (`platforms-windows-macos-apis`).

## Shortcuts

Per-platform shortcuts landed in **1.98.0 (2025-05-08)**. `Keyboard.Shortcut` is
a union of two shapes:

```ts
type Shortcut =
  | { modifiers: KeyModifier[]; key: KeyEquivalent }        // one binding, both OSes
  | { macOS: {...}; Windows: {...}; windows?: {...} };      // per platform
```

```tsx
shortcut={{
  macOS:   { modifiers: ["cmd", "shift"],  key: "c" },
  Windows: { modifiers: ["ctrl", "shift"], key: "c" },
}}
```

Three things the type tells you that the changelog does not:

- **Both branches are required.** In the per-platform form `macOS` and `Windows`
  are non-optional, so you cannot bind one OS and leave the other unset — the
  compiler makes you decide. That is the good failure mode; the bad one follows.
- **Lowercase `windows` is deprecated** (`/** @deprecated Use Windows instead */`)
  and still typechecks. Capital `W`.
- The modifier union is
  `"cmd" | "ctrl" | "opt" | "shift" | "alt" | "windows"` — one flat list with no
  platform in the type, which is exactly why the first branch compiles happily
  with a macOS-only binding.

| Modifier | macOS | Windows |
| --- | --- | --- |
| `cmd` | ⌘ | ignored |
| `opt` | ⌥ | ignored |
| `windows` | ignored | ⊞ |
| `alt` | ⌥ (alias) | Alt |
| `ctrl`, `shift` | ⌃ / ⇧ | Ctrl / Shift |

**A hardcoded modifier is dropped in silence.** Verbatim: *"If you use shortcuts
and specify a modifier like `cmd`, the shortcut will be ignored on Windows (and
vice-versa, if you specify a modifier like `windows`, it won't be available on
macOS)."* No error, no lint failure, no runtime warning — the action is simply
there with no key beside it, and the bug report says "the shortcut doesn't work".

The cheap way to never hit this: **prefer `Keyboard.Shortcut.Common.*`**, which
Raycast maps per platform itself. Reach for a literal modifier object only when
no common constant fits, and then use the two-branch form — the type will not let
you half-finish it.

## Per-platform manifest values

The manifest accepts a platform-keyed object anywhere a plain value is allowed —
*"you can specify a different value per platform by passing an object:
`{ "macOS": ..., "Windows": ... }`"*. The case that matters is a preference
default that is a filesystem path:

```json
{
  "name": "workspacePath",
  "type": "directory",
  "title": "Workspace",
  "description": "Where projects are scanned from.",
  "default": {
    "macOS": "/Users/me/projects",
    "Windows": "C:\\Users\\me\\projects"
  }
}
```

Note the doubled backslashes — this is JSON, and a single `\U` is an invalid
escape that fails to parse before `ray lint` ever sees the schema.

Do not hand a user a default that cannot exist on their machine. A `textfield`
holding `/opt/homebrew/bin/mytool` on Windows is not a harmless leftover: it
passes validation, fails `existsSync`, and the extension reports "binary not
found" while pointing at a path that was never plausible.

## Applications and paths

`Application` carries an identifier for each platform:

```ts
interface Application {
  name: string;
  path: string;
  bundleId?: string;       // "The macOS bundle identifier", e.g. com.raycast.macos
  windowsAppId?: string;   // "The Windows App ID"
  localizedName?: string;
}
```

```text
macOS    /Applications/Visual Studio Code.app          bundleId com.microsoft.VSCode
Windows  C:\Users\…\AppData\Local\Programs\Microsoft VS Code\Code.exe
```

Both fields are optional, so **match on the one for the current platform and fall
back to `name`** rather than assuming either is populated. And build every path
with `node:path` — `join`, `sep`, `homedir()` — never a string literal with a
separator in it.

## Which existing gotchas are macOS-only

Several hard-won rules elsewhere in this skill are macOS-shaped. They are still
correct; they are just scoped:

| Rule | Scope | Windows equivalent |
| --- | --- | --- |
| launchd strips `PATH` to `/usr/bin:/bin` | macOS | Raycast on Windows likewise does not run your shell profile — resolve absolute paths there too, from Windows locations |
| Probe `/opt/homebrew/bin` **and** `/usr/local/bin` | macOS | probe `%LOCALAPPDATA%\Programs`, `%ProgramFiles%`, and a user-set preference |
| `sips` for icon dimensions | macOS | `check-store-readiness.sh` reports `skip`, which is **not** a pass |
| Monochrome template SVG tinted by the menu bar | macOS | no menu bar, so no equivalent |

The invariant that survives both platforms: **never invoke a bare binary name,
and let a preference override the resolved path.** The reason differs by OS; the
rule does not.

## Developing on Windows

Requirements are the same toolchain, one host apart: **Raycast ≥ 1.26.0,
Node ≥ 22.14, npm ≥ 7**, on **Windows 10 21H2+ or Windows 11**. `Create
Extension`, `Import Extension`, and `Fork Extension` all exist there, and
`npm run dev` hot-reloads the same way.

What does not carry over is this skill's `Justfile.template` gate as written —
`just` runs fine on Windows, but the recipes assume a POSIX shell. On a
Windows-only checkout, run the four stages directly:

```powershell
npx tsc --noEmit -p tsconfig.json
npx ray lint
npx ray build
```

If you develop on macOS and declare `Windows`, you have shipped something you
have not run. That is a defensible choice for a pure API extension and a bad one
for anything that touches the filesystem — say which in the PR.
