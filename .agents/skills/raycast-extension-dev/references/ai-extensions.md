# AI extensions: tools, confirmation, and evals

Read this when adding `tools[]`, writing `ai.yaml`, deciding whether an AI layer
is worth building at all, or when the user asks why the Suggested Prompts list
under `@your-extension` is empty.

## Table of contents

- [Who can actually use this](#who-can-actually-use-this)
- [When an AI layer is worth it](#when-an-ai-layer-is-worth-it)
- [tools[]](#tools)
- [The tool file](#the-tool-file)
- [Confirmation](#confirmation)
- [ai.yaml](#aiyaml)
- [What the build extracts from your tool file](#what-the-build-extracts-from-your-tool-file)
- [Evals are the Suggested Prompts](#evals-are-the-suggested-prompts)
- [Running evals](#running-evals)
- [The AI API](#the-ai-api)

---

## Who can actually use this

The documentation and the product disagree, and the disagreement matters because
it decides whether you build the feature at all.

> *"To use AI APIs or AI Extensions, you need to subscribe to Raycast Pro."*
> — developers.raycast.com/ai/getting-started, still live 2026-07-28

That sentence is stale. Raycast Settings → AI shows three other ways in:

| Path | What it looks like in Settings → AI |
| --- | --- |
| Free allowance | `Try AI — N Messages Left \| x% Used` |
| Custom Providers | *"Add custom providers or use your local LLM by editing the `providers.yaml` file"* — routed direct to the provider, billed at their rates. GitHub Copilot is listed. |
| Local models | *"Locally downloaded AI models via Ollama are supported"*, with an Ollama host and model picker |
| Raycast Pro | unlimited messages |

So **do not reason about subscriptions in code, and do not skip the feature
because of Pro.** There is exactly one correct check, and it is a runtime
capability test:

```ts
import { AI, environment } from "@raycast/api";

if (!environment.canAccess(AI)) {
  // degrade — do not throw, do not show a paywall you cannot verify
}
```

Documented behaviour: *"You can check if a user has access to the API using
`environment.canAccess(AI)`"* and *"If the user doesn't wish to get access, the
API call will throw an error."* The signature is `canAccess(api: unknown)` — it
takes the namespace object itself, not a string. Do not copy the JSDoc example
out of `@raycast/api`'s own typings: it imports `unstableAI`, which is not an
export (the alias is `unstable_AI`, and the real name is `AI`).

`canAccess` is only needed when **your own code** calls `AI.ask`. A `tools[]`
entry is invoked *by* Raycast AI, so the user has already cleared whatever gate
applies before your function runs — a tool never needs to check.

**Windows: undocumented, but shipped.** No Raycast doc states whether AI
Extensions work there. The empirical answer is that they are at least *allowed*:
`raycast/extensions/extensions/emoji-kitchen` declares
`"platforms": ["macOS", "Windows"]` alongside three `tools[]` entries and is live
in the store. Treat that as precedent for the manifest combination passing
review, not as proof the tools execute correctly on Windows — verify on a Windows
install before you rely on it. See `cross-platform.md`.

## When an AI layer is worth it

The honest test is whether you already did the work. If Workflow B's transport
returns structured data, a tool is a fifteen-line wrapper and the remaining
effort is prompt-shaped, not code-shaped:

```ts
export default async function tool() {
  return await read({ group: undefined });   // the transport you already have
}
```

That is the whole read-side tool. Propose one whenever:

- the extension already parses its backend into typed objects, **and**
- a user question would otherwise require reading a list and doing arithmetic
  ("how far through is each group", "which failures share a cause").

Skip it when the extension is a launcher or a single-action command — there is
nothing for a model to reason over, and a tool that wraps one deterministic
action is worse than the keyboard shortcut for it.

## tools[]

Straight from the live schema at
<https://www.raycast.com/schemas/extension.json>:

```json
"tools": [
  {
    "name": "list-tasks",
    "title": "List Tasks",
    "description": "Read the queue and return every task with its status, group, and runtime."
  }
]
```

| Field | Rule |
| --- | --- |
| `name` | required · pattern `^[a-z0-9-][a-zA-Z0-9-_]*$` · 2–64 chars · **maps to the entry point file**: `list-tasks` → `src/tools/list-tasks.ts` |
| `title` | required · 2–255 · pattern `^[^\s]+(?: [^\s]+)*$`, so no leading, trailing, or doubled spaces |
| `description` | required · **minimum 12 characters** · *"helps users (and other actors like AI) understand what the tool does"* |
| `icon` | optional · 512×512 PNG in `assets/`, `@light`/`@dark` suffixes supported · inherits the extension icon |
| `keywords`, `preferences` | optional, same shapes as a command's — tool preferences inherit the extension's and can override by `name` |
| `functionalities` | optional · enum `"AI attachment provider"` \| `"AI tool"` · restricts where the tool may be used. Omit unless you mean to restrict. |

The array is capped at **100 tools**, and each item is `additionalProperties: true`
— an unknown key does not fail `ray lint`, so a typo'd field name is silently
accepted and silently ignored. `keywords`, `functionalities` and `preferences`
are in the schema but not in the prose docs.

**A command and a tool may share a name.** They resolve through different
directories (`src/<n>.tsx` vs `src/tools/<n>.ts`), and `raycast/extensions` ships
it — `cursor-agents` has both a `launch-agent` command and a `launch-agent` tool.
Note that a command may override its file with `"src": "src/info.tsx"`; **a tool
may not** — tools are resolved purely by name.

The description is **prompt engineering, not documentation** — it is the primary
signal Raycast AI uses to pick between your tools. Two tools whose descriptions
differ only in adjectives will be chosen between at random.

Tools do not appear in root search. They are reached with `@extension-name` in
Quick AI or AI Chat, or from the *Ask …* item at the top of the extension.

## The tool file

```ts
// src/tools/list-tasks.ts
type Input = {
  /**
   * Only return tasks in this group. Omit for every group.
   * Group names are case-sensitive and come from `list-groups`.
   */
  group?: string;
  /** Only return tasks in this state. */
  status?: "queued" | "running" | "success" | "failed";
};

export default async function tool(input: Input) {
  const state = await read({ group: input.group });
  return state.tasks.filter((t) => !input.status || t.status === input.status);
}
```

- **A tool takes exactly one object.** *"A tool expects a single object as its
  input."* Positional parameters are not a thing here. The type does **not** have
  to be called `Input`, and it can be declared inline —
  `async function (i: { /** … */ q: string })` extracts fine. `Input` is just the
  convention, and following it is still the right call.
- **JSDoc on the `Input` fields IS the parameter schema — but only when it is on
  its own line.** Measured:

  ```ts
  type Input = {
    /** This becomes the description. */
    a: string;
    /** inline on the same line */ b: string;   // <-- SILENTLY dropped
    // a plain line comment                     // <-- also dropped
    c: string;
  };
  ```

  Same-line JSDoc and `//` comments produce a property with **no `description`**
  and no warning. An undocumented field is one the model guesses at, so this
  formatting slip costs real accuracy while looking documented in the editor.
  The official best-practice page names the two things worth writing there, and
  both are about *acquisition*, not meaning:
  - *"Include information in your tool's input on how to format parameters like
    IDs or dates."*
  - *"Include information in your tool's input on how to get the required
    parameters."* — i.e. name the sibling tool that returns them. Without this
  the model invents an id and the call fails for reasons it cannot diagnose.
- **Prefer a string union over `string`** for anything enumerable. It lands in
  the schema as an `enum` and stops the model inventing `"pending"`.
- **Return data, not prose.** The model does the writing; a tool that returns a
  formatted sentence throws away the structure the model needed.
- **Catch expected failures; do not let them escape.** An unhandled throw or
  rejected promise is caught by Raycast, which *"shows error screens"* — the run
  ends rather than the model recovering. Network failures, missing permissions
  and empty results are ordinary outcomes: return them as data
  (`{ error: "daemon not running" }`) so the model can tell the user something
  useful. Reserve throwing for genuinely unrecoverable states.
- **Strip secrets on the way out**, exactly as at the cache boundary
  (`data-and-state.md`) — a tool return value goes to a model, and with a Custom
  Provider that model is somebody else's endpoint.

### What the Input type may contain

Measured on 1.104.23 by building one probe tool per case. **An unsupported type
fails the whole build** with `Error: extracting tool schemas failed` — which is
notable, because `ray build` otherwise does not typecheck at all. Tool schema
extraction is the one thing the dev build *does* enforce.

| Works | Extracted as |
| --- | --- |
| `string`, `number`, `boolean` | the matching JSON type |
| `string[]` | `{"type":"array","items":{"type":"string"}}` |
| `"x" \| "y"` | `{"type":"string","enum":["x","y"]}` |
| `a?: string` | present in `properties`, absent from `required` |
| nested object literal | a nested `object` with its own `required` |
| `type Input = Base` (alias) | the aliased shape |

| Hard build error | Message |
| --- | --- |
| `any` | ``` `any` type is not supported for "a" ``` |
| `unknown` | `Unknown type for "a"` |
| `"x" \| 1` | `Only string unions are supported for "a"` |
| `1 \| 2` | `Only string unions are supported for "a"` — **numeric unions are not enums here** |
| `A & B` | `Intersection types are not supported for "a"` |
| `Pick<Base,"k">` | `Could not resolve type Pick` |
| `Partial<Base>` | `Could not resolve type Partial` |

And the one that is neither — **silently wrong**:

```ts
interface Base  { b: string }
interface Input extends Base {}        // -> properties: {}   NO ERROR
interface Input { b: string }          // -> properties: { b } ✅
```

**Interface inheritance is dropped without a word.** The build succeeds, the tool
ships, and the model is handed a tool that appears to take no arguments. Declare
the members directly, or use a type alias.

Mapped types being unsupported has a practical cost: you cannot derive an
`Input` from a domain type with `Pick`/`Partial`/`Omit`. Write the tool's input
out longhand and let `tsc` check the call site instead.

## Confirmation

**There is a confirmation surface inside a tool call.** A tool file may export
one, and Raycast calls it before executing:

```ts
import { Action, Tool } from "@raycast/api";

export const confirmation: Tool.Confirmation<Input> = async (input) => {
  if (!input.ids?.length) return undefined;      // nothing destructive — skip it
  return {
    style: Action.Style.Destructive,
    message: `Kill ${input.ids.length} running task(s)? Output is not recoverable.`,
    info: [
      { name: "Group", value: input.group ?? "all" },
      { name: "Tasks", value: input.ids.join(", ") },
    ],
  };
};
```

The typings say
`type Confirmation<T> = (input: T) => Promise<undefined | { … }>` — note the
`Promise`, so the export is `async` even when it computes nothing. It is
*"executed **before** the actual tool is executed and receives the same input as
the tool"*; cancelling means the tool never runs. Fields: `message`, `info[]` of
`{ name, value? }`, `image`, and `style` — which is **`Action.Style`, reused**
(`Action.Style.Destructive`), not a `Tool`-local enum. Default is
`Action.Style.Regular`.

Two consequences:

- **A destructive tool does not have to be withheld.** The old advice "ship
  read-only first" was based on this API not existing. Gate it instead.
- **`undefined` skips the prompt**, so confirmation can be conditional — free for
  a no-op call, present for a real one. Entries in `info` whose value is
  `undefined` are not rendered, so building it is not fiddly.

`confirmAlert` still does not work here. It renders in the Raycast window, and a
tool call is not guaranteed to have one — `Tool.Confirmation` is the replacement,
not an addition.

## ai.yaml

`instructions` and `evals` live under the manifest's `ai` key, but the docs
recommend pulling them out: *"we recommend you to use a `ai.yaml` file in the
root of your extension next to the `package.json` file."* The file holds the
**contents** of the `ai` object, not an `ai:` wrapper:

```yaml
# ai.yaml
instructions: |
  Never restart, kill, or clean anything unless the user explicitly asks.
  Task ids are only unique within a group — always report the group too.
  When the queue is empty, say so plainly instead of describing the schema.

evals:
  - input: "@pueue What's running right now, and how long has each one been going?"
  - input: "@pueue Why did my failed tasks fail? Read the logs — don't fix anything yet."
  - input: "@pueue Which group has the most failures, and do they share a cause?"
```

**Four filenames are accepted, and the first match wins.** From the CLI's own
`readAIFile`, in order: `ai.json`, `ai.json5`, `ai.yaml`, `ai.yml`. So a stale
`ai.json` left in the root silently shadows the `ai.yaml` you are editing, and
nothing reports it. Keep exactly one.

`ray develop` watches `ai.json`, `ai.yml`, and `ai.yaml` for changes — **not
`ai.json5`**, which is read at build time but not hot-reloaded. Use YAML.

The file is merged over the manifest, not swapped for it: the build does
`ai = { ...packageJson.ai, ...aiFile }`. A shallow merge, so a key present in
both comes from the file, and a key only in `package.json` survives. Splitting
`instructions` and `evals` across the two works and is a bad idea — put both in
one place.

The equivalent inside `package.json` is identical in shape:

```json
"ai": { "instructions": "…", "evals": [{ "input": "@pueue …" }] }
```

`instructions` is *"added as a system message whenever the extension is
mentioned"* — extension-wide, and it applies to every tool. Put the invariants
there (what is destructive, what identifiers mean, what never to do unasked) and
keep per-tool semantics in the tool `description` and the `Input` JSDoc.

## What the build extracts from your tool file

`ray build` runs the TypeScript compiler over each `src/tools/<name>.ts` and
derives the tool's schema from the source. This is why the typing discipline
above is load-bearing rather than stylistic. Inspect what it produced with
`ray build --print-tool-schemas` — measured output on 1.104.23:

```json
{
  "name": "kill-task",
  "instructions": "…the JSDoc on the default export, verbatim…",
  "input": {
    "type": "object",
    "properties": {
      "status": { "type": "string", "enum": ["active", "done"],
                  "description": "…the JSDoc on that field…" }
    },
    "required": []
  },
  "confirmation": true
}
```

| From | Becomes |
| --- | --- |
| the default export's parameter type | the tool's `input` JSON schema |
| JSDoc on each `Input` field | that property's `description` |
| a string union field | an `enum` — this is why unions beat `string` |
| the JSDoc on the default export itself | the tool's `instructions` |
| an exported `confirmation` | `confirmation: true` |

**`output` is not emitted.** The CLI reads the awaited return type and has a code
path for an `output` schema, but on 1.104.23 no return shape produced one —
`Promise<T[]>`, `Promise<{…}>`, and `Promise<unknown[]>` all came back with the
key absent. Annotate return types anyway, because `tsc` is checking them and the
model still sees the actual returned value at runtime; just do not expect the
schema to advertise it.

Consequences worth knowing before you hit them:

- **Build errors, not runtime errors.** `Default exported function not found`,
  `Tools should have at most one parameter (has 2)`, and every unsupported-type
  message above abort the build with `Error: extracting tool schemas failed`.
- **A field with no JSDoc — or with JSDoc on the wrong line — ships with no
  `description`.** See the formatting trap above.
- **A bare `@word` in tool JSDoc is parsed as a JSDoc tag.** The extractor runs
  `getJsDocTags()` and appends them, so writing `@raycast/api` in a tool comment
  reappears in the extracted `instructions` as `@raycast /api`. Write "Raycast
  API" in prose, or backtick it.
- **`confirmation` survives every export form.** Measured: a typed
  `export const confirmation: Tool.Confirmation<Input>`, an untyped `const`, a
  `function` declaration, a named re-export, and a HoC-wrapped default export all
  produce `confirmation: true`. Only a missing export or a misspelt name gives
  `false` — and a misspelt name fails silently, since nothing requires one.
- **The extractor unwraps `with…()` wrappers**, so the shapes real extensions
  use — `export default withAccessToken(client)(tool)`,
  `export default withGoogleAPIs(tool)` — are analysed correctly.
- On `ray develop`, `--print-instructions` shows the merged `ai.instructions` and
  `--print-tool-calls` (default **on**) logs every tool input and output,
  including whether the call was the `confirmation` or the `tool`. That is the
  fastest way to see why the model passed what it passed.

## Evals are the Suggested Prompts

This is the part nobody finds by reading the API reference. From the docs:

> *"They are also used as suggested prompts for the user to learn how to make the
> most out of your AI Extension."*

The `evals[].input` strings are what render under **Suggested Prompts** when a
user types `@your-extension`. `usedAsExample` controls it — *"Whether the eval
can be used as an example in Raycast (default `true`)"*. So:

- **An extension with `tools[]` and no `evals` shows an empty prompt list.** The
  tools work; the user just has no idea what to ask. That is the single highest
  ratio of perceived quality to effort in the whole AI surface.
- **Write them as prompts, not as test fixtures.** They are user-facing copy.
  The full form is also a test:

  ```json
  {
    "input": "@pueue what are my failed tasks",
    "mocks": { "list-tasks": [{ "id": 3, "status": "failed", "group": "default" }] },
    "expected": [{ "callsTool": "list-tasks" }]
  }
  ```

  `mocks` is keyed by **tool name** and holds that tool's stubbed return value,
  so an eval never touches your backend.

  **Only `input` is required — and `mocks`/`expected` are not in the schema at
  all.** The published schema declares exactly `input` (required) and
  `usedAsExample`; the other two validate purely because the eval item does not
  set `additionalProperties: false`. Practical consequence: a **typo in `mocks`
  or `expected` is silently accepted by `ray lint`** and only shows up as an eval
  that does not do what you meant. There is no schema safety net on the two keys
  that carry all the logic.
- **Set `usedAsExample: false` on the ugly ones.** Regression cases with
  contrived phrasing are worth keeping as tests and embarrassing as UI.
- Cover the read paths first, and phrase at least one to establish the boundary:
  *"Read the logs — don't fix anything yet."* teaches the user that this
  extension can also write, and teaches the model what restraint looks like.

Expectations, from the docs:

| Matcher | Meaning |
| --- | --- |
| `callsTool` | short form `{"callsTool": "get-todos"}`, or long form with `{ name, arguments }` |
| `includes` | case-insensitive substring of the answer |
| `matches` | regular expression |
| `meetsCriteria` | plain-text criteria, judged by AI |

Inside `callsTool`'s `arguments`, each value takes its own matcher — `eq`
(the default for a scalar), `includes`, `matches`, plus the combinators `and`
(the default when you pass an array), `or`, `not`. Dot notation reaches into
nested arguments (`"user.name": "thomas"`). A real one, from
`raycast/extensions`:

```json
{ "callsTool": { "name": "search-emojis",
                 "arguments": { "query": { "includes": "happy" } } } }
```

## Running evals

```bash
npx ray evals                 # build, then run every eval
npx ray evals --only 0-2      # ranges and lists: "0-2", "1", "1,2,3"
npx ray evals --skipBuild     # reuse the last build
```

**This is a manual step, not a gate step.** It builds the extension and POSTs to
`ai-evals.raycast.com`, so it needs AI access and network and cannot join
`tsc --noEmit && ray lint && ray build` in a `just check` recipe that has to work
offline and on a fresh clone.

Run it when tool descriptions or `instructions` change — those are the only edits
that can regress tool *selection*, and nothing else in the toolchain tests
selection at all. A tool that typechecks, lints, builds, and is never chosen is
the failure mode evals exist for.

## The AI API

Distinct from tools: this is *your* code calling a model, in a command.

```ts
import { AI, environment } from "@raycast/api";

if (!environment.canAccess(AI)) return fallback();

const answer = await AI.ask("Summarise this build log:\n" + log, {
  model: AI.Model["Anthropic_Claude_4.5_Haiku"],
  creativity: 0,        // 0–2
  signal: abort.signal,
});
```

The return value is a `Promise<string>` **and** an `EventEmitter` — subscribe to
`"data"` to stream into a `Detail` as it arrives rather than blocking on the
whole answer. Wrap it in `try/catch`: with no access, the call throws.

In a React command, prefer `useAI` from `@raycast/utils`, which is the same
call wired into the `usePromise` state machine:

```tsx
const { data, isLoading, error, revalidate } = useAI(prompt, {
  creativity: 0,
  stream: true,           // update as tokens arrive, not once at the end
  execute: Boolean(prompt),
  failureToastOptions: { title: "Could not summarise the log" },
});
```

Two details from the typings that the prose docs do not spell out:

- **`useAI` has no `abortable`.** Its options are
  `Omit<PromiseOptions<…>, "abortable">`, so the escape hatch this skill
  recommends for every other fetch is unavailable here. A superseded prompt keeps
  running; gate with `execute` instead, and keep prompts short enough that it
  does not matter.
- **`data` is typed `string`, not `string | undefined`** — it is `""` before the
  first token rather than absent, so `data && …` is the right emptiness check and
  `data === undefined` never fires.

`onError` defaults to logging and showing a generic failure toast; override it
with `failureToastOptions` rather than swallowing the error silently.

The AI namespace is small — `ask`, `AskOptions`, `Creativity`, `Model`, and
nothing else. Pin a `model` only when the task needs a specific capability; the
enum churns fast, and an id that disappears is a runtime failure in a code path
users rarely hit. Omitting `model` lets Raycast pick, which also respects a
user's Custom Provider or Ollama choice.

`AI.ask` is the wrong tool when the user is already in Quick AI — that is what
`tools[]` is for. Reach for it when the extension itself needs a model in the
middle of a deterministic flow, and give the user a way to see the raw input.
