package engine

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/daviddwlee84/translate/internal/xdgpath"
)

// DictDir returns the directory holding local dictionary data.
func DictDir(dir string) string {
	if dir != "" {
		return dir
	}
	return filepath.Join(xdgpath.DataDir(), "dict")
}

// CedictPath / EcdictDBPath are the on-disk locations of the two data sources.
func CedictPath(dir string) string   { return filepath.Join(DictDir(dir), "cedict_ts.u8") }
func EcdictDBPath(dir string) string { return filepath.Join(DictDir(dir), "ecdict.db") }

const ecdictSchema = `CREATE TABLE entries(
  word TEXT, word_lc TEXT, phonetic TEXT, translation TEXT,
  definition TEXT, pos TEXT, frq INTEGER, exchange TEXT);`

// DownloadCedict fetches the gzipped CC-CEDICT, decompresses it, and writes the
// plain text file atomically (~9.4 MB).
func DownloadCedict(ctx context.Context, url, dst string, prog func(string)) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if prog != nil {
		prog("downloading CC-CEDICT…")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cedict download: http %d", resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()

	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, gz); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// BuildEcdictDB downloads the ECDICT CSV (~63 MB, MIT) and builds a queryable
// SQLite database (~80-100 MB) from it, atomically. Streams the CSV so it never
// holds the whole file in memory.
func BuildEcdictDB(ctx context.Context, csvURL, dbPath string, prog func(string)) (err error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return err
	}
	if prog != nil {
		prog("downloading ECDICT CSV (~63 MB)…")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, csvURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ecdict download: http %d", resp.StatusCode)
	}

	tmp := dbPath + ".tmp"
	_ = os.Remove(tmp)
	db, err := sql.Open("sqlite", "file:"+tmp+"?_pragma=journal_mode(off)&_pragma=synchronous(off)")
	if err != nil {
		return err
	}
	// On any error, close and remove the partial db.
	defer func() {
		if err != nil {
			db.Close()
			os.Remove(tmp)
		}
	}()

	if _, err = db.ExecContext(ctx, ecdictSchema); err != nil {
		return err
	}
	if prog != nil {
		prog("importing entries…")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO entries(word,word_lc,phonetic,translation,definition,pos,frq,exchange) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}

	r := csv.NewReader(resp.Body)
	r.FieldsPerRecord = -1 // tolerate ragged rows
	r.LazyQuotes = true
	if _, err = r.Read(); err != nil { // header row
		return err
	}
	n := 0
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		rec, rerr := r.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil || len(rec) < 4 {
			continue
		}
		frq := 0
		if len(rec) > 9 {
			frq, _ = strconv.Atoi(strings.TrimSpace(rec[9]))
		}
		pos, exchange := "", ""
		if len(rec) > 4 {
			pos = rec[4]
		}
		if len(rec) > 10 {
			exchange = rec[10]
		}
		if _, err = stmt.ExecContext(ctx, rec[0], strings.ToLower(rec[0]), rec[1], rec[3], rec[2], pos, frq, exchange); err != nil {
			return err
		}
		n++
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `CREATE INDEX idx_word_lc ON entries(word_lc)`); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	if prog != nil {
		prog(fmt.Sprintf("built %d entries", n))
	}
	return os.Rename(tmp, dbPath)
}
