package bitext

import "strings"

// IsTabular reports whether text looks like a table rendered for a terminal.
//
// It exists because a table is the one shape whose LAYOUT cannot survive being
// translated. Column alignment is achieved with padding measured in terminal
// cells, and a CJK glyph occupies two of those — so translating an aligned table
// into Chinese breaks the alignment no matter how faithful the translation is,
// and any consumer that renders in a proportional font (the Raycast Detail view)
// breaks it a second time. When this returns true the caller asks the model for
// markdown-table STRUCTURE instead, which does survive.
//
// Two shapes count, matching how tables actually arrive on stdin:
//
//   - pipe-delimited: two or more lines carrying at least two "|" each
//   - column-aligned: three or more lines that share a run of whitespace
//     starting at the same column, the way `ls -l` or a `--help` table does
//
// Deliberately conservative: a false positive costs a few tokens of prompt and
// a table-shaped answer to something that was not a table, so the thresholds sit
// where a single coincidental line cannot trip them.
func IsTabular(text string) bool {
	lines := splitNonBlank(text)
	if len(lines) < 2 {
		return false
	}
	if countPipeRows(lines) >= 2 {
		return true
	}
	return len(lines) >= 3 && hasSharedColumnGap(lines)
}

func splitNonBlank(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func countPipeRows(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.Count(l, "|") >= 2 {
			n++
		}
	}
	return n
}

// hasSharedColumnGap reports whether at least three lines start a field at the
// same column, the signature of hand-aligned columns.
//
// Note it keys on where content RESUMES after a run of spaces, not where the run
// begins: in
//
//	NAME      SIZE
//	foo       12
//
// the gaps begin at columns 4 and 3, but both second fields begin at column 10.
// The field start is the invariant; the gap start is an artifact of the previous
// cell's length.
func hasSharedColumnGap(lines []string) bool {
	counts := map[int]int{}
	for _, l := range lines {
		for _, col := range fieldStarts(l) {
			counts[col]++
			if counts[col] >= 3 {
				return true
			}
		}
	}
	return false
}

// fieldStarts returns the rune-index at which each field begins after a run of
// 2+ spaces. Leading indentation is skipped — it is not a column boundary.
func fieldStarts(line string) []int {
	var out []int
	runes := []rune(line)
	i := 0
	for i < len(runes) && runes[i] == ' ' {
		i++
	}
	for i < len(runes) {
		if runes[i] != ' ' {
			i++
			continue
		}
		start := i
		for i < len(runes) && runes[i] == ' ' {
			i++
		}
		if i-start >= 2 && i < len(runes) {
			out = append(out, i)
		}
	}
	return out
}
