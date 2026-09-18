package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func assertDictDestinationUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	after, err := os.ReadFile(path)
	if before == nil {
		if !os.IsNotExist(err) {
			t.Fatalf("failed install left a destination: %v", err)
		}
	} else if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed install changed the existing destination: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("failed install left a temporary file: %v", err)
	}
}

func TestBuildEcdictDBRejectsEmptyImport(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"header-only", "word,phonetic,definition,translation\n"},
		{"blank-headword", "word,phonetic,definition,translation\n,,,\n"},
	} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%v", tc.name, existing), func(t *testing.T) {
				dir := t.TempDir()
				var before []byte
				if existing {
					ecdictFixture(t, dir)
					var err error
					before, err = os.ReadFile(EcdictDBPath(dir))
					if err != nil {
						t.Fatal(err)
					}
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, tc.body) }))
				defer srv.Close()
				var progress []string
				err := BuildEcdictDB(context.Background(), srv.URL, EcdictDBPath(dir), func(s string) { progress = append(progress, s) })
				if err == nil || !strings.Contains(err.Error(), "no valid entries") {
					t.Fatalf("accepted empty import: %v", err)
				}
				if strings.Contains(strings.Join(progress, " "), "built ") {
					t.Fatalf("printed success: %v", progress)
				}
				assertDictDestinationUnchanged(t, EcdictDBPath(dir), before)
			})
		}
	}
}

func TestDownloadCedictRejectsCommentsOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gz := gzip.NewWriter(w)
		io.WriteString(gz, "# download succeeded, but contains no dictionary entries\n")
		gz.Close()
	}))
	defer srv.Close()
	for _, existing := range []bool{false, true} {
		dir := t.TempDir()
		var before []byte
		if existing {
			cedictFixture(t, dir)
			var err error
			before, err = os.ReadFile(CedictPath(dir))
			if err != nil {
				t.Fatal(err)
			}
		}
		err := DownloadCedict(context.Background(), srv.URL, CedictPath(dir), nil)
		if err == nil || !strings.Contains(err.Error(), "no valid CC-CEDICT entries") {
			t.Fatalf("accepted comments-only source: %v", err)
		}
		assertDictDestinationUnchanged(t, CedictPath(dir), before)
	}
}

func TestBuildCedictDBRejectsEmptyImport(t *testing.T) {
	for _, existing := range []bool{false, true} {
		dir := t.TempDir()
		src := cedictFixture(t, dir)
		var before []byte
		if existing {
			if err := BuildCedictDB(context.Background(), src, CedictDBPath(dir), nil); err != nil {
				t.Fatal(err)
			}
			var err error
			before, err = os.ReadFile(CedictDBPath(dir))
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(src, []byte("# no dictionary entries\n"), 0600); err != nil {
			t.Fatal(err)
		}
		err := BuildCedictDB(context.Background(), src, CedictDBPath(dir), nil)
		if err == nil || !strings.Contains(err.Error(), "no valid entries") {
			t.Fatalf("accepted empty source: %v", err)
		}
		assertDictDestinationUnchanged(t, CedictDBPath(dir), before)
	}
}
