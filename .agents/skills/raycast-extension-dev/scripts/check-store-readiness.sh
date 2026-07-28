#!/usr/bin/env bash
# Store-readiness checks that `ray lint` does not run.
#
# Verified motivation: `ray lint` exits 0 with a completely empty `metadata/`.
# Screenshot count and dimensions, icon size, the CHANGELOG placeholder, and a
# real author are review-time requirements the linter never touches.
set -euo pipefail

VERSION="1.1.0"
PLACEHOLDER_SHA256="8a506edd828a47487e85d5279089305fd8d531eaa790b1692b8c2f8b0c40b24a"

usage() {
  cat <<'EOF'
Usage: check-store-readiness.sh [DIR] [OPTIONS]

Check a Raycast extension against the Raycast Store requirements that
`ray lint` does NOT validate. Run `ray lint`, `tsc --noEmit`, and
`ray build -e dist` separately — this script does not duplicate them.

Arguments:
  DIR                 Extension directory (default: current directory)

Options:
  --json              Emit a JSON array of {id,status,detail,fix} (default)
  --quiet             Emit nothing on stdout; use the exit code only
  --strict            Treat warnings as failures
  --version           Print the script version and exit
  --help, -h          Show this help and exit

Statuses:
  pass                the requirement is met
  warn                probably fine, but cannot be proven locally
  fail                the requirement is not met
  skip                a required external tool is missing; NOT a pass

Examples:
  check-store-readiness.sh .
  check-store-readiness.sh ./my-ext --strict
  check-store-readiness.sh . | node -e 'JSON.parse(require("fs").readFileSync(0)).filter(c=>c.status!=="pass").forEach(c=>console.log(c.id,c.detail))'

Exit codes:
  0  every check passed (warnings allowed unless --strict)
  1  invalid arguments
  2  DIR is not a Raycast extension (no package.json with a Raycast manifest)
  3  a required external tool is missing, so a check was skipped
  4  one or more readiness checks failed
EOF
}

die() { printf '%s\n' "$*" >&2; exit 1; }

DIR="."
MODE="json"
STRICT=0
DIR_SET=0

while [ $# -gt 0 ]; do
  case "$1" in
    --json)    MODE="json" ;;
    --quiet)   MODE="quiet" ;;
    --strict)  STRICT=1 ;;
    --version) printf '%s\n' "$VERSION"; exit 0 ;;
    --help|-h) usage; exit 0 ;;
    -*)        usage >&2; die "unknown flag: $1 (see --help)" ;;
    *)
      [ "$DIR_SET" -eq 1 ] && { usage >&2; die "unexpected argument: $1 (DIR was already set to $DIR)"; }
      DIR="$1"; DIR_SET=1 ;;
  esac
  shift
done

[ -d "$DIR" ] || die "not a directory: $DIR"
[ -f "$DIR/package.json" ] || { printf 'no package.json in %s — not a Raycast extension\n' "$DIR" >&2; exit 2; }

command -v node >/dev/null 2>&1 || {
  printf 'node is required to read package.json, and a Raycast extension cannot be built without it\n' >&2
  exit 3
}

MANIFEST_OK=$(node -e '
  try { const m = require(process.argv[1]); process.stdout.write(m.commands ? "yes" : "no"); }
  catch (e) { process.stdout.write("bad"); }
' "$(cd "$DIR" && pwd)/package.json")

case "$MANIFEST_OK" in
  yes) ;;
  bad) printf '%s/package.json is not valid JSON\n' "$DIR" >&2; exit 2 ;;
  *)   printf '%s/package.json has no commands[] — not a Raycast extension manifest\n' "$DIR" >&2; exit 2 ;;
esac

# node -p over the manifest. Prints the empty string for a missing value.
q() { node -p "try{const m=require('$(cd "$DIR" && pwd)/package.json');String($1??'')}catch(e){''}"; }

FAILED=0; WARNED=0; SKIPPED=0
RESULTS=""

esc() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/	/ /g'; }

record() { # id status detail fix
  case "$2" in
    fail) FAILED=$((FAILED + 1)) ;;
    warn) WARNED=$((WARNED + 1)) ;;
    skip) SKIPPED=$((SKIPPED + 1)) ;;
  esac
  RESULTS="${RESULTS}${RESULTS:+,}
  {\"id\":\"$(esc "$1")\",\"status\":\"$2\",\"detail\":\"$(esc "$3")\",\"fix\":\"$(esc "$4")\"}"
}

