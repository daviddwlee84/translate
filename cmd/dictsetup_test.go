package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
)

func dictSetupFixture(t *testing.T, fail string) (*config.Config, map[string]int) {
	t.Helper()
	calls := make(map[string]int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if r.URL.Path == fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/cedict":
			gz := gzip.NewWriter(w)
			io.WriteString(gz, "# fixture\n")
			if fail != "empty:/cedict" {
				io.WriteString(gz, "貓 猫 [mao1] /cat/\n")
			}
			gz.Close()
		case "/ecdict":
			io.WriteString(w, "word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange\n")
			if fail != "empty:/ecdict" {
				io.WriteString(w, "test,test,a test,測試,n,0,0,,0,575,\n")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.Dict.Dir = filepath.Join(t.TempDir(), "dict")
	cfg.Dict.CedictURL, cfg.Dict.EcdictURL = srv.URL+"/cedict", srv.URL+"/ecdict"
	t.Setenv("TRANSLATE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg, calls
}

func TestSetupDictionariesDownloadsOnlyMissing(t *testing.T) {
	for _, tc := range []struct {
		name, installed string
		want            []string
	}{
		{"missing", "", []string{"cedict", "ecdict"}},
		{"partial", "cedict", []string{"ecdict"}},
		{"healthy", "all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, calls := dictSetupFixture(t, "")
			if tc.installed != "" {
				if err := updateDictionaries(context.Background(), cfg, tc.installed, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			clear(calls)
			var got []string
			err := setupMissingDictionaries(context.Background(), cfg, io.Discard, func(_ context.Context, targets []string) (bool, error) { got = targets; return true, nil })
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("prompted %v, want %v", got, tc.want)
			}
			if len(calls) != len(tc.want) {
				t.Fatalf("downloaded unexpected sources: %v", calls)
			}
			for _, target := range tc.want {
				if calls["/"+target] != 1 {
					t.Fatalf("requests %v", calls)
				}
			}
			s := engine.InspectLocalDict(context.Background(), cfg.Dict.Dir)
			if s.CedictIndex.State != "ready" || s.Ecdict.State != "ready" {
				t.Fatalf("not ready: %+v", s)
			}
		})
	}
}

func TestSetupDictionariesKeepsSourceWithoutIndex(t *testing.T) {
	cfg, calls := dictSetupFixture(t, "")
	if err := updateDictionaries(context.Background(), cfg, "all", io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(engine.CedictDBPath(cfg.Dict.Dir)); err != nil {
		t.Fatal(err)
	}
	clear(calls)
	var out bytes.Buffer
	err := setupMissingDictionaries(context.Background(), cfg, &out, func(context.Context, []string) (bool, error) {
		t.Fatal("prompted for installed dictionaries")
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || !strings.Contains(out.String(), "translate dict reindex") {
		t.Fatalf("requests %v / output %s", calls, out.String())
	}
}

func TestSetupDictionariesPreservesSavedConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		confirm   bool
		promptErr error
		fail      string
		wantErr   bool
		retry     string
	}{
		{"later", false, nil, "", false, "translate dict update all"},
		{"cancel", false, context.Canceled, "", true, "translate dict update all"},
		{"first download fails", true, nil, "/cedict", true, "translate dict update all"},
		{"second download fails", true, nil, "/ecdict", true, "translate dict update ecdict"},
		{"comments-only cedict", true, nil, "empty:/cedict", true, "translate dict update all"},
		{"header-only ecdict", true, nil, "empty:/ecdict", true, "translate dict update ecdict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, calls := dictSetupFixture(t, tc.fail)
			before, err := os.ReadFile(config.Path())
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = setupMissingDictionaries(context.Background(), cfg, &out, func(context.Context, []string) (bool, error) { return tc.confirm, tc.promptErr })
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v", err)
			}
			if tc.wantErr && strings.Contains(out.String(), "offline dictionaries installed.") {
				t.Fatalf("reported install success: %s", out.String())
			}
			if tc.promptErr != nil && !errors.Is(err, tc.promptErr) {
				t.Fatalf("lost cancellation: %v", err)
			}
			message := out.String()
			if err != nil {
				message += err.Error()
			}
			if !strings.Contains(message, "configuration saved") || !strings.Contains(message, tc.retry) {
				t.Fatalf("missing retained/retry guidance: %s", message)
			}
			after, err := os.ReadFile(config.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("saved config changed")
			}
			if !tc.confirm && len(calls) != 0 {
				t.Fatalf("unexpected requests: %v", calls)
			}
			if tc.fail == "/ecdict" || tc.fail == "empty:/ecdict" {
				s := engine.InspectLocalDict(context.Background(), cfg.Dict.Dir)
				if s.CedictIndex.State != "ready" || s.Ecdict.State != "missing" {
					t.Fatalf("successful first download lost: %+v", s)
				}
			}
		})
	}
}

func TestSetupDictionariesDisabledAndUnusable(t *testing.T) {
	for _, kind := range []string{"disabled", "api", "unusable"} {
		t.Run(kind, func(t *testing.T) {
			cfg, calls := dictSetupFixture(t, "")
			switch kind {
			case "disabled":
				cfg.Dict.Enabled = false
			case "api":
				cfg.Dict.Source = "api"
			case "unusable":
				if err := os.MkdirAll(cfg.Dict.Dir, 0700); err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{engine.CedictDBPath(cfg.Dict.Dir), engine.EcdictDBPath(cfg.Dict.Dir)} {
					if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			var out bytes.Buffer
			err := setupMissingDictionaries(context.Background(), cfg, &out, func(context.Context, []string) (bool, error) { t.Fatal("unexpected prompt"); return false, nil })
			if err != nil || len(calls) != 0 {
				t.Fatalf("err %v / requests %v", err, calls)
			}
			if kind == "unusable" && (!strings.Contains(out.String(), "ECDICT is unusable") || !strings.Contains(out.String(), "CC-CEDICT is unusable")) {
				t.Fatalf("missing repair guidance: %s", out.String())
			}
		})
	}
}
