#!/usr/bin/env bash
# bootstrap-project.sh — wire pre-commit + gitleaks + the redact-agent-
# secrets hook into a new project so agent chat/plan files can be
# committed safely.
#
# Bash 3.2 compatible (stock macOS).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SKILL_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ASSETS_DIR="$SKILL_DIR/assets"

CHEZMOI_SRC="${CHEZMOI_SOURCE_DIR:-$HOME/.local/share/chezmoi}"

usage() {
  cat <<'EOF'
Usage: bootstrap-project.sh [OPTIONS]

Install agent-history-hygiene's pre-commit stack into the current repo:
  .pre-commit-config.yaml   + redact-agent-secrets + gitleaks-system
  .gitleaks.toml            (portable subset; real leaks still fire)
  scripts/redact_secrets.py (bundled unless --from-chezmoi)
  .git/hooks/pre-commit     (via `pre-commit install`)

Options:
  --from-chezmoi      Symlink .pre-commit-config.yaml + .gitleaks.toml +
                      redact_secrets.py from your chezmoi source
                      ($HOME/.local/share/chezmoi) so updates propagate.
                      Fails if chezmoi source is missing.
  --install-hook      Also install a prepare-commit-msg hook that calls
                      stage-agent-artifacts.sh --session-only on every
                      commit (opt-in; surprises users who commit manually).
  --force             Overwrite existing files instead of skipping.
  --dry-run           Print what would happen without making changes.
  --help, -h          Show this help and exit.

Exit codes:
  0  success
  1  invalid arguments
  2  not inside a git repo
  3  chezmoi source missing (and --from-chezmoi was requested)
  4  pre-commit tool unavailable and `uvx` fallback failed
EOF
}

log()  { printf '%s\n' "$*" >&2; }
warn() { printf 'warn: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit "${2:-1}"; }

FROM_CHEZMOI=0
INSTALL_HOOK=0
FORCE=0
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --from-chezmoi)  FROM_CHEZMOI=1; shift ;;
    --install-hook)  INSTALL_HOOK=1; shift ;;
    --force)         FORCE=1; shift ;;
    --dry-run)       DRY_RUN=1; shift ;;
    --help|-h)       usage; exit 0 ;;
    -*)              die "unknown flag: $1 (try --help)" 1 ;;
    *)               die "unexpected positional arg: $1 (try --help)" 1 ;;
  esac
done

# Must be inside a git repo (or the pre-commit install step fails anyway).
if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
  die "not inside a git repo" 2
fi
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if [ "$FROM_CHEZMOI" = "1" ] && [ ! -d "$CHEZMOI_SRC" ]; then
  die "chezmoi source not found at $CHEZMOI_SRC (skip --from-chezmoi to use bundled copies)" 3
fi

dryrun_say() {
  [ "$DRY_RUN" = "1" ] && log "[dry-run] $*"
}

# install_file <dest> <src_copy> <src_chezmoi>
install_file() {
  local dest="$1" src_copy="$2" src_chezmoi="$3"
  local src
  if [ "$FROM_CHEZMOI" = "1" ]; then
    src="$src_chezmoi"
    [ -e "$src" ] || die "missing upstream file: $src" 3
  else
    src="$src_copy"
    [ -e "$src" ] || die "missing bundled asset: $src" 1
  fi

  if [ -e "$dest" ] && [ "$FORCE" != "1" ]; then
    log "skip: $dest already exists (use --force to overwrite)"
    return 0
  fi

  mkdir -p "$(dirname "$dest")"

  if [ "$DRY_RUN" = "1" ]; then
    if [ "$FROM_CHEZMOI" = "1" ]; then
      log "[dry-run] ln -sf $src -> $dest"
    else
      log "[dry-run] cp $src -> $dest"
    fi
    return 0
  fi

  if [ "$FROM_CHEZMOI" = "1" ]; then
    ln -sf "$src" "$dest"
    log "linked: $dest -> $src"
  else
    cp "$src" "$dest"
    log "wrote: $dest"
  fi
}

# 1. .pre-commit-config.yaml
install_file ".pre-commit-config.yaml" \
  "$ASSETS_DIR/pre-commit-config.yaml.template" \
  "$CHEZMOI_SRC/.pre-commit-config.yaml"

# 2. .gitleaks.toml
install_file ".gitleaks.toml" \
  "$ASSETS_DIR/gitleaks.toml.template" \
  "$CHEZMOI_SRC/.gitleaks.toml"

# 3. scripts/redact_secrets.py
install_file "scripts/redact_secrets.py" \
  "$ASSETS_DIR/redact_secrets.py" \
  "$CHEZMOI_SRC/scripts/redact_secrets.py"

# Make the redactor executable (cp loses the bit on some filesystems).
if [ "$DRY_RUN" = "0" ] && [ -f "scripts/redact_secrets.py" ] && [ ! -L "scripts/redact_secrets.py" ]; then
  chmod +x "scripts/redact_secrets.py"
