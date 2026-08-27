
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

**Config schema:** when a release adds a `config.toml` field/default, bump
`config.SchemaVersion` (`internal/config/config.go`). On the next run a config with
`schema < SchemaVersion` is **auto-migrated** — `runRoot` re-saves it so new fields land at
their `Default()` values (existing values preserved) and the schema is re-stamped; no re-init
is needed for additive changes. A **breaking** change (renamed/removed/re-meaning a key) needs
an explicit migration in `runRoot` before the re-save. `Save` stamps schema + the writing app
version (`config.Generator`, set in `cmd.Execute`).

### Cutting a release

Pushing an annotated `vX.Y.Z` tag is the whole release. `.github/workflows/release.yml`
then does everything else on one `ubuntu-latest` runner:

1. GoReleaser cross-compiles six targets (darwin/linux/windows × amd64/arm64).
   The binary is pure Go, so `CGO_ENABLED=0` covers them all from Linux — no
   docker images, no macOS/Windows runners, no self-hosted hardware.
2. It publishes the GitHub Release: six archives (each containing the binary,
   `LICENSE`, `README.md`, and generated `completions/`) plus `checksums.txt`.
3. It pushes the Scoop manifest to `daviddwlee84/scoop-bucket`.
4. `scripts/bump-formula.sh` renders `packaging/translate.rb.tmpl` from those
   checksums and pushes it to `daviddwlee84/homebrew-tap`.

So the steps are:

1. `go test ./cmd/... ./internal/...` green, working tree clean.
2. `git push origin main`
3. `git tag -a vX.Y.Z -m "<highlights>"` && `git push origin vX.Y.Z`
4. Watch the run: `gh run watch`. Verify with `gh release view vX.Y.Z`.
5. Install on this machine: **macOS** `brew upgrade daviddwlee84/tap/translate`;
   **Windows** `scoop update translate`; **Linux** `just upgrade-go` in the
   dotfiles repo (still `go install @latest` there). The `go_tools` pin in
   `dot_ansible/roles/go_tools/defaults/main.yml` is only the *fresh-install
   floor* — don't bump it for upgrades. Make sure no stale copy shadows the
   installed one earlier on `PATH`.

**Never hand-edit `Formula/translate.rb` in the tap.** It is a generated artifact
of `packaging/translate.rb.tmpl`; the next release overwrites it. Same for
`bucket/translate.json` in the scoop bucket, which GoReleaser owns.

### One-time setup (do this before the first tag)

Everything here is scriptable with `gh` **except minting the token** — GitHub
removed the PAT-creation API on purpose (`POST /authorizations` now 404s), so
that one step is browser-only.

1. Create a **fine-grained PAT** at
   <https://github.com/settings/personal-access-tokens/new>:
   - *Repository access* → **Only select repositories** → `daviddwlee84/homebrew-tap`
     **and** `daviddwlee84/scoop-bucket`
   - *Permissions* → *Repository permissions* → **Contents: Read and write**
   - Set an expiry you will actually notice; the release job fails loudly when it
     lapses.
2. Store it (this part *is* `gh`):

   ```sh
   gh secret set TAP_GITHUB_TOKEN --repo daviddwlee84/translate
   ```

3. Verify: `gh secret list --repo daviddwlee84/translate`.

The workflow's default `GITHUB_TOKEN` cannot substitute — it is scoped to this
repository and cannot push to the tap or the bucket.

Before tagging, `goreleaser check` validates `.goreleaser.yaml` (CI runs it on
every PR too). `goreleaser release --snapshot --clean` does a full local dry run
into `dist/` without publishing anything.

There is no CHANGELOG; the annotated tag message is the release note — the
workflow feeds it to GoReleaser via `--release-notes`, falling back to the commit
changelog when a tag has no message body. Keep commit subjects in
`feat(scope): …` / `fix(scope): …` form so `git log <prev-tag>..<tag>` reads as a
coherent changelog.

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
