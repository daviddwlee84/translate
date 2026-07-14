
## Releasing & versioning

`translate --version` is derived from Go build info (`cmd/version.go` →
`debug.ReadBuildInfo`), so **the version *is* the git tag** — there is no version
constant to edit. `go install …@latest` and the chezmoi/ansible pin both resolve
to a published **tag**, not to `main`; a commit on `main` does **not** reach an
installed binary until it is pushed **and** tagged.

SemVer, pre-1.0 (`0.y.z`):

- backward-compatible feature (new mode/flag, e.g. `--learn`) → bump **minor** (`0.1.0 → 0.2.0`)
- bug fix only → bump **patch** (`0.2.0 → 0.2.1`)
- breaking CLI/flag/config change → still a minor bump while `0.x`; call it out in the tag message
- cut **`v1.0.0`** only once the CLI/flags/config surface is considered stable

To ship the current `main` to this machine via chezmoi:

1. `go test ./...` green, working tree clean.
2. `git push origin main`
3. `git tag -a vX.Y.Z -m "<highlights>"` && `git push origin vX.Y.Z`
4. verify: `go install github.com/daviddwlee84/translate@vX.Y.Z && translate --version` → `vX.Y.Z`
5. install on this machine (dotfiles repo): run `just upgrade-go` — it installs each go
   tool at `@latest` (= the new tag) into `~/.local/bin`. The `go_tools` pin in
   `dot_ansible/roles/go_tools/defaults/main.yml` is only the *fresh-install floor*
   (its header says **don't** bump it for upgrades); `chezmoi apply`/ansible is
   install-only and won't move an already-installed binary. Make sure no stale copy
   shadows `~/.local/bin` earlier on `PATH` (e.g. a hand-built one in `~/.dotfiles/bin`).

There is no CHANGELOG; the annotated tag message is the release note. Keep commit
subjects in `feat(scope): …` / `fix(scope): …` form so `git log <prev-tag>..<tag>`
reads as a coherent changelog.

<!-- project-knowledge-harness:agent-guidance -->
<!-- Snippet for the project's agent contract file (AGENTS.md / CLAUDE.md /
     similar). The bundled scripts/init.sh appends this between sentinel
     markers; safe to re-run. -->

### Long-term backlog → `TODO.md` + `backlog/`

When the user surfaces an idea explicitly **not** being implemented this
session (signals: "maybe later", "nice to have", "if I'm interested",
"工程量太大需要再評估", "先記下來"), add an entry to [`TODO.md`](TODO.md) using
the priority + effort tag schema. Do **not** create new `ROADMAP.md` /
`IDEAS.md` / `BACKLOG.md` files — `TODO.md` is the single index.

The bundled `scripts/todo-kanban.sh` validates the format. Run it
(`scripts/todo-kanban.sh --validate-only TODO.md`) after editing so syntax
drift is caught immediately.

#### Three ways to add a TODO entry (preferred order)

1. **Structured CLI — `scripts/add-todo.sh`** (default):

   ```
   scripts/add-todo.sh --priority P3 --effort M \
     --title "Title" --description "Description"
   ```

   Inserts a canonically-formatted line into the right `## P*` lane and
   re-runs the validator. Add `--backlog` to also scaffold
   `backlog/<slug>.md` from the bundled template.

2. **Quick capture — `backlog/inbox.md`** (when priority/effort unclear):

   ```
   echo "- maybe add docs versioning with mike" >> backlog/inbox.md
   ```

   When the user asks "sweep the inbox", run
   `scripts/sweep-inbox.sh`. It prompts for the missing fields per loose
   line and calls `add-todo.sh`. Use `--batch` for non-interactive runs
   that only formalize lines with parseable `key=value` pairs.

3. **Direct edit of `TODO.md`** — fine if the format is fresh; run
   `scripts/todo-kanban.sh --validate-only` afterwards.

Add a `backlog/<slug>.md` companion doc when the item meets any of:

- carries a `P?` tag (record what was tried so it doesn't need re-investigation)
- captures a paused troubleshooting session that you intend to fix later
  (preserve the error trace + root cause analysis before context evaporates)
- weighs multiple options (record trade-offs, not only the winner)
- is `[L]` or `[XL]` (architectural; needs design before code)

`[S]` items rarely need a backlog doc — a file path in the `TODO.md` line is
usually enough. See [`backlog/README.md`](backlog/README.md) for the full
template and "when to add a doc" rules.

When implementing a `TODO.md` item, in the same commit:

1. Run `scripts/promote-todo.sh --title "<substring>" --summary "<what shipped>"`
   to move the entry into `## Done` with the dated syntax and re-validate.
2. Mark the corresponding `backlog/<slug>.md` (if any) `Status: shipped`
   and keep it as a historical record (don't delete — future-you may
   revisit adjacent decisions).

`backlog/` is excluded from N/A (no packaging — these files stay in the repo) (see N/A); it
is repo metadata for maintainers, not user-facing config to deploy.

### Past pitfalls → `pitfalls/`

When you spend more than ~15 minutes debugging something that wasn't
googleable and the fix is non-obvious, write a `pitfalls/<slug>.md`
capturing:

1. **Verbatim symptom** — copy-paste error messages exactly, do not
   paraphrase (preserves grep-ability for future-you / future agent)
2. **Root cause** — why this happens (with source / docs / upstream issue link)
3. **Workaround** — copy-pasteable commands or config diff
4. **Prevention** — how to avoid stepping on this again

Title the doc by the **symptom**, not the root cause (you'll search by what
you're seeing, not by what you eventually learned). See
[`pitfalls/README.md`](pitfalls/README.md) for the full template and
when-to-add rules.

**Pitfall vs Hard invariant**: a pitfall *graduates* to a Hard invariant in
this file when it (a) recurs across machines/agents/sessions despite being
documented, (b) silently corrupts state, or (c) the workaround is non-obvious
enough that "remember to do X" isn't safe. When graduating, leave the
`pitfalls/<slug>.md` as historical record and link to it from the new
invariant.

`pitfalls/` is excluded from N/A (no packaging — these files stay in the repo) (see N/A) and
**not** auto-redacted; review for secrets before committing.
<!-- project-knowledge-harness:agent-guidance --> (end)
