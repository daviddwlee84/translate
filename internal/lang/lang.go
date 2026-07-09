// Package lang resolves language names/codes with typo tolerance and provides
// offline language detection. It is shared by the CLI and the TUI.
package lang

import (
	"sort"
	"strings"

	"github.com/agnivade/levenshtein"
)

// Lang is one known language: its ISO-639-1 code, English name, and aliases
// (native names, ISO-639-2 codes, common misspellings).
type Lang struct {
	Code    string
	Name    string
	Aliases []string
}

// Match is the result of resolving a query to a language.
type Match struct {
	Code  string
	Name  string
	Score float64 // 1.0 == exact; lower for fuzzy
	Exact bool
}

// table is a pragmatic set of common languages. Fuzzy matching handles typos
// on the Latin names, so aliases mainly carry native scripts and ISO-639-2.
var table = []Lang{
	{"en", "english", []string{"eng"}},
	{"zh", "chinese", []string{"mandarin", "zho", "chi", "中文", "汉语", "漢語", "普通话"}},
	{"zh-TW", "chinese (traditional)", []string{"traditional chinese", "chinese traditional", "zh-hant", "zht", "繁體中文", "繁体中文", "繁中", "taiwanese mandarin", "taiwan"}},
	{"zh-CN", "chinese (simplified)", []string{"simplified chinese", "chinese simplified", "zh-hans", "zhs", "简体中文", "简中", "mainland chinese"}},
	{"es", "spanish", []string{"spa", "español", "castellano"}},
	{"fr", "french", []string{"fra", "fre", "français"}},
	{"de", "german", []string{"deu", "ger", "deutsch"}},
	{"ja", "japanese", []string{"jpn", "日本語"}},
	{"ko", "korean", []string{"kor", "한국어", "조선말"}},
	{"pt", "portuguese", []string{"por", "português"}},
	{"it", "italian", []string{"ita", "italiano"}},
	{"ru", "russian", []string{"rus", "русский"}},
	{"ar", "arabic", []string{"ara", "العربية"}},
	{"hi", "hindi", []string{"hin", "हिन्दी"}},
	{"nl", "dutch", []string{"nld", "dut", "nederlands"}},
	{"pl", "polish", []string{"pol", "polski"}},
	{"tr", "turkish", []string{"tur", "türkçe"}},
	{"vi", "vietnamese", []string{"vie", "tiếng việt"}},
	{"th", "thai", []string{"tha", "ไทย"}},
	{"id", "indonesian", []string{"ind", "bahasa indonesia"}},
	{"uk", "ukrainian", []string{"ukr", "українська"}},
	{"sv", "swedish", []string{"swe", "svenska"}},
	{"cs", "czech", []string{"ces", "cze", "čeština"}},
	{"el", "greek", []string{"ell", "gre", "ελληνικά"}},
	{"he", "hebrew", []string{"heb", "עברית", "iw"}},
	{"ro", "romanian", []string{"ron", "rum", "română"}},
	{"hu", "hungarian", []string{"hun", "magyar"}},
	{"fi", "finnish", []string{"fin", "suomi"}},
	{"da", "danish", []string{"dan", "dansk"}},
	{"no", "norwegian", []string{"nor", "norsk", "nb", "nn"}},
	{"fa", "persian", []string{"fas", "per", "farsi", "فارسی"}},
	{"bn", "bengali", []string{"ben", "বাংলা"}},
	{"ta", "tamil", []string{"tam", "தமிழ்"}},
	{"ms", "malay", []string{"msa", "may", "bahasa melayu"}},
	{"ca", "catalan", []string{"cat", "català"}},
}

// candidate is one searchable token (code, name, or alias) pointing at a Lang.
type candidate struct {
	token string
	lang  *Lang
	// weight scales the fuzzy score: exact code/name matches are preferred.
	primary bool
}

var candidates = buildCandidates()

func buildCandidates() []candidate {
	var cs []candidate
	for i := range table {
		l := &table[i]
		cs = append(cs, candidate{token: strings.ToLower(l.Code), lang: l, primary: true})
		cs = append(cs, candidate{token: strings.ToLower(l.Name), lang: l, primary: true})
		for _, a := range l.Aliases {
			cs = append(cs, candidate{token: strings.ToLower(a), lang: l})
		}
	}
	return cs
}

// Resolve maps a free-form query to a language, tolerating typos. It returns the
// best match plus ranked alternatives (for TUI disambiguation). "auto" and ""
// pass through as an auto-detect sentinel (Code "auto", Exact true).
func Resolve(q string) (Match, []Match) {
	norm := strings.ToLower(strings.TrimSpace(q))
	if norm == "" || norm == "auto" {
		return Match{Code: "auto", Name: "auto-detect", Score: 1, Exact: true}, nil
	}

	// Exact match on any code / name / alias.
	for _, c := range candidates {
		if c.token == norm {
			return Match{Code: c.lang.Code, Name: c.lang.Name, Score: 1, Exact: true}, nil
		}
	}

	// Fuzzy: rank candidates by normalized Levenshtein similarity.
	type scored struct {
		lang  *Lang
		score float64
	}
	best := map[string]scored{} // dedupe by language code, keep best score
	for _, c := range candidates {
		d := levenshtein.ComputeDistance(norm, c.token)
		maxLen := max(len(norm), len(c.token))
		if maxLen == 0 {
			continue
		}
		sim := 1 - float64(d)/float64(maxLen)
		if !c.primary {
			sim -= 0.05 // slight preference for code/name over alias
		}
		if cur, ok := best[c.lang.Code]; !ok || sim > cur.score {
			best[c.lang.Code] = scored{lang: c.lang, score: sim}
		}
	}

	ranked := make([]Match, 0, len(best))
	for _, s := range best {
		ranked = append(ranked, Match{Code: s.lang.Code, Name: s.lang.Name, Score: s.score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

	if len(ranked) == 0 || ranked[0].Score < 0.5 {
		// No confident match; echo the query back as an opaque code so callers
		// can still attempt it (e.g. a code we don't have in the table).
		return Match{Code: norm, Name: norm, Score: 0}, ranked
	}
	top := 3
	if len(ranked) < top {
		top = len(ranked)
	}
	return ranked[0], ranked[1:top]
}

// List returns all known languages (for the target-language picker).
func List() []Lang {
	out := make([]Lang, len(table))
	copy(out, table)
	return out
}

// Name returns the English name for a code, or the code itself if unknown.
func Name(code string) string {
	if strings.EqualFold(code, "auto") || code == "" {
		return "auto-detect"
	}
	for i := range table {
		if strings.EqualFold(table[i].Code, code) {
			return table[i].Name
		}
	}
	return code
}
