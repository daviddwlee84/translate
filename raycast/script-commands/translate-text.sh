#!/usr/bin/env bash

# @raycast.schemaVersion 1
# @raycast.title Translate Text
# @raycast.mode fullOutput
# @raycast.packageName Translate
# @raycast.icon 🌐
# @raycast.argument1 { "type": "dropdown", "placeholder": "Language", "data": [ {"title":"English","value":"en"}, {"title":"Chinese (Traditional)","value":"zh-TW"}, {"title":"Chinese (Simplified)","value":"zh-CN"}, {"title":"Japanese","value":"ja"}, {"title":"Korean","value":"ko"}, {"title":"Spanish","value":"es"}, {"title":"French","value":"fr"}, {"title":"German","value":"de"}, {"title":"Italian","value":"it"}, {"title":"Portuguese","value":"pt"} ] }
# @raycast.argument2 { "type": "text", "placeholder": "Text to translate" }
# @raycast.description Translate text into the chosen language via the translate CLI.
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

BIN="$(find_translate)" || {
  echo "translate CLI not found (looked in ~/.local/bin, /opt/homebrew/bin, /usr/local/bin, ~/go/bin)."
  echo "Install: 'just install' or 'brew install daviddwlee84/tap/translate'."
  exit 1
}

# CLI contract: translate <text> --to <lang>. Plain stdout (no ANSI when non-TTY).
"$BIN" "$2" --to "$1"
