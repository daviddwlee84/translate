package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agnivade/levenshtein"
)

// suggestLimit caps the number of ranked "did you mean" candidates on a miss.
const suggestLimit = 7

// DictConfig configures the dictionary engine.
type DictConfig struct {
	Endpoint string // base, e.g. https://api.dictionaryapi.dev/api/v2/entries
	Lang     string // e.g. "en"
	Fuzzy    bool
	Wordlist string // path to a newline word list; "" => /usr/share/dict/words
	Timeout  time.Duration
}

// DictEngine looks up word definitions via the Free Dictionary API, with an
// exact match first and a local fuzzy (nearest-headword) fallback on 404.
type DictEngine struct {
	cfg  DictConfig
	wl   *wordIndex
	http *http.Client
}

// NewDict builds a dictionary engine.
func NewDict(cfg DictConfig) *DictEngine {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.dictionaryapi.dev/api/v2/entries"
	}
	if cfg.Lang == "" {
		cfg.Lang = "en"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	path := cfg.Wordlist
	if path == "" {
		path = "/usr/share/dict/words"
	}
	return &DictEngine{cfg: cfg, wl: &wordIndex{path: path}, http: &http.Client{Timeout: cfg.Timeout}}
}

// Name returns "dictionary".
func (e *DictEngine) Name() string { return "dictionary" }

// Supports reports that this engine handles dictionary lookups only.
func (e *DictEngine) Supports(m Mode) bool { return m == ModeDict }

// Detect is not meaningful for the dictionary engine.
func (e *DictEngine) Detect(ctx context.Context, text string) (string, error) { return "", nil }

// Available probes the API with a common word.
func (e *DictEngine) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	_, status, err := e.fetch(ctx, "test")
	return err == nil && status == http.StatusOK
}

// Translate performs a dictionary lookup (exact, then fuzzy on 404).
func (e *DictEngine) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	word := strings.ToLower(strings.TrimSpace(req.Text))
	if word == "" {
		return nil, ErrEmptyInput
	}

	entry, status, err := e.fetch(ctx, word)
	if err == nil && status == http.StatusOK && entry != nil {
		return single(e.result(entry, req), nil), nil
	}
	if err != nil {
		return single(nil, fmt.Errorf("dictionary: %w", err)), nil
	}

	// 404: return a ranked "did you mean" list (the frontend chooses one). Gated
	// to English because the bundled wordlist (/usr/share/dict/words) is English.
	if status == http.StatusNotFound && e.cfg.Fuzzy && strings.HasPrefix(strings.ToLower(e.cfg.Lang), "en") {
		if cands, best := e.wl.nearestN(word, 2, suggestLimit); len(cands) > 0 {
			return single(&TranslateResult{Target: req.Target, Engine: e.Name(), Suggestions: cands, SuggestDistance: best}, nil), nil
		}
	}
	return single(nil, fmt.Errorf("dictionary: %w: %q", ErrNoDictEntry, word)), nil
}

func (e *DictEngine) result(entry *DictEntry, req Request) *TranslateResult {
	return &TranslateResult{
		Translation: entry.gloss(),
		Target:      req.Target,
		Engine:      e.Name(),
		Dictionary:  entry,
	}
}

// --- Free Dictionary API wire types ---

type apiEntry struct {
	Word       string        `json:"word"`
	Phonetic   string        `json:"phonetic"`
	Phonetics  []apiPhonetic `json:"phonetics"`
	Meanings   []apiMeaning  `json:"meanings"`
	SourceURLs []string      `json:"sourceUrls"`
}
type apiPhonetic struct {
	Text string `json:"text"`
}
type apiMeaning struct {
	PartOfSpeech string          `json:"partOfSpeech"`
	Definitions  []apiDefinition `json:"definitions"`
	Synonyms     []string        `json:"synonyms"`
	Antonyms     []string        `json:"antonyms"`
}
type apiDefinition struct {
	Definition string `json:"definition"`
	Example    string `json:"example"`
}

// fetch requests one word; returns (entry, httpStatus, err). A 404 yields
// (nil, 404, nil) so the caller can trigger the fuzzy fallback.
func (e *DictEngine) fetch(ctx context.Context, word string) (*DictEntry, int, error) {
	u := fmt.Sprintf("%s/%s/%s", strings.TrimRight(e.cfg.Endpoint, "/"), e.cfg.Lang, urlPathEscape(word))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, resp.StatusCode, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var entries []apiEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode: %w", err)
	}
	if len(entries) == 0 {
		return nil, http.StatusNotFound, nil
	}
	return mergeEntries(entries), http.StatusOK, nil
}

// mergeEntries folds the API's per-entry results into one DictEntry.
func mergeEntries(entries []apiEntry) *DictEntry {
	out := &DictEntry{Word: entries[0].Word}
	for _, en := range entries {
		if out.Phonetic == "" && en.Phonetic != "" {
			out.Phonetic = en.Phonetic
		}
		for _, p := range en.Phonetics {
			if out.Phonetic == "" && p.Text != "" {
				out.Phonetic = p.Text
			}
		}
		for _, m := range en.Meanings {
			meaning := Meaning{PartOfSpeech: m.PartOfSpeech, Synonyms: m.Synonyms, Antonyms: m.Antonyms}
			for _, d := range m.Definitions {
				meaning.Definitions = append(meaning.Definitions, Definition{Text: d.Definition, Example: d.Example})
			}
			out.Meanings = append(out.Meanings, meaning)
		}
		if out.SourceURL == "" && len(en.SourceURLs) > 0 {
			out.SourceURL = en.SourceURLs[0]
		}
	}
	return out
}

// gloss returns a short one-line definition for history/plain display.
func (d *DictEntry) gloss() string {
	for _, m := range d.Meanings {
		for _, def := range m.Definitions {
			if def.Text != "" {
				return def.Text
			}
		}
	}
	return d.Word
}

func urlPathEscape(s string) string {
	// dictionary words are simple; escape spaces defensively.
	return strings.ReplaceAll(s, " ", "%20")
}

// --- local fuzzy word index ---

type wordIndex struct {
	path  string
	once  sync.Once
	words []string
}

func (wi *wordIndex) load() {
	wi.once.Do(func() {
		b, err := os.ReadFile(wi.path)
		if err != nil {
			return // no wordlist: fuzzy silently disabled
		}
		for _, line := range strings.Split(string(b), "\n") {
			w := strings.ToLower(strings.TrimSpace(line))
			if w != "" {
				wi.words = append(wi.words, w)
			}
		}
	})
}

// nearestN returns up to n headwords within maxDist edits of word, closest first
// (ties broken alphabetically), excluding word itself. The second return is the
// best (smallest) edit distance among the results, or 0 when there are none.
func (wi *wordIndex) nearestN(word string, maxDist, n int) ([]string, int) {
	wi.load()
	type cand struct {
		w string
		d int
	}
	var cs []cand
	for _, w := range wi.words {
		if w == word || abs(len(w)-len(word)) > maxDist {
			continue
		}
		if d := levenshtein.ComputeDistance(word, w); d <= maxDist {
			cs = append(cs, cand{w, d})
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].d != cs[j].d {
			return cs[i].d < cs[j].d
		}
		return cs[i].w < cs[j].w
	})
	if len(cs) > n {
		cs = cs[:n]
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.w
	}
	best := 0
	if len(cs) > 0 {
		best = cs[0].d // slice is sorted by distance ascending
	}
	return out, best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
