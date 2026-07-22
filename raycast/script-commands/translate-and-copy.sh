#!/usr/bin/env bash

# @raycast.schemaVersion 1
# @raycast.title Translate & Copy
# @raycast.mode silent
# @raycast.packageName Translate
# @raycast.icon 📋
# @raycast.argument1 { "type": "dropdown", "placeholder": "Language", "data": [ {"title":"English","value":"en"}, {"title":"Chinese (Traditional)","value":"zh-TW"}, {"title":"Chinese (Simplified)","value":"zh-CN"}, {"title":"Japanese","value":"ja"}, {"title":"Korean","value":"ko"}, {"title":"Spanish","value":"es"}, {"title":"French","value":"fr"}, {"title":"German","value":"de"}, {"title":"Italian","value":"it"}, {"title":"Portuguese","value":"pt"} ] }
# @raycast.argument2 { "type": "text", "placeholder": "Text (blank = use clipboard)", "optional": true }
# @raycast.description Translate text (or the clipboard) and copy the result.
# @raycast.author David Lee
# @raycast.authorURL https://github.com/daviddwlee84

set -euo pipefail

# Raycast runs under launchd and does NOT inherit your shell PATH, so a bare
# `translate` is usually not found. Resolve an absolute path instead.
find_translate() {
  for d in "$HOME/.local/bin" /opt/homebrew/bin /usr/local/bin "$HOME/go/bin"; do
    [ -x "$d/translate" ] && { printf '%s\n' "$d/translate"; return 0; }
  done
  command -v translate 2>/dev/null && return 0
  return 1
}

BIN="$(find_translate)" || { echo "translate CLI not found (see ~/.local/bin)."; exit 1; }

# Script Commands have no getSelectedText — fall back to the clipboard when the
# text argument is blank (copy your text first, then run with the field empty).
TEXT="${2:-}"
[ -z "$TEXT" ] && TEXT="$(pbpaste)"
[ -z "$TEXT" ] && { echo "Nothing to translate (empty argument and clipboard)."; exit 1; }

RESULT="$("$BIN" "$TEXT" --to "$1")"
printf '%s' "$RESULT" | pbcopy
echo "Copied → $1: $RESULT"   # silent mode surfaces this last line as a HUD
