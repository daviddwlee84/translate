package engine

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
)

type ecdictEntry struct {
	Word, Phonetic, Translation, Definition string
}

// ecdictDB queries the locally-built ECDICT SQLite (English → Chinese). Point
// lookups are on-disk (near-zero steady-state RAM). Opened lazily.
type ecdictDB struct {
	path string
	once sync.Once
	err  error
	db   *sql.DB
}

func newEcdictDB(path string) *ecdictDB { return &ecdictDB{path: path} }

func (e *ecdictDB) available() bool {
	_, err := os.Stat(e.path)
	return err == nil
}

func (e *ecdictDB) open() error {
	e.once.Do(func() {
		if _, err := os.Stat(e.path); err != nil {
			e.err = err
			return
		}
		db, err := sql.Open("sqlite", "file:"+e.path+"?_pragma=query_only(true)")
		if err != nil {
			e.err = err
			return
		}
		e.db = db
	})
	return e.err
}

// lookup returns the exact (case-insensitive) entry, or nil if not found.
func (e *ecdictDB) lookup(ctx context.Context, word string) (*ecdictEntry, error) {
	if err := e.open(); err != nil {
		return nil, err
	}
	row := e.db.QueryRowContext(ctx,
		`SELECT word, phonetic, translation, definition FROM entries WHERE word_lc = ? LIMIT 1`,
		strings.ToLower(strings.TrimSpace(word)))
	var en ecdictEntry
	switch err := row.Scan(&en.Word, &en.Phonetic, &en.Translation, &en.Definition); err {
	case nil:
		return &en, nil
	case sql.ErrNoRows:
		return nil, nil
	default:
		return nil, err
	}
}

// splitEcdict splits ECDICT translation/definition fields — their senses are
// joined by a literal backslash-n, not a real newline.
func splitEcdict(s string) []string {
	var out []string
	for _, p := range strings.Split(s, `\n`) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
