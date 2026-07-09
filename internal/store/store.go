// Package store persists translation history. v1 uses an append-only JSONL file
// with in-memory fuzzy search behind the Store interface; a sqlite+FTS5 backend
// can drop in later without changing callers.
package store

import (
	"context"
	"time"
)

// Record is one history entry (also the JSONL line shape).
type Record struct {
	ID           string    `json:"id"`
	TS           time.Time `json:"ts"`
	SourceLang   string    `json:"source_lang"`
	TargetLang   string    `json:"target_lang"`
	Engine       string    `json:"engine"`
	Model        string    `json:"model,omitempty"`
	Input        string    `json:"input"`
	Output       string    `json:"output"`
	Alternatives []string  `json:"alternatives,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	Favorite     bool      `json:"favorite,omitempty"`
}

// Store is the history persistence interface.
type Store interface {
	// Add assigns an ID and timestamp, persists the record, and returns it.
	Add(ctx context.Context, r Record) (Record, error)
	// Recent returns up to limit records, newest first.
	Recent(ctx context.Context, limit int) ([]Record, error)
	// Search returns records fuzzy-matching query (over input+output), best first.
	Search(ctx context.Context, query string, limit int) ([]Record, error)
	// Close releases any resources.
	Close() error
}
