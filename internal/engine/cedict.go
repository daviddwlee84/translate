package engine

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// cedictLine matches: Traditional Simplified [pin1 yin1] /def1/def2/
var cedictLine = regexp.MustCompile(`^(\S+)\s+(\S+)\s+\[([^\]]*)\]\s+/(.*)/\s*$`)

type cedictEntry struct {
	Trad, Simp, Pinyin string
	Defs               []string
}

// cedictIndex is an in-memory CC-CEDICT (Chinese → English), keyed on both the
// traditional and simplified forms. Loaded lazily on first lookup.
type cedictIndex struct {
	path  string
	once  sync.Once
	err   error
	byKey map[string][]*cedictEntry
	keys  []string // sorted, for prefix suggestions
}

func newCedictIndex(path string) *cedictIndex { return &cedictIndex{path: path} }

func (ci *cedictIndex) load() error {
	ci.once.Do(func() {
		f, err := os.Open(ci.path)
		if err != nil {
			ci.err = err
			return
		}
		defer f.Close()
		ci.byKey = map[string][]*cedictEntry{}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "#") {
				continue
			}
			m := cedictLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			e := &cedictEntry{Trad: m[1], Simp: m[2], Pinyin: m[3]}
			for _, d := range strings.Split(m[4], "/") {
				if d = strings.TrimSpace(d); d != "" {
					e.Defs = append(e.Defs, d)
				}
			}
			ci.byKey[e.Simp] = append(ci.byKey[e.Simp], e)
			if e.Trad != e.Simp {
				ci.byKey[e.Trad] = append(ci.byKey[e.Trad], e)
			}
		}
		ci.err = sc.Err()
		ci.keys = make([]string, 0, len(ci.byKey))
		for k := range ci.byKey {
			ci.keys = append(ci.keys, k)
		}
		sort.Strings(ci.keys)
	})
	return ci.err
}

func (ci *cedictIndex) lookup(word string) []*cedictEntry {
	if ci.load() != nil {
		return nil
	}
	return ci.byKey[word]
}

// prefixSuggest returns up to n headwords beginning with word (excluding it).
// Chinese uses prefix, not edit distance (CJK edit distance is noisy).
func (ci *cedictIndex) prefixSuggest(word string, n int) []string {
	if ci.load() != nil {
		return nil
	}
	i := sort.SearchStrings(ci.keys, word)
	var out []string
	for ; i < len(ci.keys) && len(out) < n; i++ {
		k := ci.keys[i]
		if !strings.HasPrefix(k, word) {
			break
		}
		if k != word {
			out = append(out, k)
		}
	}
	return out
}

// cedictHit is one CC-CEDICT headword plus the entries filed under it. Both the
// in-memory index and the built SQLite index return this shape.
type cedictHit struct {
	Key     string
	Entries []*cedictEntry
}

// prefixSearch returns up to n headwords beginning with word, the exact match
// first, then shortest, then alphabetical — unlike prefixSuggest it *includes*
// the query itself, because a picker wants to show what you typed.
func (ci *cedictIndex) prefixSearch(word string, n int) []cedictHit {
	if ci.load() != nil || word == "" || n <= 0 {
		return nil
	}
	// Collect a wider window than n so the ranking below sees more than the first
	// n keys in raw alphabetical order.
	window := n * 8
	var keys []string
	for i := sort.SearchStrings(ci.keys, word); i < len(ci.keys) && len(keys) < window; i++ {
		if !strings.HasPrefix(ci.keys[i], word) {
			break
		}
		keys = append(keys, ci.keys[i])
	}
	keys = rankCedictKeys(keys, word, n)
	out := make([]cedictHit, 0, len(keys))
	for _, k := range keys {
		out = append(out, cedictHit{Key: k, Entries: ci.byKey[k]})
	}
	return out
}

// rankCedictKeys orders prefix matches: exact first, then by rune length, then
// alphabetically; capped at n.
func rankCedictKeys(keys []string, query string, n int) []string {
	sort.SliceStable(keys, func(i, j int) bool {
		if (keys[i] == query) != (keys[j] == query) {
			return keys[i] == query
		}
		li, lj := len([]rune(keys[i])), len([]rune(keys[j]))
		if li != lj {
			return li < lj
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys
}
