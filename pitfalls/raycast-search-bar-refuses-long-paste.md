# Raycast refuses a long paste: "The text you are trying to paste is too long"

**Symptoms** (grep this section): pasting a paragraph into a Raycast extension's search bar shows a red footer "The text you are trying to paste is too long"; only part of a long text lands in the search bar; a Raycast `List` command can't accept more than a short phrase; `onSearchTextChange` never sees the full pasted text; translating/processing a long passage is impossible from a Raycast command.
**First seen**: 2026-07-26
**Affects**: any Raycast extension on macOS that takes input via the search bar (`List` / `Grid` with `onSearchTextChange`). Not specific to this extension.

## Symptom

Pasting a long passage into the **Look up Word** (or **Translate**, or **Define**)
search bar is rejected with a red footer message:

```
The text you are trying to paste is too long
```

The text is simply not accepted — there is no partial paste, no error thrown into
the extension, and nothing in `onSearchTextChange`.

## Root cause

The guard belongs to the Raycast **application**, not to the extension API:

```console
$ strings -a /Applications/Raycast.app/Contents/MacOS/Raycast | grep "paste is too long"
The text you are trying to paste is too long

$ grep -r "paste is too long" raycast/extension/node_modules/@raycast/
$        # nothing — not part of the extension API
```

So an extension cannot raise the limit, configure it, or even observe that it
fired: `@raycast/api` exposes no length option on `List`/`Grid`, and the
rejection never reaches `onSearchTextChange`. The search bar is a single-line
launcher input; long documents were never its job.

## Workaround

Give long text an input that is not the search bar. `Form.TextArea` has no such
cap — that is what `raycast/extension/src/translate-text.tsx` (**Translate Text**)
exists for.

```tsx
<Form actions={…}>
  <Form.TextArea id="text" title="Text" value={text} onChange={setText} />
</Form>
```

Two further limits sit behind it, both worth knowing before assuming the CLI is
at fault:

1. **`ARG_MAX` on the exec.** Passing the text as an argv element caps out at
   1 MiB on macOS (`getconf ARG_MAX` → 1048576). Measured: 900 KB works, 1.1 MB
   fails the spawn with `argument list too long`. `src/lib/translate.ts` switches
   to stdin above 128 KB of UTF-8 (`ARGV_SAFE_BYTES`), since `translate` reads
   stdin when given no text argument and a pipe has no such limit.
2. **The provider.** The Google engine puts the text in a GET query string and
   fails around 180 KB with a URL-length error; LLM providers have their own
   token limits (copilot-proxy returned `http 400` at 1.1 MB). These are the real
   ceiling for a genuinely large document, and no client change moves them.

**Also worth trying before writing any code:** *Translate Selection* reads the
selection programmatically and seeds Translate through `launchContext`, so it
never pastes into the search bar at all.

## Prevention

- Treat the search bar as an input for words and short phrases. Any command that
  should accept a paragraph needs a `Form`.
- When something "can't be pasted" in Raycast, check `strings` on the app binary
  before hunting through the extension — an app-level guard produces no error the
  extension can see.

## Related

- [`raycast-still-prefills-clipboard-after-default-changed-to-nothing.md`](raycast-still-prefills-clipboard-after-default-changed-to-nothing.md) — the other Raycast-app-vs-extension boundary trap.
- [`../docs/raycast-extension.md`](../docs/raycast-extension.md) — Gotchas.
