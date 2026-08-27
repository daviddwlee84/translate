#!/usr/bin/env bash
# Render packaging/translate.rb.tmpl from a release's checksums and push it to
# the Homebrew tap.
#
# GoReleaser cannot do this for us: `brews:` (formulae) was deprecated in v2.10
# in favour of `homebrew_casks:`, and a cask gets the com.apple.quarantine
# attribute, which breaks an unsigned binary with "translate is damaged and
# cannot be opened". A formula does not. So we template the formula ourselves.
#
# Usage:
#   scripts/bump-formula.sh --version v0.6.0 [--checksums dist/checksums.txt]
#                           [--out-file FILE] [--dry-run]
#
# Flags:
#   --version VER      Tag to publish, with or without the leading "v". Required.
#   --checksums FILE   goreleaser checksum file. Default: dist/checksums.txt
#   --template FILE    Formula template. Default: packaging/translate.rb.tmpl
#   --out-file FILE    Write the rendered formula here instead of pushing.
#   --dry-run          Render to stdout; never clone or push.
#   -h, --help         This message.
#
# Environment (push mode only):
#   TAP_GITHUB_TOKEN   PAT with contents:write on the tap repo. Required.
#                      A workflow's default GITHUB_TOKEN cannot push cross-repo.
#   TAP_REPO           Default: daviddwlee84/homebrew-tap
#
# Exit codes:
#   0  formula rendered (and pushed, unless --dry-run/--out-file)
#   1  bad usage, or a required flag is missing
#   2  a required checksum was not found, or a placeholder went unsubstituted
#   3  push failed (clone, commit, or push)
set -euo pipefail

TEMPLATE="packaging/translate.rb.tmpl"
CHECKSUMS="dist/checksums.txt"
TAP_REPO="${TAP_REPO:-daviddwlee84/homebrew-tap}"
VERSION=""
OUT_FILE=""
DRY_RUN=0

die() { printf 'bump-formula: %s\n' "$1" >&2; exit "${2:-1}"; }

while [ $# -gt 0 ]; do
    case "$1" in
        --version)   VERSION="${2:-}"; shift 2 ;;
        --checksums) CHECKSUMS="${2:-}"; shift 2 ;;
        --template)  TEMPLATE="${2:-}"; shift 2 ;;
        --out-file)  OUT_FILE="${2:-}"; shift 2 ;;
        --dry-run)   DRY_RUN=1; shift ;;
        -h|--help)   sed -n '2,31p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *)           die "unknown argument: $1" ;;
    esac
done

[ -n "$VERSION" ]   || die "--version is required"
[ -f "$TEMPLATE" ]  || die "template not found: $TEMPLATE"
[ -f "$CHECKSUMS" ] || die "checksum file not found: $CHECKSUMS"

# Accept v0.6.0 or 0.6.0; the template re-adds the "v" where a URL needs it.
BARE_VERSION="${VERSION#v}"

# Pull one archive's sha256 out of the goreleaser checksum file.
sha_for() {
    local archive="$1" sum
    sum="$(awk -v want="$archive" '$2 == want { print $1 }' "$CHECKSUMS")"
    [ -n "$sum" ] || { printf 'bump-formula: no checksum for %s\n' "$archive" >&2; return 1; }
    printf '%s' "$sum"
}

rendered="$(cat "$TEMPLATE")"
rendered="${rendered//__VERSION__/$BARE_VERSION}"
for pair in \
    "DARWIN_ARM64:translate_${BARE_VERSION}_darwin_arm64.tar.gz" \
    "DARWIN_AMD64:translate_${BARE_VERSION}_darwin_amd64.tar.gz" \
    "LINUX_ARM64:translate_${BARE_VERSION}_linux_arm64.tar.gz" \
    "LINUX_AMD64:translate_${BARE_VERSION}_linux_amd64.tar.gz"
do
    key="${pair%%:*}"
    archive="${pair#*:}"
    sum="$(sha_for "$archive")" || die "checksum lookup failed for $archive" 2
    rendered="${rendered//__SHA256_${key}__/$sum}"
done

case "$rendered" in
    *__SHA256_*|*__VERSION__*) die "template still has unsubstituted placeholders" 2 ;;
esac

# Drop the template-only header comment (everything before the class line).
case "$rendered" in
    *$'\n'"class Translate"*)
        rendered="class Translate${rendered#*$'\n'class Translate}" ;;
esac

if [ "$DRY_RUN" = 1 ]; then
    printf '%s\n' "$rendered"
    exit 0
fi

if [ -n "$OUT_FILE" ]; then
    mkdir -p "$(dirname "$OUT_FILE")"
    printf '%s\n' "$rendered" > "$OUT_FILE"
    printf 'bump-formula: wrote %s\n' "$OUT_FILE" >&2
    exit 0
fi

[ -n "${TAP_GITHUB_TOKEN:-}" ] || die "TAP_GITHUB_TOKEN is required to push" 3

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

git clone --depth 1 \
    "https://x-access-token:${TAP_GITHUB_TOKEN}@github.com/${TAP_REPO}.git" \
    "$workdir/tap" >/dev/null 2>&1 || die "could not clone $TAP_REPO" 3

mkdir -p "$workdir/tap/Formula"
printf '%s\n' "$rendered" > "$workdir/tap/Formula/translate.rb"

cd "$workdir/tap"
if git diff --quiet -- Formula/translate.rb; then
    printf 'bump-formula: formula already at %s; nothing to push\n' "$BARE_VERSION" >&2
    exit 0
fi

git -c user.name="goreleaser-bot" -c user.email="bot@goreleaser.com" \
    commit -am "translate ${BARE_VERSION}" >/dev/null || die "commit failed" 3
git push >/dev/null 2>&1 || die "push to $TAP_REPO failed" 3

printf 'bump-formula: pushed translate %s to %s\n' "$BARE_VERSION" "$TAP_REPO" >&2
