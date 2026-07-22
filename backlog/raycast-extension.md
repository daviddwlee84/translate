# Publish the Raycast extension to the store

**Status**: P? — local-dev shipped; publish deferred
**Effort**: L
**Related**: `TODO.md` P? · `raycast/extension/` · [../docs/raycast-extension.md](../docs/raycast-extension.md) · [homebrew-distribution.md](homebrew-distribution.md)

## Context

2026-07. The Raycast integration shipped as two local tracks (`raycast/script-commands/`
bash + `raycast/extension/` TypeScript). The TS extension runs via `just raycast-dev`
(`ray develop`) and **persists in root search after dev stops**, so personal use needs
no store at all. The open question is whether — and how — to distribute it beyond this
machine.

## Investigation

- **Calling a user-installed binary is a sanctioned pattern** ("✅ Calling known system
  binaries"). The store's hard rule targets *bundling opaque/heavy binaries*, which we
  don't do — we shell out to a Homebrew/`go install` `translate`.
- **Soft guideline friction**: the store discourages requiring users to manually install
  external tools ("avoid... automate where possible"). Our binary dependency is exactly
  that — advisory, not an auto-reject, but the most likely reviewer comment. Mitigate
  with graceful binary detection + a "not installed → install instructions" onboarding
  view + README.
- **Private org store** (Raycast for Teams/Pro): publish via `owner` in package.json +
  `npm run publish`; visible only to org members, **no public human review**. Best fit if
  distributing to a controlled group who all have the binary.
- **Public store requirements**: `author` = a *registered Raycast username* (currently
  fails `ray lint` — `daviddwlee84` 404s against Raycast's user API), `license: MIT`
  (wrapper only; the Go binary's license is untouched since we don't bundle it), a custom
  512×512 icon (the current one is a placeholder), ≥3 screenshots (2000×1250), a
  `CHANGELOG.md`, a Title-Case category, `package-lock.json`, and `ray lint` clean.
- **No telemetry, no Keychain**. If published, expose the openrouter API key via the
  Raycast preferences API (`required: true`), not by reading `config.toml`.
- **Platform**: mark `platforms: ["macOS"]` (already set) — the binary shell-out doesn't
  work on Raycast's iOS/Windows clients.

## Options considered

| Option | What | Verdict |
|---|---|---|
| A. Local dev only (personal) | `ray develop`, per-machine, no review | current default — done |
| B. Private org store (Pro/Team) | `owner` + `npm run publish`, internal-only | best if sharing to a team; needs Pro |
| C. Public store | PR to raycast/extensions, automated + human review | widest reach; expect binary-dependency review friction |

## Current blocker / open questions

- Is public publication worth it given the crowded category, or is local/private enough?
  (Differentiators are real — see [../docs/raycast-extension.md](../docs/raycast-extension.md)
  competitive section — but the binary dependency is a review headwind.)
- What's the binary-not-found onboarding UX (detection + actionable install view)?
- Register/confirm the Raycast `author` handle before any publish (blocks `ray lint`).

## Decision (if any)

`2026-07 deferred` — ship the local-dev extension first and dogfood it; **private/
personal is the current path**, public store stays deferred.

Publish prerequisites now IN PLACE: registered author (`da-wei_lee`), `license: MIT`,
a designed 512×512 icon (`assets/extension-icon.png`, source `.svg`), `CHANGELOG.md`,
an extension `README.md`, and a graceful binary-not-found onboarding view. Remaining
before a public PR: **3–6 screenshots (2000×1250, GUI capture)** and a decision on the
binary-dependency review friction (a private org store sidesteps it entirely). The
full dev→publish process is documented in
[../docs/raycast-extension.md](../docs/raycast-extension.md) "Publishing & distribution".

## References

- Prepare for Store: https://developers.raycast.com/basics/prepare-an-extension-for-store
- Publish: https://developers.raycast.com/basics/publish-an-extension
- Teams / private extensions: https://developers.raycast.com/teams/publish-a-private-extension
