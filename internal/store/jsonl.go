package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sahilm/fuzzy"
)

// jsonlStore is an append-only JSONL-backed Store with in-memory fuzzy search.
type jsonlStore struct {
	path string
	mu   sync.Mutex
}

// OpenJSONL opens (creating parent dirs for) a JSONL history file.
func OpenJSONL(path string) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &jsonlStore{path: path}, nil
}

func (s *jsonlStore) Close() error { return nil }

// Add stamps the record with an id + timestamp and appends it as one JSON line.
func (s *jsonlStore) Add(ctx context.Context, r Record) (Record, error) {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.TS.IsZero() {
		r.TS = time.Now()
	}
	line, err := json.Marshal(r)
	if err != nil {
		return r, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return r, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return r, err
	}
	return r, nil
}

// readAll loads every record in chronological (file) order.
func (s *jsonlStore) readAll() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Record
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(line), &r) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// Recent returns up to limit records, newest first.
func (s *jsonlStore) Recent(ctx context.Context, limit int) ([]Record, error) {
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	reverse(all)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// Search fuzzy-ranks records by "input ⏎ output", best match first.
func (s *jsonlStore) Search(ctx context.Context, query string, limit int) ([]Record, error) {
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	q := strings.TrimSpace(query)
	if q == "" {
		reverse(all)
		if limit > 0 && len(all) > limit {
			all = all[:limit]
		}
		return all, nil
	}
	hay := make([]string, len(all))
	for i, r := range all {
		hay[i] = r.Input + " " + r.Output
	}
	matches := fuzzy.Find(q, hay)
	out := make([]Record, 0, len(matches))
	for _, m := range matches {
		out = append(out, all[m.Index])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func reverse(rs []Record) {
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
}

// newID is a sortable-enough unique id: base36 nanos + 4 random bytes.
func newID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + hex.EncodeToString(b[:])
}
