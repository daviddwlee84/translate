package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cedictFixture writes a small CC-CEDICT file and returns its path. It covers
// the shapes the parser has to get right: a comment line, an entry whose
// traditional and simplified forms differ (filed under both keys), an entry
// where they are identical (filed once), multiple definitions, and a headword
// that is a prefix of longer ones.
func cedictFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "cedict_ts.u8")
	lines := []string{
		"# CC-CEDICT test fixture",
		"貓 猫 [mao1] /cat/(dialect) to hide oneself/",
		"貓咪 猫咪 [mao1 mi1] /kitty/",
		"貓熊 猫熊 [mao1 xiong2] /see 熊貓|熊猫/",
		"貓兒 猫儿 [mao1 er2] /kitten/",
		"你好 你好 [ni3 hao3] /hello/hi/",
		"狗 狗 [gou3] /dog/",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return path
}

func TestCedictIndexPrefixSearch(t *testing.T) {
	dir := t.TempDir()
	ci := newCedictIndex(cedictFixture(t, dir))

	hits := ci.prefixSearch("貓", 10)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Key != "貓" {
		t.Fatalf("the exact headword must come first, got %q", hits[0].Key)
	}
	if len(hits[0].Entries) == 0 || hits[0].Entries[0].Pinyin != "mao1" {
		t.Fatalf("entries missing from the exact hit: %+v", hits[0])
	}
	// Unlike prefixSuggest, prefixSearch includes the query itself.
	if got := ci.prefixSuggest("貓", 10); len(got) != len(hits)-1 {
		t.Fatalf("prefixSuggest should exclude the query: %v vs %d hits", got, len(hits))
	}
	// Shorter headwords first.
	for i := 1; i < len(hits); i++ {
		if len([]rune(hits[i-1].Key)) > len([]rune(hits[i].Key)) {
			t.Fatalf("not ordered by length: %v", keysOf(hits))
		}
	}
	// A simplified form resolves too — CC-CEDICT files each entry under both.
	if hits := ci.prefixSearch("猫", 10); len(hits) == 0 || hits[0].Key != "猫" {
		t.Fatalf("simplified key lookup failed: %v", keysOf(hits))
	}
	if hits := ci.prefixSearch("狗", 10); len(hits) != 1 {
		t.Fatalf("trad == simp should be filed once, got %v", keysOf(hits))
	}
	if hits := ci.prefixSearch("鳥", 10); len(hits) != 0 {
		t.Fatalf("unknown prefix should be empty, got %v", keysOf(hits))
	}
}

// The built index must answer identically to the in-memory one. Without this,
// the two lookup paths could silently drift and Chinese results would depend on
// whether `dict reindex` had been run.
func TestBuildCedictDBRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := cedictFixture(t, dir)
	dbPath := filepath.Join(dir, "cedict.db")
	if err := BuildCedictDB(context.Background(), src, dbPath, nil); err != nil {
		t.Fatalf("BuildCedictDB: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("index not written: %v", err)
	}

	ci := newCedictIndex(src)
	cd := newCedictDB(dbPath)
	if !cd.available() {
		t.Fatal("available() false for a freshly built index")
	}
	ctx := context.Background()

	for _, word := range []string{"貓", "猫", "你好", "狗", "nope"} {
		want := ci.lookup(word)
		got, err := cd.lookup(ctx, word)
		if err != nil {
			t.Fatalf("lookup %q: %v", word, err)
		}
		if len(got) != len(want) {
			t.Fatalf("lookup %q: %d entries from the index, %d from the file", word, len(got), len(want))
		}
		for i := range want {
			if got[i].Pinyin != want[i].Pinyin ||
				strings.Join(got[i].Defs, "|") != strings.Join(want[i].Defs, "|") ||
				got[i].Trad != want[i].Trad || got[i].Simp != want[i].Simp {
				t.Fatalf("lookup %q entry %d diverged:\n index %+v\n  file %+v", word, i, got[i], want[i])
			}
		}
	}

	for _, prefix := range []string{"貓", "猫", "你", "鳥"} {
		want := ci.prefixSearch(prefix, 10)
		got, err := cd.prefixSearch(ctx, prefix, 10)
		if err != nil {
			t.Fatalf("prefixSearch %q: %v", prefix, err)
		}
		if keysOf(got) != keysOf(want) {
			t.Fatalf("prefixSearch %q diverged:\n index %s\n  file %s", prefix, keysOf(got), keysOf(want))
		}
	}

	// Definitions contain "/" (the CC-CEDICT delimiter), so the stored column
	// must not be split on it.
	hits, err := cd.prefixSearch(ctx, "貓熊", 1)
	if err != nil || len(hits) != 1 {
		t.Fatalf("貓熊: %v / %v", hits, err)
	}
	if len(hits[0].Entries[0].Defs) != 1 || !strings.Contains(hits[0].Entries[0].Defs[0], "熊貓|熊猫") {
		t.Fatalf("definition containing a slash was mangled: %+v", hits[0].Entries[0].Defs)
	}

	if hits, err := cd.prefixSearch(ctx, "  ", 10); err != nil || hits != nil {
		t.Fatalf("blank prefix: %v / %v", hits, err)
	}
}

