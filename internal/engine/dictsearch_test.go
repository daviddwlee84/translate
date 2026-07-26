package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// ecdictFixture builds a tiny ECDICT database in dir and returns its path. Rows
// are deliberately out of every natural order so a ranking bug can't pass by
// accident.
func ecdictFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "ecdict.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(ecdictSchema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	rows := []struct {
		word string
		frq  int
	}{
		{"testosterone", 10400},
		{"test case", 300},  // multi-word: must sort below every single word
		{"testable", 24960}, // ranked, but rare
		{"testing", 2501},
		{"testy", 0},     // unranked: must sort below every ranked word
		{"testudo", 0},   // unranked, same length tier as testing
		{"test", 575},    // the exact match: must be first regardless of frq
		{"zzz", 0},       // never a prefix match for "test"
		{"receive", 497}, // fuzzy fixtures below
		{"relieve", 3764},
		{"reeve", 0},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			`INSERT INTO entries(word,word_lc,phonetic,translation,definition,pos,frq,exchange) VALUES(?,?,?,?,?,?,?,?)`,
			r.word, strings.ToLower(r.word), "/"+r.word+"/", `zh `+r.word+`\nzh2 `+r.word, "en "+r.word, "n", r.frq, "",
		); err != nil {
			t.Fatalf("insert %q: %v", r.word, err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX idx_word_lc ON entries(word_lc)`); err != nil {
		t.Fatalf("index: %v", err)
	}
	return path
}

// prefixSearch ranks: exact first, single words before phrases, ranked words
// before unranked, then lower frq (= more common), then shorter, then alpha.
//
// The frq direction is the trap: it is a rank (1 = most common), not a count, so
// DESC would put the most obscure word on top and still look plausible.
func TestEcdictPrefixSearchRanking(t *testing.T) {
	db := newEcdictDB(ecdictFixture(t, t.TempDir()))
	rows, err := db.prefixSearch(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("prefixSearch: %v", err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Word)
	}
	want := []string{
		"test",         // exact
		"testing",      // frq 2501
		"testosterone", // frq 10400
		"testable",     // frq 24960
		"testy",        // unranked, 5 runes
		"testudo",      // unranked, 7 runes
		"test case",    // multi-word, last despite frq 300
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ranking:\n got %v\nwant %v", got, want)
	}
	if rows[0].Frq != 575 || rows[0].Phonetic != "/test/" {
		t.Fatalf("exact row lost its columns: %+v", rows[0])
	}
}

func TestEcdictPrefixSearchLimitAndEmpty(t *testing.T) {
	db := newEcdictDB(ecdictFixture(t, t.TempDir()))
	rows, err := db.prefixSearch(context.Background(), "test", 3)
	if err != nil || len(rows) != 3 {
		t.Fatalf("limit: got %d rows, err %v", len(rows), err)
	}
	if rows, err := db.prefixSearch(context.Background(), "  ", 10); err != nil || rows != nil {
		t.Fatalf("blank prefix: got %v, err %v", rows, err)
	}
	// A prefix nothing starts with is an empty result, not an error.
	if rows, err := db.prefixSearch(context.Background(), "qqqq", 10); err != nil || len(rows) != 0 {
		t.Fatalf("no-match prefix: got %d rows, err %v", len(rows), err)
	}
}

func TestEcdictLookupMany(t *testing.T) {
	db := newEcdictDB(ecdictFixture(t, t.TempDir()))
	got, err := db.lookupMany(context.Background(), []string{"receive", "Relieve", "nosuchword"})
	if err != nil {
		t.Fatalf("lookupMany: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 hits, got %d (%v)", len(got), got)
	}
	if got["receive"].Frq != 497 {
		t.Fatalf("receive frq: %+v", got["receive"])
	}
	if _, ok := got["nosuchword"]; ok {
		t.Fatal("missing word should be absent, not zero-valued")
	}
	// An empty request must not build a `IN ()` query.
	if got, err := db.lookupMany(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("empty lookupMany: %v / %v", got, err)
	}
}

func TestEcdictPrefixSearchNoDB(t *testing.T) {
	db := newEcdictDB(filepath.Join(t.TempDir(), "absent.db"))
	if _, err := db.prefixSearch(context.Background(), "test", 5); err == nil {
		t.Fatal("want an error when the database is missing")
	}
}

// --- LocalDictEngine.Search ---

// localDictFor builds an engine over a temp dir, optionally with an ECDICT
// fixture and a wordlist file.
func localDictFor(t *testing.T, withEcdict bool, wordlist []string) *LocalDictEngine {
	t.Helper()
	dir := t.TempDir()
	if withEcdict {
		ecdictFixture(t, dir)
	}
	cfg := LocalDictConfig{Dir: dir, Fuzzy: true}
	if wordlist != nil {
		wl := filepath.Join(dir, "words")
		if err := os.WriteFile(wl, []byte(strings.Join(wordlist, "\n")), 0o600); err != nil {
			t.Fatalf("wordlist: %v", err)
		}
		cfg.Wordlist = wl
	} else {
		cfg.Wordlist = filepath.Join(dir, "no-such-wordlist")
	}
	return NewLocalDict(cfg)
}

func TestSearchEmptyQuery(t *testing.T) {
	e := localDictFor(t, true, nil)
	res, err := e.Search(context.Background(), "   ", 10)
	if err != nil {
		t.Fatalf("empty query must not be an error: %v", err)
	}
	if res.Candidates == nil {
		t.Fatal("Candidates must be non-nil so it encodes as [] rather than null")
	}
	// Guard the wire contract directly: a null here breaks every JSON consumer.
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), `"candidates":[]`) {
		t.Fatalf("want an empty JSON array, got %s", b)
	}
}

func TestSearchNoDataIsNotAnError(t *testing.T) {
	e := localDictFor(t, false, nil)
	res, err := e.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("a dictionary-less install must still exit 0: %v", err)
	}
	if res.Source != "none" || len(res.Candidates) != 0 {
		t.Fatalf("want source=none with no candidates, got %+v", res)
	}
	if res.Notes == "" {
		t.Fatal("want Notes explaining how to install the dictionary")
	}
}

func TestSearchWordlistOnly(t *testing.T) {
	e := localDictFor(t, false, []string{"test", "testing", "tested", "zebra"})
	res, err := e.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Source != "wordlist" {
		t.Fatalf("want source=wordlist, got %q", res.Source)
	}
	if res.Candidates[0].Word != "test" || res.Candidates[0].Match != MatchExact {
		t.Fatalf("want the exact match first, got %+v", res.Candidates[0])
	}
	for _, c := range res.Candidates {
		if c.Word == "zebra" {
			t.Fatal("non-prefix, non-near word leaked into the results")
		}
		if c.Preview != "" {
			t.Fatalf("wordlist candidates have no definitions: %+v", c)
		}
	}
}

func TestSearchEcdictPrefixAndFuzzy(t *testing.T) {
	e := localDictFor(t, true, []string{"receive", "relieve", "reeve", "recipe"})

	// Prefix tier: exact first, previews populated from the translation column.
	res, err := e.Search(context.Background(), "test", 4)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Candidates) != 4 {
		t.Fatalf("limit not respected: %d", len(res.Candidates))
	}
	if c := res.Candidates[0]; c.Word != "test" || c.Match != MatchExact || c.Rank != 575 {
		t.Fatalf("first candidate: %+v", c)
	}
	if !strings.Contains(res.Candidates[0].Preview, "zh test") {
		t.Fatalf("preview should come from the Chinese gloss: %q", res.Candidates[0].Preview)
	}
	if strings.Contains(res.Candidates[0].Preview, `\n`) {
		t.Fatalf("preview must be one line: %q", res.Candidates[0].Preview)
	}

	// Fuzzy tier: no prefix match at all, so every candidate is a typo candidate,
	// ordered by distance and then by frequency rank.
	res, err = e.Search(context.Background(), "recieve", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Candidates) == 0 {
		t.Fatal("want typo candidates for a misspelling")
	}
	for _, c := range res.Candidates {
		if c.Match != MatchFuzzy || c.Distance == 0 {
			t.Fatalf("want fuzzy candidates with a distance: %+v", c)
		}
	}
	if got := res.Candidates[0].Word; got != "relieve" {
		// relieve is one edit away; receive is two. Distance still outranks frequency.
		t.Fatalf("want the closest word first, got %q", got)
	}
	// receive (frq 497) must beat reeve (unranked) at the same distance — this is
	// the frequency re-rank that plain edit distance gets wrong.
	iRecv, iReeve := indexOfWord(res.Candidates, "receive"), indexOfWord(res.Candidates, "reeve")
	if iRecv < 0 || iReeve < 0 || iRecv > iReeve {
		t.Fatalf("frequency re-rank failed: receive@%d reeve@%d in %v", iRecv, iReeve, wordsOf(res.Candidates))
	}
	if res.Candidates[iRecv].Preview == "" {
		t.Fatal("fuzzy candidates should be enriched with ECDICT previews")
	}
}

func TestSearchShortQuerySkipsFuzzy(t *testing.T) {
	// At two edits nearly every short word matches, so the list would be noise.
	e := localDictFor(t, true, []string{"receive", "relieve", "reeve"})
	res, err := e.Search(context.Background(), "re", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, c := range res.Candidates {
		if c.Match == MatchFuzzy {
			t.Fatalf("no fuzzy candidates below %d runes, got %+v", minFuzzyQuery, c)
		}
	}
}

func TestSearchLimitClamped(t *testing.T) {
	e := localDictFor(t, true, nil)
	for _, limit := range []int{0, -3, maxSearchLimit + 100} {
		res, err := e.Search(context.Background(), "test", limit)
		if err != nil {
			t.Fatalf("limit %d: %v", limit, err)
		}
		if len(res.Candidates) > maxSearchLimit {
			t.Fatalf("limit %d produced %d candidates", limit, len(res.Candidates))
		}
	}
}

// --- pure helpers ---

func TestPrefixUpperBounds(t *testing.T) {
	for _, p := range []string{"test", "", "貓", "zzz"} {
		upper := prefixUpper(p)
		if !(p < upper) {
			t.Fatalf("%q must sort below its upper bound %q", p, upper)
		}
		for _, suffix := range []string{"", "a", "￿", "ing", "貓貓"} {
			if s := p + suffix; !(s < upper) && s != upper {
				t.Fatalf("%q should sort below %q", s, upper)
			}
		}
		// The next string that does NOT share the prefix must sort at or above it.
		if p != "" && !(p+"\U0010FFFF" <= upper) {
			t.Fatalf("upper bound too low for %q", p)
		}
	}
}

func TestMergeCandidates(t *testing.T) {
	prefix := []Candidate{
		{Word: "test", Match: MatchExact},
		{Word: "testing", Match: MatchPrefix},
	}
	fuzzy := []Candidate{
		{Word: "Testing", Match: MatchFuzzy, Distance: 1}, // duplicate, different case
		{Word: "text", Match: MatchFuzzy, Distance: 1},
	}

	got := mergeCandidates(prefix, fuzzy, 10)
	if wordsOf(got) != "test,testing,text" {
		t.Fatalf("prefix must come first and win the dedupe: %v", wordsOf(got))
	}
	if got[1].Match != MatchPrefix {
		t.Fatalf("dedupe kept the fuzzy copy: %+v", got[1])
	}
	if got := mergeCandidates(prefix, fuzzy, 2); len(got) != 2 {
		t.Fatalf("limit ignored: %d", len(got))
	}
	if got := mergeCandidates(nil, nil, 5); got == nil || len(got) != 0 {
		t.Fatalf("want a non-nil empty slice, got %v", got)
	}
	// A blank headword would render as an empty row.
	if got := mergeCandidates([]Candidate{{Word: ""}}, nil, 5); len(got) != 0 {
		t.Fatalf("blank word should be dropped: %v", got)
	}
}

func TestSortByDistanceThenRank(t *testing.T) {
	cs := []Candidate{
		{Word: "reeve", Distance: 2, Rank: 0},
		{Word: "recipe", Distance: 2, Rank: 2509},
		{Word: "receive", Distance: 2, Rank: 497},
		{Word: "relieve", Distance: 1, Rank: 3764},
		{Word: "aaa", Distance: 2, Rank: 0},
	}
	sortByDistanceThenRank(cs)
	if got := wordsOf(cs); got != "relieve,receive,recipe,aaa,reeve" {
		t.Fatalf("order: %s", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("a  b\nc", 10); got != "a b c" {
		t.Fatalf("whitespace should collapse to one line: %q", got)
	}
	// Truncation counts runes, not bytes — CJK previews would be cut mid-character.
	if got := truncateRunes("貓貓貓貓", 2); got != "貓貓…" {
		t.Fatalf("rune truncation: %q", got)
	}
	if got := truncateRunes("short", 99); got != "short" {
		t.Fatalf("under the cap should be untouched: %q", got)
	}
}

func indexOfWord(cs []Candidate, w string) int {
	for i, c := range cs {
		if strings.EqualFold(c.Word, w) {
			return i
		}
	}
	return -1
}

func wordsOf(cs []Candidate) string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Word
	}
	return strings.Join(out, ",")
}
