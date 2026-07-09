// Package state persists small session state (the last language pair and source
// mode) so a bare `translate` resumes where the user left off.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"translate/internal/xdgpath"
)

// State is the persisted last-session snapshot.
type State struct {
	Source     string    `json:"source"`                // "auto" or a code
	Target     string    `json:"target"`                // code
	SourceMode string    `json:"source_mode,omitempty"` // auto | fixed
	Provider   string    `json:"provider,omitempty"`
	Engine     string    `json:"engine,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Load reads the state file, returning a zero State if it does not exist.
func Load() (*State, error) {
	b, err := os.ReadFile(xdgpath.StateFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return &State{}, nil // corrupt state is non-fatal
	}
	return &s, nil
}

// Save writes the state atomically (temp file + rename).
func Save(s *State) error {
	if err := xdgpath.EnsureDirs(); err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	p := xdgpath.StateFile()
	tmp, err := os.CreateTemp(filepath.Dir(p), ".state-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}
