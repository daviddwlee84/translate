package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/agnivade/levenshtein"

	"github.com/daviddwlee84/translate/internal/lang"
)

const (
	defaultSearchLimit = 12
	maxSearchLimit     = 50
	// maxFuzzyDistance caps how far a typo candidate may be from the query. Two
	// edits is the same budget the "did you mean" path uses.
	maxFuzzyDistance = 2
	// minFuzzyQuery is the shortest query worth running edit distance on — under
	// it nearly every short word is within two edits and the list is noise.
	minFuzzyQuery = 3
	// previewRunes caps a definition preview so it fits one list-row subtitle.
	previewRunes = 120
)

// SearchResult is the payload of a dictionary headword search. It is a top-level
// object rather than a bare array so script/source/notes can ride along — the
// same reason TranslateResult is a struct.
type SearchResult struct {
	Query      string      `json:"query"`
	Script     string      `json:"script"`     // "latin" | "han"
	Source     string      `json:"source"`     // "ecdict" | "cedict" | "wordlist" | "none"
	Candidates []Candidate `json:"candidates"` // never nil — encodes [] rather than null
	Notes      string      `json:"notes,omitempty"`
}

// Candidate is one ranked headword plus the one-line definition preview a picker
// renders as a subtitle.
type Candidate struct {
	Word     string `json:"word"`
	Phonetic string `json:"phonetic,omitempty"`
	Preview  string `json:"preview,omitempty"`
	POS      string `json:"pos,omitempty"`
	// Rank is the ECDICT frequency rank: 1 is the most common word, 0 is unranked.
	// Smaller is better — see ecdictDB.prefixSearch.
	Rank     int    `json:"rank,omitempty"`
	Match    string `json:"match"`              // "exact" | "prefix" | "fuzzy"
	Distance int    `json:"distance,omitempty"` // edit distance, when Match == "fuzzy"
}

// Match kinds, in the order a picker should show them.
const (
	MatchExact  = "exact"
	MatchPrefix = "prefix"
	MatchFuzzy  = "fuzzy"
)

// Searcher is the optional headword-search capability: given what the user has
// typed so far, return ranked candidates with previews. *LocalDictEngine
// implements it; *DictEngine (dictionaryapi.dev) does not, because a remote
// point-lookup API exposes no headword list to scan.
type Searcher interface {
	Search(ctx context.Context, q string, limit int) (*SearchResult, error)
}

// Search returns ranked dictionary headwords for a partially-typed query. It
// routes by script exactly as Translate does (Han → CC-CEDICT, else → ECDICT),
// reads only local data, and never calls the network or an LLM — it is meant to
// run on every keystroke.
//
// "Nothing found" is not an error: the result carries an empty Candidates slice
// (and possibly Notes explaining why), so callers can always exit 0. Only real
// I/O failures return a non-nil error.
func (e *LocalDictEngine) Search(ctx context.Context, q string, limit int) (*SearchResult, error) {
	q = strings.TrimSpace(q)
	switch {
	case limit <= 0:
		limit = defaultSearchLimit
	case limit > maxSearchLimit:
		limit = maxSearchLimit
	}
	res := &SearchResult{Query: q, Script: "latin", Candidates: []Candidate{}}
	if q == "" {
		res.Source = "none"
		return res, nil
	}
	if lang.IsChinese(q) {
		res.Script = "han"
		return e.searchZh(ctx, q, limit, res)
	}
	return e.searchEn(ctx, q, limit, res)
}

// searchEn ranks ECDICT headwords by prefix, then tops the list up with
// edit-distance candidates so a typo still lands somewhere useful.
func (e *LocalDictEngine) searchEn(ctx context.Context, q string, limit int, res *SearchResult) (*SearchResult, error) {
	lower := strings.ToLower(q)

	if !e.ec.available() {
		// No ECDICT: the wordlist still gives headwords, just without previews.
		res.Source = "wordlist"
		res.Notes = "ECDICT not installed — run `translate dict update ecdict` for definitions"
		prefix := wordlistCandidates(e.wl.prefixN(lower, limit), lower)
		fuzzy := e.fuzzyCandidates(lower, limit)
		res.Candidates = mergeCandidates(prefix, fuzzy, limit)
		if len(res.Candidates) == 0 {
			res.Source = "none"
		}
		return res, nil
	}

	res.Source = "ecdict"
	rows, err := e.ec.prefixSearch(ctx, lower, limit)
	if err != nil {
		return nil, err
	}
	prefix := make([]Candidate, 0, len(rows))
	for _, r := range rows {
		prefix = append(prefix, ecdictCandidate(r, lower))
	}

	var fuzzy []Candidate
	if len(prefix) < limit {
		fuzzy = e.fuzzyCandidates(lower, limit-len(prefix))
		e.enrichPreviews(ctx, fuzzy)
	}
	res.Candidates = mergeCandidates(prefix, fuzzy, limit)
	return res, nil
}

