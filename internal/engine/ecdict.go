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

// ecdictRow is one row of a headword search: the fields a picker renders, plus
// the frequency rank used to order them.
type ecdictRow struct {
	Word, Phonetic, Translation, Definition, POS string
	Frq                                          int
}

// prefixSearch returns headwords beginning with prefix, most useful first.
//
// Two non-obvious things are load-bearing here:
//
//   - `frq` is a frequency *rank*, not a count (the=1, test=575, tester=7037,
//     0 = unranked), so the order is ASCENDING with zeros pushed last. `frq DESC`
//     looks plausible and puts the most obscure words on top.
//   - The predicate is a RANGE, not `LIKE prefix||'%'`. SQLite's LIKE optimization
//     needs case_sensitive_like or a NOCASE index, and we have neither, so LIKE
//     degrades to a full scan of ~770k rows (435 ms vs 12 ms measured).
func (e *ecdictDB) prefixSearch(ctx context.Context, prefix string, limit int) ([]ecdictRow, error) {
	if err := e.open(); err != nil {
		return nil, err
	}
	p := strings.ToLower(strings.TrimSpace(prefix))
	if p == "" {
		return nil, nil
	}
	rows, err := e.db.QueryContext(ctx, `
SELECT word, phonetic, substr(translation,1,240), substr(definition,1,240), pos, frq
  FROM entries
 WHERE word_lc >= ? AND word_lc < ?
 ORDER BY (word_lc = ?) DESC,          -- exact headword first
          (instr(word_lc,' ') > 0) ASC, -- single words before multi-word phrases
          (frq = 0) ASC,                -- known frequency before unranked
          frq ASC,                      -- lower rank = more common
          length(word_lc) ASC,          -- short forms before long compounds
          word_lc ASC                   -- stable
 LIMIT ?`, p, prefixUpper(p), p, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEcdictRows(rows)
}

// lookupMany fetches entries for a set of headwords in one query, so fuzzy
// candidates get definition previews without N round trips.
func (e *ecdictDB) lookupMany(ctx context.Context, words []string) (map[string]ecdictRow, error) {
	out := map[string]ecdictRow{}
	if len(words) == 0 {
		return out, nil
	}
	if err := e.open(); err != nil {
		return nil, err
	}
	args := make([]any, len(words))
	for i, w := range words {
		args[i] = strings.ToLower(strings.TrimSpace(w))
	}
	q := `SELECT word, phonetic, substr(translation,1,240), substr(definition,1,240), pos, frq
	        FROM entries WHERE word_lc IN (?` + strings.Repeat(",?", len(words)-1) + `)`
	rows, err := e.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanEcdictRows(rows)
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		k := strings.ToLower(r.Word)
		// Keep the first row per headword; ECDICT can carry near-duplicates.
		if _, seen := out[k]; !seen {
			out[k] = r
		}
	}
	return out, nil
}

func scanEcdictRows(rows *sql.Rows) ([]ecdictRow, error) {
	var out []ecdictRow
	for rows.Next() {
		var r ecdictRow
		if err := rows.Scan(&r.Word, &r.Phonetic, &r.Translation, &r.Definition, &r.POS, &r.Frq); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
