package engine

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectLocalDictReadiness(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s := InspectLocalDict(ctx, dir)
	if s.CedictSource.State != "missing" || s.CedictIndex.State != "missing" || s.Ecdict.State != "missing" {
		t.Fatalf("missing: %+v", s)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("inspection wrote files: %v / %v", files, err)
	}
	src := cedictFixture(t, dir)
	s = InspectLocalDict(ctx, dir)
	if s.CedictSource.State != "ready" || s.CedictIndex.State != "missing" {
		t.Fatalf("source only: %+v", s)
	}
	if err := BuildCedictDB(ctx, src, CedictDBPath(dir), nil); err != nil {
		t.Fatal(err)
	}
	ecdictFixture(t, dir)
	s = InspectLocalDict(ctx, dir)
	if s.CedictSource.State != "ready" || s.CedictIndex.State != "ready" || s.Ecdict.State != "ready" {
		t.Fatalf("healthy: %+v", s)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	s = InspectLocalDict(ctx, dir)
	if s.CedictSource.State != "missing" || s.CedictIndex.State != "ready" {
		t.Fatalf("index only: %+v", s)
	}
}

func TestInspectLocalDictUnusable(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"empty", ""}, {"corrupt", "not a SQLite database"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, path := range []string{CedictPath(dir), CedictDBPath(dir), EcdictDBPath(dir)} {
				if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s := InspectLocalDict(context.Background(), dir)
			for _, data := range []DictDataStatus{s.CedictSource, s.CedictIndex, s.Ecdict} {
				if data.State != "unusable" || data.Err == nil {
					t.Fatalf("%s: %+v", tc.name, data)
				}
				b, err := os.ReadFile(data.Path)
				if err != nil || string(b) != tc.content {
					t.Fatalf("inspection changed file: %q / %v", b, err)
				}
			}
		})
	}
	for _, schema := range []string{"CREATE TABLE other(x)", ecdictSchema} {
		t.Run(schema, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "space ?#")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			// Build elsewhere then move, so this fixture does not depend on a
			// writer's URI escaping when the inspected path contains punctuation.
			tmp := filepath.Join(t.TempDir(), "fixture.db")
			db, err := sql.Open("sqlite", tmp)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(schema); err != nil {
				t.Fatal(err)
			}
			db.Close()
			if err := os.Rename(tmp, EcdictDBPath(dir)); err != nil {
				t.Fatal(err)
			}
			s := InspectLocalDict(context.Background(), dir)
			if s.Ecdict.State != "unusable" || s.Ecdict.Err == nil {
				t.Fatalf("invalid schema or empty data accepted: %+v", s.Ecdict)
			}
		})
	}
}
