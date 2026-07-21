package config

import (
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// A config written before schema tracking has no `schema` key, so it reads as 0
// and is reported outdated; a config at the current schema is not.
func TestOutdated(t *testing.T) {
	// Pre-versioning config: no schema key. Load seeds Default() (schema 0) then
	// unmarshal leaves it 0, so the absent key correctly reads as outdated.
	old := Default()
	if err := toml.Unmarshal([]byte("[general]\ndefault_target = 'en'\n"), old); err != nil {
		t.Fatal(err)
	}
	if !old.Outdated() {
		t.Errorf("config with no schema key should be Outdated (schema=%d, want < %d)", old.Schema, SchemaVersion)
	}

	cur := Default()
	if err := toml.Unmarshal([]byte("schema = 1\n[general]\n"), cur); err != nil {
		t.Fatal(err)
	}
	if cur.Schema == SchemaVersion && cur.Outdated() {
		t.Errorf("config at current schema %d should not be Outdated", SchemaVersion)
	}
}

// Save stamps the current schema (and the app version when Generator is set), so a
// freshly saved config round-trips as up-to-date.
func TestSaveStampsVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRANSLATE_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)

	old := Generator
	Generator = "v9.9.9"
	defer func() { Generator = old }()

	cfg := Default() // Schema 0 in memory until Save stamps it
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Schema != SchemaVersion {
		t.Errorf("Save should stamp Schema=%d, got %d", SchemaVersion, cfg.Schema)
	}
	if cfg.Version != "v9.9.9" {
		t.Errorf("Save should stamp Version from Generator, got %q", cfg.Version)
	}

	loaded, created, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("Load should read the just-saved file, not report created")
	}
	if loaded.Outdated() {
		t.Errorf("a just-saved config should not be Outdated (schema=%d)", loaded.Schema)
	}
	if loaded.Version != "v9.9.9" {
		t.Errorf("Version should persist, got %q", loaded.Version)
	}
}
