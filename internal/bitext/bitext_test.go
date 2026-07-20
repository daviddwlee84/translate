package bitext

import (
	"strings"
	"testing"
)

func TestStrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"sgr", "\x1b[1;31mHello\x1b[0m", "Hello"},
		{"sgr_cjk", "\x1b[32m世界\x1b[0m!", "世界!"},
		{"mixed", "a \x1b[31mfaster\x1b[0m alt to \x1b[31mgrep\x1b[0m.", "a faster alt to grep."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Strip(c.in); got != c.want {
				t.Errorf("Strip(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSplit(t *testing.T) {
	// Real tldr output indents the WHOLE body: prose at 2 columns, command
	// examples at 4. Classification is relative to the base margin (this fixture
	// guards the bug where 2-space-indented prose was misread as code).
	input := "  \x1b[35mrg\x1b[0m\n\n" +
		"  Ripgrep, a \x1b[32mfast\x1b[0m tool.\n  More info.\n\n" +
		"    rg \x1b[31mpattern\x1b[0m\n"

	blocks := Split(input)
	want := []struct {
		kind  Kind
		plain string
	}{
		{Prose, "  rg"},
		{Blank, ""},
		{Prose, "  Ripgrep, a fast tool.\n  More info."},
		{Blank, ""},
		{Code, "    rg pattern"},
	}
	if len(blocks) != len(want) {
		t.Fatalf("Split returned %d blocks, want %d: %#v", len(blocks), len(want), blocks)
	}
	for i, w := range want {
		if blocks[i].Kind != w.kind {
			t.Errorf("block %d Kind = %d, want %d", i, blocks[i].Kind, w.kind)
		}
		if blocks[i].Plain != w.plain {
			t.Errorf("block %d Plain = %q, want %q", i, blocks[i].Plain, w.plain)
		}
	}
	// Raw must retain the original ANSI so display keeps color.
	if !strings.Contains(blocks[0].Raw, "\x1b[35m") {
		t.Errorf("block 0 Raw lost ANSI: %q", blocks[0].Raw)
	}
	if !strings.Contains(blocks[4].Raw, "\x1b[31m") {
		t.Errorf("code block Raw lost ANSI: %q", blocks[4].Raw)
	}
}

func TestClassifyRelativeToBase(t *testing.T) {
	// Code is "indented deeper than the document's base margin", not an absolute
	// column — so the same 4-col example reads as Code whether prose sits at 0 or 2.
	cases := []struct {
		name string
		in   string
		want []Kind
	}{
		{"base0_prose_then_code", "Search for a pattern:\n\n    rg pattern", []Kind{Prose, Blank, Code}},
		{"base2_prose_then_code", "  Search for a pattern:\n\n    rg pattern", []Kind{Prose, Blank, Code}},
		{"uniform_indent_all_prose", "  line one\n  line two", []Kind{Prose}},
		{"tab_deeper_is_code", "desc\n\n\tcmd --flag", []Kind{Prose, Blank, Code}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blocks := Split(c.in)
			if len(blocks) != len(c.want) {
				t.Fatalf("got %d blocks, want %d: %#v", len(blocks), len(c.want), blocks)
			}
			for i, k := range c.want {
				if blocks[i].Kind != k {
					t.Errorf("block %d Kind = %d, want %d", i, blocks[i].Kind, k)
				}
			}
		})
	}
}

func TestSplitEmpty(t *testing.T) {
	// Empty and pure-newline input trims to nothing — no blocks to translate.
	for _, in := range []string{"", "\n", "\n\n"} {
		if got := Split(in); got != nil {
			t.Errorf("Split(%q) = %#v, want nil", in, got)
		}
	}
	// A blank line between two prose lines is preserved as a Blank block.
	if got := Split("a\n\nb"); len(got) != 3 || got[1].Kind != Blank {
		t.Errorf("Split(a/blank/b) = %#v, want 3 blocks with middle Blank", got)
	}
}

func TestRender(t *testing.T) {
	blocks := []Block{
		{Raw: "\x1b[35mrg\x1b[0m", Plain: "rg", Kind: Prose},
		{Raw: "", Kind: Blank},
		{Raw: "A search tool.", Plain: "A search tool.", Kind: Prose},
		{Raw: "    rg pattern", Plain: "    rg pattern", Kind: Code},
	}
	translations := map[int]string{
		0: "rg",                // echo — caller may drop it, but Render honors the map
		2: "一個搜尋工具。",           // prose translation
		3: "should be ignored", // Code must never render a translation
	}
	out := Render(blocks, translations, nil) // nil dim => identity

	if !strings.Contains(out, "  ↳ 一個搜尋工具。") {
		t.Errorf("prose translation missing/malformed:\n%s", out)
	}
	if strings.Contains(out, "should be ignored") {
		t.Errorf("Code block was translated:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[35mrg\x1b[0m") {
		t.Errorf("original ANSI not preserved:\n%s", out)
	}
	// Blank block reproduces the empty line between original and next block.
	if !strings.Contains(out, "rg\n\n") {
		t.Errorf("blank spacing not reproduced:\n%q", out)
	}
}

func TestRenderDimApplied(t *testing.T) {
	blocks := []Block{{Raw: "hi", Plain: "hi", Kind: Prose}}
	out := Render(blocks, map[int]string{0: "嗨"}, func(s string) string { return "<" + s + ">" })
	if !strings.Contains(out, "<  ↳ 嗨>") {
		t.Errorf("dim func not applied to translation line:\n%s", out)
	}
	if strings.Contains(out, "<hi>") {
		t.Errorf("dim func wrongly applied to original line:\n%s", out)
	}
}

func TestRenderMultilineTranslation(t *testing.T) {
	blocks := []Block{{Raw: "orig", Plain: "orig", Kind: Prose}}
	out := Render(blocks, map[int]string{0: "line one\nline two"}, nil)
	if !strings.Contains(out, "  ↳ line one\n    line two\n") {
		t.Errorf("multiline translation not aligned:\n%q", out)
	}
}