# --- manifest ---------------------------------------------------------------

NAME_FIELD=$(q "m.name")

AUTHOR=$(q "m.author")
case "$AUTHOR" in
  "")            record author-set fail "author is empty" "Set author to your registered Raycast username" ;;
  __AUTHOR__|me|your-username)
                 record author-set fail "author is still the placeholder \"$AUTHOR\"" "Set author to your registered Raycast username" ;;
  *)             record author-set warn "author is \"$AUTHOR\" — registration cannot be verified locally" "Confirm this username exists at raycast.com" ;;
esac

LICENSE_FIELD=$(q "m.license")
if [ "$LICENSE_FIELD" = "MIT" ]; then
  record license-field pass "license is MIT" ""
else
  record license-field fail "license is \"$LICENSE_FIELD\", expected MIT" "Set \"license\": \"MIT\" in package.json"
fi

if [ -f "$DIR/LICENSE" ] || [ -f "$DIR/LICENSE.md" ] || [ -f "$DIR/LICENSE.txt" ]; then
  record license-file pass "a LICENSE file is present" ""
else
  record license-file fail "no LICENSE file at the extension root" "Add an MIT LICENSE file"
fi

CATEGORIES=$(q "(m.categories||[]).length")
if [ "${CATEGORIES:-0}" -ge 1 ] 2>/dev/null; then
  record categories-nonempty pass "$CATEGORIES categor(y|ies) declared" ""
else
  record categories-nonempty fail "categories is empty or missing" "Add at least one Title Case category from Raycast's list"
fi

DESCRIPTION=$(q "m.description")
if [ -n "$DESCRIPTION" ]; then
  record description-present pass "description is set" ""
else
  record description-present fail "description is empty" "Add a one-sentence description — the store shows it"
fi

VERSION_FIELD=$(q "m.version")
if [ -z "$VERSION_FIELD" ]; then
  record no-version-field pass "no version field, as the store expects" ""
else
  record no-version-field warn "package.json declares version \"$VERSION_FIELD\"" "Remove it — the store derives the version"
fi

HAS_MENUBAR=$(q "(m.commands||[]).some(c=>c.mode==='menu-bar')?'yes':'no'")
PLATFORMS_SET=$(q "Array.isArray(m.platforms)?'yes':'no'")
HAS_MACOS=$(q "(m.platforms||[]).includes('macOS')?'yes':'no'")
HAS_WINDOWS=$(q "(m.platforms||[]).includes('Windows')?'yes':'no'")

# platforms is extension-level — no per-command form — so one menu-bar command
# takes the whole extension off Windows. An absent platforms field is left alone
# here: the changelog says it defaults to ["macOS"] and the JSON schema says it
# defaults to every platform, so `platforms-declared` flags the ambiguity instead.
if [ "$HAS_MENUBAR" = "yes" ] && [ "$HAS_WINDOWS" = "yes" ]; then
  record platforms-menu-bar-exclusive fail "a menu-bar command exists but platforms lists Windows" \
    "Menu bar is macOS-only and platforms has no per-command form — use [\"macOS\"], or move the menu-bar command to its own extension"
elif [ "$HAS_MENUBAR" = "yes" ] && [ "$PLATFORMS_SET" = "yes" ] && [ "$HAS_MACOS" != "yes" ]; then
  record platforms-menu-bar-exclusive fail "a menu-bar command exists but platforms does not list macOS" \
    "Set \"platforms\": [\"macOS\"] — menu bar exists nowhere else"
else
  record platforms-menu-bar-exclusive pass "platforms is consistent with the command modes" ""
fi

if [ "$PLATFORMS_SET" = "yes" ]; then
  record platforms-declared pass "platforms is declared explicitly" ""
else
  record platforms-declared warn "platforms is absent, and the two authorities disagree on the default" \
    "The changelog says [\"macOS\"], the JSON schema says all platforms — declare it either way"
fi