fi

# 4. `pre-commit install`. Prefer a local `pre-commit` binary; fall back to
#    `uvx pre-commit@4` so users without a manual install still get hooks.
#    Skipped if `core.hooksPath` is set globally (chezmoi pattern) — the
#    global hook wrapper already runs `.pre-commit-config.yaml` for us.
install_hooks() {
  if [ "$DRY_RUN" = "1" ]; then
    log "[dry-run] pre-commit install (or skip if core.hooksPath set)"
    return 0
  fi

  # If `core.hooksPath` is set at any scope AND it points somewhere
  # outside `.git/hooks/`, `pre-commit install` refuses to clobber it.
  # In that case assume the user's global hook wrapper runs
  # `.pre-commit-config.yaml` (the chezmoi pattern) and skip install.
  local hooks_path
  hooks_path="$(git config --get core.hooksPath 2>/dev/null || true)"
  if [ -n "$hooks_path" ]; then
    log "core.hooksPath is set to '$hooks_path' — skipping \`pre-commit install\`."
    log "  Your global hook wrapper at '$hooks_path/pre-commit' should run"
    log "  the repo's .pre-commit-config.yaml automatically."
    log "  To use per-repo hooks instead, run: git config --unset core.hooksPath"
    return 0
  fi

  if command -v pre-commit >/dev/null 2>&1; then
    pre-commit install
    return 0
  fi
  if command -v uvx >/dev/null 2>&1; then
    log "pre-commit not found — using uvx fallback"
    uvx pre-commit@4 install
    return 0
  fi
  warn "neither pre-commit nor uvx available; install pre-commit manually"
  warn "  macOS: brew install pre-commit"
  warn "  or:    pipx install pre-commit"
  return 4
}
install_hooks || rc=$?
if [ "${rc:-0}" != "0" ]; then
  exit "$rc"
fi

# 5. Optional prepare-commit-msg hook that auto-stages the current session.
if [ "$INSTALL_HOOK" = "1" ]; then
  hook_path=".git/hooks/prepare-commit-msg"
  hook_body="#!/usr/bin/env bash
# Installed by agent-history-hygiene bootstrap-project.sh --install-hook.
# Auto-stage the current SpecStory transcript + newest .claude/plans/*.md
# with every commit. Remove this file to disable.
set -eu
bash '$SCRIPT_DIR/stage-agent-artifacts.sh' --session-only --allow-empty >/dev/null 2>&1 || true
"
  if [ "$DRY_RUN" = "1" ]; then
    log "[dry-run] write $hook_path + chmod +x"
  else
    if [ -e "$hook_path" ] && [ "$FORCE" != "1" ]; then
      warn "$hook_path already exists (use --force to overwrite)"
    else
      printf '%s' "$hook_body" > "$hook_path"
      chmod +x "$hook_path"
      log "installed: $hook_path (auto-stage current session)"
    fi
  fi
fi

# 6. Audit .gitignore / .git/info/exclude — warn if any artifact dir is
#    silently hidden so pre-commit won't actually see those files.
audit_ignore() {
  local dirs_file="$ASSETS_DIR/artifact-dirs.txt"
  [ -f "$dirs_file" ] || return 0
  local dir
  while IFS= read -r line; do
    line="${line%%#*}"
    line="$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    [ -z "$line" ] && continue
    dir="$line"
    # check-ignore returns 0 when the path matches an ignore rule.
    if git check-ignore -q "$dir" 2>/dev/null; then
      warn "$dir is gitignored — staged files under it won't be committed"
      warn "  Check .gitignore and .git/info/exclude for a matching rule."
    fi
  done < "$dirs_file"
}
audit_ignore

# 7. Verify ~/.claude/settings.json has plansDirectory set. Don't silently
#    edit — print the patch for the user.
check_plans_directory() {
  local settings="$HOME/.claude/settings.json"
  [ -f "$settings" ] || {
    log ""
    log "note: ~/.claude/settings.json not found. To keep Claude Code plan"
    log "      files inside each project, create it with:"
    log ""
    log '      { "plansDirectory": "./.claude/plans" }'
    log ""
    return 0
  }
  if grep -q '"plansDirectory"' "$settings"; then
    return 0
  fi
  log ""
  log "note: ~/.claude/settings.json exists but does NOT set plansDirectory."
  log '      Add:  "plansDirectory": "./.claude/plans"'
  log "      so Claude Code writes plan files inside each project (committable"
  log "      with the feature diff)."
  log ""
}
check_plans_directory

log ""
log "Bootstrap complete. Recommended next steps:"
log "  1. Review .pre-commit-config.yaml + .gitleaks.toml for project-specific tweaks."
log "  2. pre-commit run --all-files      # shake out any existing issues"
log "  3. Have the agent commit code + chat together; scan with:"
log "     bash $SCRIPT_DIR/scan-staged.sh"
