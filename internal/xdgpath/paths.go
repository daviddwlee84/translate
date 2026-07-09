// Package xdgpath resolves XDG-style config/data/state directories for translate.
//
// It deliberately does NOT use github.com/adrg/xdg or os.UserConfigDir(): both
// return ~/Library/Application Support on macOS, which breaks this tool's
// ~/.config convention. Instead it honors the XDG_* environment variables and
// otherwise falls back to the freedesktop defaults (~/.config, ~/.local/share,
// ~/.local/state) on macOS AND Linux alike.
package xdgpath

import (
	"os"
	"path/filepath"
)

// app is the per-tool subdirectory used under each base dir.
const app = "translate"

// base returns $env if set, else $HOME/def.
func base(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, def)
}

// ConfigDir returns the translate config directory (e.g. ~/.config/translate).
func ConfigDir() string { return filepath.Join(base("XDG_CONFIG_HOME", ".config"), app) }

// DataDir returns the translate data directory (e.g. ~/.local/share/translate).
func DataDir() string { return filepath.Join(base("XDG_DATA_HOME", ".local/share"), app) }

// StateDir returns the translate state directory (e.g. ~/.local/state/translate).
func StateDir() string { return filepath.Join(base("XDG_STATE_HOME", ".local/state"), app) }

// ConfigFile returns the full path to config.toml.
func ConfigFile() string { return filepath.Join(ConfigDir(), "config.toml") }

// StateFile returns the full path to state.json.
func StateFile() string { return filepath.Join(StateDir(), "state.json") }

// HistoryFile returns the default history path (JSONL).
func HistoryFile() string { return filepath.Join(DataDir(), "history.jsonl") }

// EnsureDirs creates the config, data, and state directories (0700) if missing.
func EnsureDirs() error {
	for _, d := range []string{ConfigDir(), DataDir(), StateDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