# Declaring Windows while calling macOS-only APIs is a silent store bug: it
# installs there and then fails at the first call.
if [ "$HAS_WINDOWS" = "yes" ] && [ -d "$DIR/src" ]; then
  MACOS_HITS=""
  grep -rq 'runAppleScript' "$DIR/src" 2>/dev/null && MACOS_HITS="${MACOS_HITS}${MACOS_HITS:+, }runAppleScript"
  grep -rq 'MenuBarExtra' "$DIR/src" 2>/dev/null && MACOS_HITS="${MACOS_HITS}${MACOS_HITS:+, }MenuBarExtra"
  grep -rq '/opt/homebrew' "$DIR/src" 2>/dev/null && MACOS_HITS="${MACOS_HITS}${MACOS_HITS:+, }a hardcoded /opt/homebrew path"
  grep -rq '/usr/local/bin' "$DIR/src" 2>/dev/null && MACOS_HITS="${MACOS_HITS}${MACOS_HITS:+, }a hardcoded /usr/local/bin path"
  if [ -n "$MACOS_HITS" ]; then
    record platforms-windows-macos-apis fail "platforms lists Windows but src/ uses: $MACOS_HITS" \
      "Move each behind a platform adapter (runPowerShellScript, Windows paths), or drop Windows from platforms"
  else
    record platforms-windows-macos-apis pass "no macOS-only API in src/ while Windows is declared" ""
  fi
elif [ "$HAS_WINDOWS" = "yes" ]; then
  record platforms-windows-macos-apis skip "Windows is declared but there is no src/ to scan" "See command-src-files"
else
  record platforms-windows-macos-apis pass "Windows is not declared, so macOS-only APIs are fine" ""
fi

# A cmd modifier is dropped in SILENCE on Windows — no error, no lint failure.
if [ "$HAS_WINDOWS" = "yes" ] && [ -d "$DIR/src" ]; then
  if grep -rqE 'modifiers:[[:space:]]*\[[^]]*"cmd"' "$DIR/src" 2>/dev/null \
     && ! grep -rq 'Windows:' "$DIR/src" 2>/dev/null; then
    record shortcuts-hardcoded-cmd warn "a literal \"cmd\" modifier with no Windows branch anywhere in src/" \
      "Use Keyboard.Shortcut.Common.*, or the two-branch { macOS: {...}, Windows: {...} } form — a bare cmd is ignored on Windows without any error"
  else
    record shortcuts-hardcoded-cmd pass "no unpaired cmd modifier while Windows is declared" ""
  fi
else
  record shortcuts-hardcoded-cmd pass "Windows is not declared, so cmd modifiers are fine" ""
fi

# --- AI extension -----------------------------------------------------------

TOOL_COUNT=$(q "(m.tools||[]).length")
if [ "${TOOL_COUNT:-0}" -ge 1 ] 2>/dev/null; then
  # readAIFile accepts ai.json, ai.json5, ai.yaml, ai.yml — first match wins.
  AI_FILE=""
  for f in ai.json ai.json5 ai.yaml ai.yml; do
    [ -f "$DIR/$f" ] && { AI_FILE="$f"; break; }
  done
  EVALS_IN_MANIFEST=$(q "(m.ai&&Array.isArray(m.ai.evals)&&m.ai.evals.length)?'yes':'no'")
  EVALS_IN_FILE="no"
  [ -n "$AI_FILE" ] && grep -qE '^[[:space:]]*"?evals"?[[:space:]]*:' "$DIR/$AI_FILE" 2>/dev/null && EVALS_IN_FILE="yes"

  if [ "$EVALS_IN_MANIFEST" = "yes" ] || [ "$EVALS_IN_FILE" = "yes" ]; then
    record ai-evals-present pass "evals are declared in ${AI_FILE:-package.json}" ""
  else
    record ai-evals-present warn "$TOOL_COUNT tool(s) but no ai.evals anywhere" \
      "evals[].input IS the Suggested Prompts list under @${NAME_FIELD:-your-extension} — with none, the AI surface looks empty. Only \"input\" is required"
  fi

  # Multiple AI files: the first match silently shadows the rest.
  AI_FILE_COUNT=0
  for f in ai.json ai.json5 ai.yaml ai.yml; do
    [ -f "$DIR/$f" ] && AI_FILE_COUNT=$((AI_FILE_COUNT + 1))
  done
  if [ "$AI_FILE_COUNT" -gt 1 ]; then
    record ai-file-unique fail "$AI_FILE_COUNT AI files present; only $AI_FILE is read" \
      "readAIFile tries ai.json, ai.json5, ai.yaml, ai.yml in that order and stops at the first — delete the others"
  else
    record ai-file-unique pass "at most one AI file" ""
  fi

  # Anchored to the start of the line so the commented-out stub in the scaffold's
  # tool template does not read as a real export.
  if [ -d "$DIR/src/tools" ] && grep -rqE '^[[:space:]]*export[[:space:]]+(const|function|async[[:space:]]+function)[[:space:]]+confirmation\b' "$DIR/src/tools" 2>/dev/null; then
    record ai-tool-confirmation pass "at least one tool exports a confirmation" ""
  else
    record ai-tool-confirmation warn "no tool exports a confirmation" \
      "Fine if every tool is read-only. Anything mutating should export Tool.Confirmation<Input> — it runs before the tool and can return undefined to skip itself"
  fi
