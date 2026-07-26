# Raycast Translate still prefills from the clipboard/selection after the manifest default was changed to "Nothing"

**Symptoms** (grep this section): opening the Raycast **Translate** command auto-fills the input box with the clipboard or the selected text even though `package.json` says `"default": "none"`; a Raycast preference change made in `package.json` has no visible effect; a Raycast command behaves per an *old* manifest; `getPreferenceValues()` returns a value you never chose; a Raycast extension keeps doing the thing you just "fixed" in the manifest.
**First seen**: 2026-07-26
**Affects**: any Raycast extension on macOS whose `package.json` preference `default` is changed after the extension has been installed/run once.

## Symptom

Commit `e87db06` changed the `translate` extension's `prefill` preference default
from `"selection"` to `"none"` so plain **Translate** would open with an empty
input box. It did not take effect: opening Translate still seeded the box from the
clipboard.

Confusingly the setting was `"selection"`, not `"clipboard"` — but on macOS
`getSelectedText()` falls back to a copy-and-read-pasteboard path in apps that
don't expose a selection, so with nothing selected it returns *clipboard*
contents. So a stale `"selection"` value looks like clipboard prefill.

## Root cause

Three causes compound; check them in this order.

1. **A stored preference value survives a manifest-default change.** Raycast
   persists a preference value once it has been materialized for a command
   (opening the command, or touching its preference pane). A manifest `default`
   applies **only when nothing is stored**. So changing `default` in `package.json`
   is invisible to any install that already ran the command — and *no code change
   can fix it*, because the stored value is the user's expressed choice as far as
   Raycast is concerned.

   ```console
   $ git show --stat e87db06
    raycast/extension/package.json | 4 ++--
    1 file changed, 2 insertions(+), 2 deletions(-)
   ```

   One file changed — nothing that could reach an existing install's stored value.

2. **A stale installed dev build.** `ray develop` leaves the built bundle
   registered with Raycast after you stop dev mode. If `just raycast-dev` /
   `raycast-build` has not been run since the manifest changed, Raycast is still
   executing the *old* manifest, defaults included.

3. **A code default that contradicts the manifest.** `src/translate.tsx` read

   ```ts
   const mode = prefs.prefill ?? "selection";   // manifest default is "none"
   ```

   Whenever `getPreferenceValues()` yields `undefined` — an older API, or a
   manifest/bundle mismatch as in (2) — the code silently re-enabled prefill.
   TypeScript never caught it because a hand-written `interface Prefs { prefill?: string }`
   **shadowed** the generated `Preferences.Translate`, where `prefill` is required.

## Workaround

**First, reset the stored value** (this is usually the whole problem):

> Raycast → Settings → Extensions → Translate → *Translate* → **Prefill input from**
> → "Nothing (type manually)"

Then rebuild so Raycast is running the current manifest:

```sh
just raycast-dev     # or: just raycast-build
```

The code fix, so the two defaults can never disagree again:

```diff
-interface Prefs { defaultTarget?: string; liveDebounceMs?: string; prefill?: string }
-  const prefs = getPreferenceValues<Prefs>();
+  const prefs = getPreferenceValues<Preferences.Translate>();
-    const mode = prefs.prefill ?? "selection";
+    const mode = prefs.prefill ?? "none"; // must match package.json's default
```

`Preferences.Translate` is generated from the manifest by `ray build` into the
(gitignored) `raycast-env.d.ts`, and declares `prefill` as **required** — so a
future manifest/code drift is a compile error rather than a silent behaviour
change.

**Considered and rejected:** renaming the key (`prefill` → `prefillMode`) to force
Raycast to re-apply the new default. It works, but it silently discards a
deliberate opt-in for anyone who chose selection/clipboard on purpose.

## Prevention

- Read preferences through the generated `Preferences.<Command>` type. Never
  hand-write a preferences interface — the hand-written one makes fields optional
  and re-opens exactly this gap.
- When you change a preference `default`, remember the change reaches **new
  installs only**. If the old behaviour was a bug, say so in the release note and
  tell existing users which setting to reset.
- Test from Raycast, not from a terminal, and rebuild first — see
  [`raycast-launchd-path-translate-not-found.md`](raycast-launchd-path-translate-not-found.md)
  for the other half of that rule.
