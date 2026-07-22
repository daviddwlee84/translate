#!/usr/bin/env bash

# @raycast.schemaVersion 1
# @raycast.title Define Word
# @raycast.mode fullOutput
# @raycast.packageName Translate
# @raycast.icon 📖
# @raycast.argument1 { "type": "text", "placeholder": "Word" }
# @raycast.description Dictionary lookup (exact → fuzzy → LLM) via the translate CLI.
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

"$BIN" define "$1"