// searchZh ranks CC-CEDICT headwords by prefix. CJK gets no edit distance — the
// existing convention (see cedictIndex.prefixSuggest) is that it is too noisy.
func (e *LocalDictEngine) searchZh(ctx context.Context, q string, limit int, res *SearchResult) (*SearchResult, error) {
	res.Source = "cedict"

	// Prefer the built index: the plain .u8 is re-parsed from scratch by every
	// process (~1.7 s), which a keystroke-driven caller cannot afford.
	if e.cedb.available() {
		hits, err := e.cedb.prefixSearch(ctx, q, limit)
		if err != nil {
			return nil, err
		}
		res.Candidates = mergeCandidates(cedictCandidates(hits, q), nil, limit)
		return res, nil
	}
	if !fileExists(CedictPath(e.cfg.Dir)) {
		res.Source = "none"
		res.Notes = "CC-CEDICT not installed — run `translate dict update cedict`"
		return res, nil
	}
	res.Notes = "CC-CEDICT index not built — run `translate dict reindex` for fast Chinese search"
	res.Candidates = mergeCandidates(cedictCandidates(e.ce.prefixSearch(q, limit), q), nil, limit)
	return res, nil
}

// fuzzyCandidates returns edit-distance candidates for a (lowercased) query,
// ordered closest-first. Short queries are skipped: at two edits almost every
// short word matches, which buries the useful answers.
func (e *LocalDictEngine) fuzzyCandidates(lower string, n int) []Candidate {
	if n <= 0 || len([]rune(lower)) < minFuzzyQuery {
		return nil
	}
	words, _ := e.wl.nearestN(lower, maxFuzzyDistance, n)
	out := make([]Candidate, 0, len(words))
	for _, w := range words {
		out = append(out, Candidate{
			Word:     w,
			Match:    MatchFuzzy,
			Distance: levenshtein.ComputeDistance(lower, w),
		})
	}
	return out
}

// enrichPreviews fills in phonetic/preview/rank for fuzzy candidates with one
// batched ECDICT query, and re-sorts them by (distance, frequency) — the common
// correction should beat the obscure one at the same edit distance.
func (e *LocalDictEngine) enrichPreviews(ctx context.Context, cands []Candidate) {
	if len(cands) == 0 {
		return
	}
	words := make([]string, len(cands))
	for i, c := range cands {
		words[i] = c.Word
	}
	rows, err := e.ec.lookupMany(ctx, words)
	if err != nil {
		return // previews are a nicety; a failed enrich must not fail the search
	}
	for i := range cands {
		if r, ok := rows[strings.ToLower(cands[i].Word)]; ok {
			cands[i].Word = r.Word
			cands[i].Phonetic = r.Phonetic
			cands[i].Preview = previewOf(r)
			cands[i].POS = r.POS
			cands[i].Rank = r.Frq
		}
	}
	sortByDistanceThenRank(cands)
}

// mergeCandidates folds the prefix and fuzzy tiers into one deduped list capped
// at limit. Prefix matches keep the order they arrived in (the SQL already ranked
// them) and always beat a fuzzy match for the same headword.
func mergeCandidates(prefix, fuzzy []Candidate, limit int) []Candidate {
	out := make([]Candidate, 0, limit)
	seen := map[string]bool{}
	for _, group := range [][]Candidate{prefix, fuzzy} {
		for _, c := range group {
			key := strings.ToLower(c.Word)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, c)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// sortByDistanceThenRank orders typo candidates by edit distance, then by
// frequency rank (ascending, unranked last), then alphabetically.
func sortByDistanceThenRank(cs []Candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		if (a.Rank == 0) != (b.Rank == 0) {
			return b.Rank == 0 // a ranked word beats an unranked one
		}
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		return a.Word < b.Word
	})
}

// --- row → candidate mapping ---

func ecdictCandidate(r ecdictRow, lowerQuery string) Candidate {
	match := MatchPrefix
	if strings.EqualFold(r.Word, lowerQuery) {
		match = MatchExact
	}
	return Candidate{
		Word:     r.Word,
		Phonetic: r.Phonetic,
		Preview:  previewOf(r),
		POS:      r.POS,
		Rank:     r.Frq,
		Match:    match,
	}
}

func cedictCandidates(hits []cedictHit, query string) []Candidate {
	out := make([]Candidate, 0, len(hits))
	for _, h := range hits {
		c := Candidate{Word: h.Key, Match: MatchPrefix}
		if h.Key == query {
			c.Match = MatchExact
		}
		if len(h.Entries) > 0 {
			c.Phonetic = h.Entries[0].Pinyin
			c.Preview = truncateRunes(strings.Join(h.Entries[0].Defs, "; "), previewRunes)
		}
		out = append(out, c)
	}
	return out
}

func wordlistCandidates(words []string, lowerQuery string) []Candidate {
	out := make([]Candidate, 0, len(words))
	for _, w := range words {
		match := MatchPrefix
		if w == lowerQuery {
			match = MatchExact
		}
		out = append(out, Candidate{Word: w, Match: match})
	}
	return out
}

// previewOf renders one ECDICT row as a single subtitle line, preferring the
// Chinese gloss over the English definition.
func previewOf(r ecdictRow) string {
	parts := splitEcdict(r.Translation)
	if len(parts) == 0 {
		parts = splitEcdict(r.Definition)
	}
	return truncateRunes(strings.Join(parts, "; "), previewRunes)
}

func truncateRunes(s string, n int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return strings.TrimSpace(string(rs[:n])) + "…"
}

// prefixUpper returns the exclusive upper bound for a BINARY-collated prefix
// scan: every string starting with p sorts below p+U+10FFFF, and nothing else
// does. Used instead of LIKE so the word_lc index is actually usable.
func prefixUpper(p string) string { return p + "\U0010FFFF" }
