package engine

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// DictDataStatus distinguishes missing downloads from existing, unusable data.
// Inspection is local and read-only; it never creates a database or downloads.
type DictDataStatus struct {
	Path  string
	State string // "missing", "ready", or "unusable"
	Err   error
}

type LocalDictStatus struct {
	CedictSource DictDataStatus
	CedictIndex  DictDataStatus
	Ecdict       DictDataStatus
}

// InspectLocalDict performs small read-only checks for setup, not an exhaustive
// database integrity scan. Either a source file or an index can serve CC-CEDICT.
func InspectLocalDict(ctx context.Context, dir string) LocalDictStatus {
	return LocalDictStatus{
		CedictSource: inspectDictFile(CedictPath(dir), func(path string) error {
			return validateCedictSource(ctx, path)
		}),
		CedictIndex: inspectDictDB(ctx, CedictDBPath(dir), "key,trad,simp,pinyin,defs,n"),
		Ecdict:      inspectDictDB(ctx, EcdictDBPath(dir), "word,word_lc,phonetic,translation,definition,pos,frq,exchange"),
	}
}

// validateCedictSource ensures a download contains dictionary data before it
// replaces an installed file. Inspection shares this bounded, read-only check.
func validateCedictSource(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for s.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cedictLine.MatchString(s.Text()) {
			return nil
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	return fmt.Errorf("no valid CC-CEDICT entries")
}

func inspectDictFile(path string, check func(string) error) DictDataStatus {
	s := DictDataStatus{Path: path, State: "unusable"}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		s.State = "missing"
		return s
	}
	if err == nil && (!info.Mode().IsRegular() || info.Size() == 0) {
		err = fmt.Errorf("not a non-empty regular file")
	}
	if err == nil {
		err = check(path)
	}
	s.Err = err
	if err == nil {
		s.State = "ready"
	}
	return s
}

func inspectDictDB(ctx context.Context, path, columns string) DictDataStatus {
	return inspectDictFile(path, func(path string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "mode=ro"}
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := db.QueryContext(ctx, "SELECT "+columns+" FROM entries LIMIT 1")
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return fmt.Errorf("dictionary contains no entries")
		}
		return rows.Err()
	})
}
