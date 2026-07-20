// Package bitext splits piped terminal input into blank-line-delimited blocks and
// renders a bilingual ("immersive") view: each original block is kept verbatim
// (ANSI/color intact) and a translation is shown beneath prose blocks. It powers
// the CLI's --bilingual pipe mode. ANSI stripping is delegated to
// github.com/charmbracelet/x/ansi so the tokenizer stays wcwidth/CJK-aware.
package bitext

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Kind classifies a block of piped input.
type Kind int

const (
	// Blank is a whitespace-only line, preserved so original spacing survives.
	Blank Kind = iota
	// Prose is natural-language text — the only kind that gets translated.
	Prose
	// Code is an indented command/example block — echoed verbatim, not translated.
	Code
)

// Block is one unit of piped input: a run of consecutive non-blank lines, or a
// single blank line.
type Block struct {
	Raw   string // original line(s) joined by "\n", ANSI escapes intact (for display)
	Plain string // Raw with all ANSI/SGR escapes stripped (for the LLM + classification)
	Kind  Kind
}

// Strip removes every ANSI/SGR escape sequence from s, returning display text.
func Strip(s string) string { return ansi.Strip(s) }

// codeIndent is how many columns DEEPER than the document's base margin a block
// must be indented to count as a command/code example. Tools like tldr indent the
// whole body (prose at a base margin, examples deeper), so detection is relative to
// that base — an absolute threshold would misread the indented prose as code.
const codeIndent = 2

// Split breaks raw piped input into blocks. Consecutive non-blank lines coalesce
// into one block (grouping soft-wrapped prose so it translates with context); each
// blank line becomes its own Blank block so original spacing is reproduced. A
// trailing newline is trimmed so it does not yield a spurious final blank block.
func Split(raw string) []Block {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")

	// The document's base margin: the minimum leading indentation over all
	// non-blank lines. tldr/man indent the entire body, so "code" is measured
	// relative to this, not from column zero.
	base := -1
	for _, ln := range lines {
		p := Strip(ln)
		if strings.TrimSpace(p) == "" {
			continue
		}
		if c := leadingCols(p); base < 0 || c < base {
			base = c
		}
	}
	if base < 0 {
		base = 0
	}

	var blocks []Block
	var cur []string // current run of non-blank raw lines

	flush := func() {
		if len(cur) == 0 {
			return
		}
		rawBlk := strings.Join(cur, "\n")
		blocks = append(blocks, Block{
			Raw:   rawBlk,
			Plain: Strip(rawBlk),
			Kind:  classify(cur, base),
		})
		cur = nil
	}

	for _, ln := range lines {
		if strings.TrimSpace(Strip(ln)) == "" {
			flush()
			blocks = append(blocks, Block{Raw: ln, Kind: Blank})
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return blocks
}

// classify returns Code when every (stripped) line is indented at least codeIndent
// columns deeper than the document's base margin, else Prose.
func classify(rawLines []string, base int) Kind {
	for _, ln := range rawLines {
		if leadingCols(Strip(ln)) < base+codeIndent {
			return Prose
		}
	}
	return Code
}

// leadingCols counts leading whitespace columns, treating a tab as codeIndent.
func leadingCols(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ':
			n++
		case '\t':
			n += codeIndent
		default:
			return n
		}
	}
	return n
}

// Render assembles the bilingual output. Each block's Raw is emitted verbatim; for
// Prose blocks whose index has a translation, each translation line is emitted
// beneath — the first prefixed "  ↳ ", continuations aligned under it — and passed
// through dim (identity when styling is off). Code and Blank blocks pass through
// untouched.
func Render(blocks []Block, translations map[int]string, dim func(string) string) string {
	if dim == nil {
		dim = func(s string) string { return s }
	}
	var b strings.Builder
	for i, blk := range blocks {
		b.WriteString(blk.Raw)
		b.WriteByte('\n')
		tr, ok := translations[i]
		if !ok || blk.Kind != Prose {
			continue
		}
		for j, ln := range strings.Split(strings.TrimRight(tr, "\n"), "\n") {
			prefix := "  ↳ "
			if j > 0 {
				prefix = "    "
			}
			b.WriteString(dim(prefix + ln))
			b.WriteByte('\n')
		}
	}
	return b.String()
}