else
  record ai-evals-present pass "no tools[], so evals are not applicable" ""
  record ai-file-unique pass "no tools[], so AI files are not applicable" ""
  record ai-tool-confirmation pass "no tools[], so confirmations are not applicable" ""
fi

# Every commands[].name needs a matching src file.
MISSING_SRC=$(node -e '
  const fs = require("fs"), p = require("path");
  const dir = process.argv[1];
  const m = require(p.join(dir, "package.json"));
  const missing = (m.commands || [])
    .map((c) => c.name)
    .filter((n) => !["tsx", "ts", "jsx", "js"].some((e) => fs.existsSync(p.join(dir, "src", n + "." + e))));
  process.stdout.write(missing.join(", "));
' "$(cd "$DIR" && pwd)")
if [ -z "$MISSING_SRC" ]; then
  record command-src-files pass "every commands[].name has a matching src file" ""
else
  record command-src-files fail "no src file for: $MISSING_SRC" "commands[].name must equal src/<name>.tsx"
fi

# --- files ------------------------------------------------------------------

if [ -f "$DIR/package-lock.json" ]; then
  record lockfile-present pass "package-lock.json is present" ""
else
  record lockfile-present fail "no package-lock.json" "Commit it — the store CI runs npm ci from a clean checkout"
fi

if [ -f "$DIR/README.md" ]; then
  record readme-present pass "README.md is present" ""
else
  record readme-present fail "no README.md" "Add one covering setup and any default that makes a fresh install look empty"
fi

if [ -f "$DIR/raycast-env.d.ts" ]; then
  record env-dts-present pass "raycast-env.d.ts is present" ""
else
  record env-dts-present fail "no raycast-env.d.ts" "Run ray build to generate it, then commit it"
fi

if [ -d "$DIR/src" ] && grep -rqE '^[[:space:]]*(export[[:space:]]+)?interface[[:space:]]+Preferences\b' "$DIR/src" 2>/dev/null; then
  record no-handwritten-preferences fail "src/ declares its own Preferences interface" \
    "Delete it and use the generated global Preferences.<Command> — drift should be a compile error"
else
  record no-handwritten-preferences pass "no hand-written Preferences interface" ""
fi

if [ -f "$DIR/CHANGELOG.md" ]; then
  PLACEHOLDERS=$(grep -c '{PR_MERGE_DATE}' "$DIR/CHANGELOG.md" || true)
  if grep -q '^## \[Initial Version\] - {PR_MERGE_DATE}' "$DIR/CHANGELOG.md"; then
    if [ "$PLACEHOLDERS" -gt 1 ]; then
      record changelog-initial-version warn "$PLACEHOLDERS {PR_MERGE_DATE} sections for a first release" \
        "Fold unreleased sections into [Initial Version] — nothing has shipped yet"
    else
      record changelog-initial-version pass "CHANGELOG has the Initial Version placeholder" ""
    fi
  elif [ "$PLACEHOLDERS" -ge 1 ]; then
    record changelog-initial-version warn "a {PR_MERGE_DATE} section exists but no [Initial Version] heading" \
      "A first submission should open with ## [Initial Version] - {PR_MERGE_DATE}"
  else
    record changelog-initial-version fail "no {PR_MERGE_DATE} placeholder in CHANGELOG.md" \
      "Open with ## [Initial Version] - {PR_MERGE_DATE}; do not substitute the date yourself"
  fi
else
  record changelog-initial-version fail "no CHANGELOG.md" "Add one opening with ## [Initial Version] - {PR_MERGE_DATE}"
fi

# --- images -----------------------------------------------------------------

dimensions() { # path -> "WxH", or "" when it cannot be read
  if command -v sips >/dev/null 2>&1; then
    sips -g pixelWidth -g pixelHeight "$1" 2>/dev/null \
      | awk '/pixelWidth/{w=$2} /pixelHeight/{h=$2} END{if (w && h) print w "x" h}'
  fi
}

HAVE_SIPS=1
command -v sips >/dev/null 2>&1 || HAVE_SIPS=0

ICON_NAME=$(q "m.icon")
ICON_PATH="$DIR/assets/${ICON_NAME:-extension-icon.png}"
if [ ! -f "$ICON_PATH" ]; then
  record icon-present fail "no icon at assets/${ICON_NAME:-extension-icon.png}" "Add a 512x512 PNG and point package.json icon at it"
else
  record icon-present pass "icon found at assets/${ICON_NAME}" ""

  if [ "$HAVE_SIPS" -eq 0 ]; then
    record icon-512 skip "sips is unavailable, so the icon size was not checked" "Run this on macOS"
  else
    ICON_DIM=$(dimensions "$ICON_PATH")
    if [ "$ICON_DIM" = "512x512" ]; then
      record icon-512 pass "icon is 512x512" ""
    else
      record icon-512 fail "icon is ${ICON_DIM:-unreadable}, expected 512x512" "Resize: sips -z 512 512 <icon>"
    fi
  fi

  if command -v shasum >/dev/null 2>&1; then
    ICON_SHA=$(shasum -a 256 "$ICON_PATH" | awk '{print $1}')
    if [ "$ICON_SHA" = "$PLACEHOLDER_SHA256" ]; then
      record icon-not-placeholder fail "the icon is still the scaffolder placeholder" \
        "Draw a real 512x512 icon that reads on light and dark — placeholder icons are rejected"
    else
      record icon-not-placeholder pass "the icon is not the scaffolder placeholder" ""
    fi
  else
    record icon-not-placeholder skip "shasum is unavailable" "Run this on macOS"
  fi
fi

SHOT_COUNT=0
BAD_SHOTS=""
if [ -d "$DIR/metadata" ]; then
  for f in "$DIR"/metadata/*.png "$DIR"/metadata/*.PNG; do
    [ -e "$f" ] || continue
    SHOT_COUNT=$((SHOT_COUNT + 1))
    [ "$HAVE_SIPS" -eq 0 ] && continue
    d=$(dimensions "$f")
    if [ "$d" != "2000x1250" ]; then
      BAD_SHOTS="${BAD_SHOTS}${BAD_SHOTS:+, }$(basename "$f") is ${d:-unreadable}"
    fi
  done
fi

if [ "$SHOT_COUNT" -ge 3 ] && [ "$SHOT_COUNT" -le 6 ]; then
  record metadata-count pass "$SHOT_COUNT screenshots in metadata/" ""
elif [ "$SHOT_COUNT" -eq 0 ]; then
  record metadata-count fail "metadata/ has no PNGs — and ray lint exits 0 anyway" \
    "Capture 3-6 with Raycast Window Capture + \"Save to Metadata\""
elif [ "$SHOT_COUNT" -lt 3 ]; then
  record metadata-count fail "only $SHOT_COUNT screenshot(s); the store wants 3-6" \
    "Capture more with Raycast Window Capture + \"Save to Metadata\""
else
  record metadata-count fail "$SHOT_COUNT screenshots; the maximum is 6" "Remove the weakest ones"
fi

if [ "$HAVE_SIPS" -eq 0 ]; then
  record metadata-dimensions skip "sips is unavailable, so screenshot sizes were not checked" "Run this on macOS"
elif [ "$SHOT_COUNT" -eq 0 ]; then
  record metadata-dimensions skip "no screenshots to measure" "See metadata-count"
elif [ -z "$BAD_SHOTS" ]; then
  record metadata-dimensions pass "every screenshot is 2000x1250" ""
else
  record metadata-dimensions fail "$BAD_SHOTS" \
    "Recapture with Raycast Window Capture, which writes 2000x1250 directly"
fi

# --- report -----------------------------------------------------------------

if [ "$MODE" = "json" ]; then
  printf '[%s\n]\n' "$RESULTS"
fi

if [ "$FAILED" -gt 0 ]; then
  printf '%d check(s) failed, %d warning(s), %d skipped\n' "$FAILED" "$WARNED" "$SKIPPED" >&2
  exit 4
fi
if [ "$SKIPPED" -gt 0 ]; then
  printf '%d check(s) skipped for a missing tool — skipped is not passed\n' "$SKIPPED" >&2
  exit 3
fi
if [ "$WARNED" -gt 0 ] && [ "$STRICT" -eq 1 ]; then
  printf '%d warning(s), and --strict was given\n' "$WARNED" >&2
  exit 4
fi
printf 'all checks passed (%d warning(s))\n' "$WARNED" >&2
exit 0