func TestBuildCedictDBMissingSource(t *testing.T) {
	dir := t.TempDir()
	err := BuildCedictDB(context.Background(), filepath.Join(dir, "absent.u8"), filepath.Join(dir, "out.db"), nil)
	if err == nil {
		t.Fatal("want an error for a missing source file")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out.db")); statErr == nil {
		t.Fatal("a failed build must not leave a database behind")
	}
}

func TestCedictIndexMissingFile(t *testing.T) {
	ci := newCedictIndex(filepath.Join(t.TempDir(), "absent.u8"))
	if got := ci.lookup("貓"); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
	if got := ci.prefixSearch("貓", 5); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

// Chinese search prefers the built index and says so when it is missing, so the
// UI can point at `dict reindex` instead of silently being slow.
func TestSearchZh(t *testing.T) {
	dir := t.TempDir()
	src := cedictFixture(t, dir)
	e := NewLocalDict(LocalDictConfig{Dir: dir, Fuzzy: true})
	ctx := context.Background()

	res, err := e.Search(ctx, "貓", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Script != "han" || res.Source != "cedict" {
		t.Fatalf("routing: %+v", res)
	}
	if res.Notes == "" {
		t.Fatal("want a hint that the index is not built")
	}
	if res.Candidates[0].Word != "貓" || res.Candidates[0].Match != MatchExact {
		t.Fatalf("first candidate: %+v", res.Candidates[0])
	}
	if res.Candidates[0].Phonetic != "mao1" || !strings.Contains(res.Candidates[0].Preview, "cat") {
		t.Fatalf("candidate lost its pinyin/preview: %+v", res.Candidates[0])
	}
	unindexed := wordsOf(res.Candidates)

	if err := BuildCedictDB(ctx, src, CedictDBPath(dir), nil); err != nil {
		t.Fatalf("BuildCedictDB: %v", err)
	}
	e2 := NewLocalDict(LocalDictConfig{Dir: dir, Fuzzy: true})
	res2, err := e2.Search(ctx, "貓", 10)
	if err != nil {
		t.Fatalf("Search (indexed): %v", err)
	}
	if res2.Notes != "" {
		t.Fatalf("no hint once the index exists: %q", res2.Notes)
	}
	if got := wordsOf(res2.Candidates); got != unindexed {
		t.Fatalf("indexed and unindexed search disagree:\n%s\n%s", got, unindexed)
	}
}

func TestSearchZhNoDictionary(t *testing.T) {
	e := NewLocalDict(LocalDictConfig{Dir: t.TempDir(), Fuzzy: true})
	res, err := e.Search(context.Background(), "貓", 10)
	if err != nil {
		t.Fatalf("missing CC-CEDICT must not be an error: %v", err)
	}
	if res.Source != "none" || len(res.Candidates) != 0 || res.Notes == "" {
		t.Fatalf("want an empty result with an install hint, got %+v", res)
	}
}

func keysOf(hits []cedictHit) string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Key
	}
	return strings.Join(out, ",")
}
