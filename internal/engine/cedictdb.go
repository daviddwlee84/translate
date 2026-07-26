package engine

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// CedictDBPath is the built CC-CEDICT index, sitting beside the plain cedict_ts.u8
// it is derived from.
func CedictDBPath(dir string) string { return filepath.Join(DictDir(dir), "cedict.db") }

// One row per lookup key: CC-CEDICT files an entry under both its traditional and
// its simplified form, matching cedictIndex.byKey. n is the key's rune length,
// stored so prefix results can be ranked without re-decoding UTF-8 per row.
const cedictSchema = `CREATE TABLE entries(
  key TEXT, trad TEXT, simp TEXT, pinyin TEXT, defs TEXT, n INTEGER);`

// cedictDefSep joins an entry's definitions in the defs column. CC-CEDICT itself
// delimits them with "/", which appears inside definitions, so use a character
// that cannot.
const cedictDefSep = "\x1f"

// BuildCedictDB parses a local cedict_ts.u8 into a queryable SQLite index,
// atomically (temp file + rename). No network: the .u8 must already be on disk.
//
// This exists because cedictIndex re-parses the whole 9.8 MB file — a regexp per
// line — on every process start (~1.7 s), which is fine for a one-shot CLI and
// unusable for a caller that looks something up on each keystroke.
func BuildCedictDB(ctx context.Context, srcPath, dbPath string, prog func(string)) (err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return err
	}
	tmp := dbPath + ".tmp"
	_ = os.Remove(tmp)
	db, err := sql.Open("sqlite", "file:"+tmp+"?_pragma=journal_mode(off)&_pragma=synchronous(off)")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			db.Close()
			os.Remove(tmp)
		}
	}()

	if _, err = db.ExecContext(ctx, cedictSchema); err != nil {
		return err
	}
	if prog != nil {
		prog("indexing CC-CEDICT…")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO entries(key,trad,simp,pinyin,defs,n) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		return err
	}

	n := 0
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		if err = ctx.Err(); err != nil {
			return err
		}
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		m := cedictLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		trad, simp, pinyin := m[1], m[2], m[3]
		var defs []string
		for _, d := range strings.Split(m[4], "/") {
			if d = strings.TrimSpace(d); d != "" {
				defs = append(defs, d)
			}
		}
		joined := strings.Join(defs, cedictDefSep)
		keys := []string{simp}
		if trad != simp {
			keys = append(keys, trad)
		}
		for _, k := range keys {
			if _, err = stmt.ExecContext(ctx, k, trad, simp, pinyin, joined, len([]rune(k))); err != nil {
				return err
			}
		}
		n++
	}
	if err = sc.Err(); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `CREATE INDEX idx_cedict_key ON entries(key)`); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	if prog != nil {
		prog(fmt.Sprintf("indexed %d entries", n))
	}
	return os.Rename(tmp, dbPath)
}

// cedictDB queries the built CC-CEDICT index. Same shape as ecdictDB: on-disk
// point lookups, opened lazily, read-only.
type cedictDB struct {
	path string
	once sync.Once
	err  error
	db   *sql.DB
}

func newCedictDB(path string) *cedictDB { return &cedictDB{path: path} }

func (c *cedictDB) available() bool {
	_, err := os.Stat(c.path)
	return err == nil
}

func (c *cedictDB) open() error {
	c.once.Do(func() {
		if _, err := os.Stat(c.path); err != nil {
			c.err = err
			return
		}
		db, err := sql.Open("sqlite", "file:"+c.path+"?_pragma=query_only(true)")
		if err != nil {
			c.err = err
			return
		}
		c.db = db
	})
	return c.err
}

// lookup returns every entry filed under an exact headword (traditional or
// simplified), or nil when there is none.
func (c *cedictDB) lookup(ctx context.Context, word string) ([]*cedictEntry, error) {
	if err := c.open(); err != nil {
		return nil, err
	}
	rows, err := c.db.QueryContext(ctx,
		`SELECT trad, simp, pinyin, defs FROM entries WHERE key = ?`,
		strings.TrimSpace(word))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*cedictEntry
	for rows.Next() {
		e, err := scanCedictEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// prefixSearch returns up to limit headwords beginning with prefix, exact match
// first, then shortest, then alphabetical. Like ecdictDB.prefixSearch it uses a
// range predicate rather than LIKE so the key index is actually used.
func (c *cedictDB) prefixSearch(ctx context.Context, prefix string, limit int) ([]cedictHit, error) {
	if err := c.open(); err != nil {
		return nil, err
	}
	p := strings.TrimSpace(prefix)
	if p == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := c.db.QueryContext(ctx, `
SELECT key, trad, simp, pinyin, defs
  FROM entries
 WHERE key >= ? AND key < ?
 ORDER BY (key = ?) DESC, n ASC, key ASC
 LIMIT ?`, p, prefixUpper(p), p, limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var order []string
	byKey := map[string][]*cedictEntry{}
	for rows.Next() {
		var key string
		var trad, simp, pinyin, defs string
		if err := rows.Scan(&key, &trad, &simp, &pinyin, &defs); err != nil {
			return nil, err
		}
		if _, seen := byKey[key]; !seen {
			if len(order) >= limit {
				continue // already have `limit` distinct headwords
			}
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], &cedictEntry{
			Trad: trad, Simp: simp, Pinyin: pinyin, Defs: splitCedictDefs(defs),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]cedictHit, 0, len(order))
	for _, k := range order {
		out = append(out, cedictHit{Key: k, Entries: byKey[k]})
	}
	return out, nil
}

func scanCedictEntry(rows *sql.Rows) (*cedictEntry, error) {
	var trad, simp, pinyin, defs string
	if err := rows.Scan(&trad, &simp, &pinyin, &defs); err != nil {
		return nil, err
	}
	return &cedictEntry{Trad: trad, Simp: simp, Pinyin: pinyin, Defs: splitCedictDefs(defs)}, nil
}

func splitCedictDefs(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, cedictDefSep)
}
