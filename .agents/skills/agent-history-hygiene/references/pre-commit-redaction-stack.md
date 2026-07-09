# Pre-commit redaction stack

The three-layer defense agents can rely on when committing agent
transcripts + plan files.

```
  staged .md (transcript / plan)
            │
            ▼
  Layer 1: redact-agent-secrets   (pre-commit, local hook)
            │  rewrites file in place, re-stages via pre-commit
            ▼
  Layer 2: gitleaks-system         (pre-commit, github.com/gitleaks/gitleaks)
            │  blocks commit if a rule still matches
            ▼
  Layer 3: scan-staged.sh          (agent-invoked, belt-and-suspenders)
            │  run by the agent before `git commit` to branch on exit codes
            ▼
         git commit
```

Layers 1 and 2 are installed by `bootstrap-project.sh`. Layer 3 is the
wrapper agents are expected to call before committing so they can react
to structured exit codes without parsing pre-commit output.

## Layer 1: redact-agent-secrets

Implemented by `assets/redact_secrets.py`, wired as a `local` pre-commit
hook in `assets/pre-commit-config.yaml.template`. On each commit:

1. Pre-commit matches staged files against the `files:` regex
   (rendered from `assets/artifact-dirs.txt`).
2. `redact_secrets.py --fix` runs gitleaks against those files in staged
   mode, gathers findings, and replaces each literal secret with
   `first3...last3` (e.g. `sk-proj-abc...xyz`).
3. Private-key PEM blocks (`-----BEGIN ... PRIVATE KEY-----`) are
   replaced wholesale with `[REDACTED PRIVATE KEY BLOCK]`, and the
   literal string `PRIVATE KEY` becomes `PRIV***KEY` to stop the
   detect-private-key hook from firing downstream.
4. Any modified file is rewritten on disk. Pre-commit notices and exits
   non-zero with "files were modified by this hook" — the user then
   `git add`s the redacted files and recommits. Same UX as
   trailing-whitespace or end-of-file-fixer.

### Why redact (Layer 1) instead of block (Layer 2) for agent artifacts?

Because "blocking" a chat transcript on a leak is useless — the secret
is already in the file system, in the shell history, in the agent's
conversation context. The only defense left is "don't let it hit
`origin/`". Rewriting the file is the cheapest way to achieve that
while keeping the surrounding prose intact for future readers.

## Layer 2: gitleaks-system

The upstream `github.com/gitleaks/gitleaks` hook (pinned at `v8.22.1` in
the template). Runs after Layer 1 so it sees the redacted file. With
`.gitleaks.toml` in the repo root, custom rules apply automatically.

### Allowlist design

Two allowlists in `gitleaks.toml.template`:

- **Global** (`[allowlist]`): tolerates `*_REDACTED*` sentinels emitted
  by Layer 1, plus truncated example shapes like `sk-proj-abc...`.
- **Path-scoped** (`[[allowlists]]` with `paths`): inside agent
  artifact directories, tolerate example markers (`example-key`,
  `your-api-key-here`) and truncated example shapes. Both the **path
  AND a regex** must match — a real `sk-ant-api03-<95 chars>AA` inside
  a transcript still fires.

Real leaks take precedence over documentation because gitleaks evaluates
the finding's actual bytes, and truncated-example shapes don't have
enough entropy to match the strict rules in the first place.

## Layer 3: scan-staged.sh

Belt-and-suspenders wrapper:

```bash
bash /path/to/skills/local/agent-history-hygiene/scripts/scan-staged.sh
```

Runs `gitleaks git --staged` and translates the outcome into exit codes
agents can branch on:

| Exit | Meaning                              | Typical agent reaction                                  |
|-----:|--------------------------------------|---------------------------------------------------------|
|    0 | Clean                                | Proceed with `git commit`                               |
|   10 | Leaks found, `--redact` passed       | Run `redact_secrets.py --fix`, re-stage, re-run         |
|   20 | Leaks found, no redaction            | Rotate secret at provider, then redact + re-stage       |
|   30 | gitleaks not installed               | Surface install hint; fall back to bare pre-commit      |
|    2 | Not inside a git repo                | Abort or bootstrap a repo first                         |

Scripts always emit **JSON-lines findings on stdout** so the agent can
feed them to follow-up tooling without parsing prose.

## Keeping the bundled redactor in sync with chezmoi

The chezmoi version at `~/.local/share/chezmoi/scripts/redact_secrets.py`
is the **upstream source of truth**. The skill bundles a copy under
`assets/redact_secrets.py` so non-chezmoi users still get protection.

When chezmoi ships a fix, re-sync:

```bash
cp ~/.local/share/chezmoi/scripts/redact_secrets.py \
   $(git rev-parse --show-toplevel)/skills/local/agent-history-hygiene/assets/redact_secrets.py
git diff -- skills/local/agent-history-hygiene/assets/redact_secrets.py
# review, adjust DEFAULT_PATHS if the skill's list differs, commit.
```

The skill's copy intentionally widens `DEFAULT_PATHS` beyond chezmoi's
default (adds `.cursor/rules`, `.specify`, `.codex`). If chezmoi later
adopts the wider list, the two converge; otherwise keep the skill
version as the broader superset.

## Inline allowlist pragmas

For one-off false positives that don't deserve a config change, gitleaks
accepts inline `#gitleaks:allow` on the same line as the match. Use
sparingly — every pragma is an opportunity for a real secret to ride
along in review.

```
# Example call pattern:
# curl -H "Authorization: Bearer sk-proj-fake1234..." #gitleaks:allow
```

For whole-file exceptions, prefer adding a path to `.gitleaksignore`
(one glob per line, gitignore syntax).

## Cross-reference

- [`transcript-session-discovery.md`](./transcript-session-discovery.md)
  — how we locate which files to scan.
- [`remediation.md`](./remediation.md) — what to do when Layer 3 fires
  on an already-pushed commit.
- [`../assets/artifact-dirs.txt`](../assets/artifact-dirs.txt) — the
  shared source of truth for "which directories contain agent artifacts".
