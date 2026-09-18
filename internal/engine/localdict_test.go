package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLocalDictSetupDiagnostics(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				if status == http.StatusOK {
					io.WriteString(w, `[{"word":"test","meanings":[{"partOfSpeech":"noun","definitions":[{"definition":"a trial"}]}]}]`)
				}
			}))
			defer srv.Close()
			dict := NewLocalDict(LocalDictConfig{Dir: t.TempDir(), APIFallback: NewDict(DictConfig{Endpoint: srv.URL})})
			ch, err := dict.Translate(context.Background(), Request{Text: "test"})
			if err != nil {
				t.Fatal(err)
			}
			res, err := Drain(ch, nil)
			if status == http.StatusOK {
				if err != nil || res.Dictionary == nil || !strings.Contains(res.Notes, "translate dict update ecdict") || !strings.Contains(res.Notes, "online dictionary") {
					t.Fatalf("result %+v / %v", res, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "ECDICT not installed") || !strings.Contains(err.Error(), "online dictionary failed") {
				t.Fatalf("lost setup reason: %v", err)
			}

			llm := &fakeEngine{name: "fake", res: &TranslateResult{Translation: "LLM definition", Engine: "fake"}}
			smart := NewSmartDict(dict, llm, SmartDictConfig{})
			ch, err = smart.Translate(context.Background(), Request{Text: "test"})
			if err != nil {
				t.Fatal(err)
			}
			res, err = Drain(ch, nil)
			if err != nil {
				t.Fatal(err)
			}
			if status == http.StatusOK {
				if llm.called || !strings.Contains(res.Notes, "ECDICT not installed") {
					t.Fatalf("API hit lost notes or used LLM: %+v", res)
				}
			} else {
				if !llm.called || !strings.Contains(strings.Join(res.Warnings, " "), "ECDICT not installed") {
					t.Fatalf("fallback lost setup reason: %+v", res)
				}
			}
		})
	}
}

func TestLocalDictOfflineHitsAndMissingChinese(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.Error(w, "unexpected", 500) }))
	defer srv.Close()
	dir := t.TempDir()
	cfg := LocalDictConfig{Dir: dir, APIFallback: NewDict(DictConfig{Endpoint: srv.URL})}
	de := NewLocalDict(cfg)
	ch, err := de.Translate(context.Background(), Request{Text: "貓"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Drain(ch, nil)
	if err != nil || !strings.Contains(res.Notes, "translate dict update cedict") {
		t.Fatalf("missing Chinese: %+v / %v", res, err)
	}
	cedictFixture(t, dir)
	ecdictFixture(t, dir)
	de = NewLocalDict(cfg)
	for _, word := range []string{"貓", "test"} {
		ch, err := de.Translate(context.Background(), Request{Text: word})
		if err != nil {
			t.Fatal(err)
		}
		res, err := Drain(ch, nil)
		if err != nil || res.Dictionary == nil || res.Notes != "" {
			t.Fatalf("offline %s: %+v / %v", word, res, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("offline path used network: %d", requests.Load())
	}
}

func TestLocalDictCorruptionDiagnostics(t *testing.T) {
	for _, source := range []bool{true, false} {
		t.Run(fmt.Sprint(source), func(t *testing.T) {
			dir := t.TempDir()
			if source {
				cedictFixture(t, dir)
			}
			if err := os.WriteFile(CedictDBPath(dir), []byte("not sqlite"), 0600); err != nil {
				t.Fatal(err)
			}
			ch, err := NewLocalDict(LocalDictConfig{Dir: dir}).Translate(context.Background(), Request{Text: "貓"})
			if err != nil {
				t.Fatal(err)
			}
			res, err := Drain(ch, nil)
			if source {
				if err != nil || res.Dictionary == nil || !strings.Contains(res.Notes, "translate dict reindex") {
					t.Fatalf("source fallback: %+v / %v", res, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "CC-CEDICT is unusable") || errors.Is(err, ErrNoDictEntry) {
				t.Fatalf("corruption described as a miss: %v", err)
			}
		})
	}
}

type synchronousDictFailure struct{ fakeEngine }

func TestLocalDictCorruptIndexMissRetainsRepairHint(t *testing.T) {
	dir := t.TempDir()
	cedictFixture(t, dir)
	if err := os.WriteFile(CedictDBPath(dir), []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	dict := NewLocalDict(LocalDictConfig{Dir: dir, Fuzzy: true})
	req := Request{Text: "飛鳥", Mode: ModeDict}
	ch, err := dict.Translate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Drain(ch, nil)
	if !errors.Is(err, ErrNoDictEntry) || !strings.Contains(err.Error(), "translate dict reindex") {
		t.Fatalf("plain miss lost repair hint or identity: %v", err)
	}
	llm := &fakeEngine{name: "LLM", res: &TranslateResult{Translation: "bird", Engine: "LLM"}}
	ch, err = NewSmartDict(dict, llm, SmartDictConfig{}).Translate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Drain(ch, nil)
	if err != nil || !llm.called || !strings.Contains(strings.Join(res.Warnings, " "), "translate dict reindex") {
		t.Fatalf("smart fallback lost repair hint: %+v / %v", res, err)
	}
}

func (f *synchronousDictFailure) Translate(context.Context, Request) (<-chan Chunk, error) {
	return nil, fmt.Errorf("LLM connection failed")
}

func TestSmartDictPreservesSetupReason(t *testing.T) {
	for _, failure := range []string{"", "sync", "chunk"} {
		t.Run(failure, func(t *testing.T) {
			dict := &fakeEngine{name: "dictionary", res: &TranslateResult{Notes: "CC-CEDICT not installed — run `translate dict update cedict`"}}
			var llm Engine = &fakeEngine{name: "LLM", res: &TranslateResult{Translation: "cat", Notes: "other tip", Engine: "LLM"}}
			if failure == "sync" {
				llm = &synchronousDictFailure{}
			}
			if failure == "chunk" {
				llm = &fakeEngine{name: "LLM", err: fmt.Errorf("LLM completion failed")}
			}
			e := NewSmartDict(dict, llm, SmartDictConfig{})
			ch, err := e.Translate(context.Background(), Request{Text: "貓"})
			if err != nil {
				t.Fatal(err)
			}
			res, err := Drain(ch, nil)
			if failure == "" {
				if err != nil || !strings.Contains(res.Notes, "translate dict update cedict") || !strings.Contains(res.Notes, "other tip") {
					t.Fatalf("notes lost: %+v / %v", res, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "translate dict update cedict") || !strings.Contains(err.Error(), "LLM fallback failed") {
				t.Fatalf("setup context lost on error: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	llm := &fakeEngine{name: "LLM"}
	ch, err := NewSmartDict(&fakeEngine{}, llm, SmartDictConfig{}).Translate(ctx, Request{Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Drain(ch, nil)
	if !errors.Is(err, context.Canceled) || llm.called {
		t.Fatalf("cancelled lookup fell back: %v / %v", err, llm.called)
	}
}
