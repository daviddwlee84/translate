package bitext

import "testing"

func TestIsDocument(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"empty", "", false},
		{"whitespace", " \r\n\t\n", false},
		{"word", "test", false},
		{"echo word", "test\n", false},
		{"outer blank lines", "\n\n test \n\n", false},
		{"one CRLF line", "test\r\n", false},
		{"sentence", "This is a sentence.", false},
		{"multiple lines", "First line\nSecond line\n", true},
		{"paragraphs", "First paragraph.\n\nSecond paragraph.", true},
		{"CRLF lines", "First\r\nSecond\r\n", true},
		{"heading", "# Heading\n", true},
		{"deep heading", "   ###### Heading", true},
		{"hashtag", "#hashtag", false},
		{"too many heading markers", "####### Heading", false},
		{"bullet", "- List item", true},
		{"task item", "- [ ] Todo", true},
		{"star bullet", "* List item", true},
		{"numbered item", "1. First item", true},
		{"parenthesized marker", "2) Second item", true},
		{"negative number", "-5", false},
		{"decimal", "1.23", false},
		{"blockquote", ">Quoted text", true},
		{"backtick fence", "```go", true},
		{"tilde fence", "~~~sh", true},
		{"indented code", "    command\n", true},
		{"tab code after blank", "\n\tcommand\n", true},
		{"light indentation", "  word\n", false},
		{"thematic break", "---", true},
		{"spaced thematic break", "_ _ _", true},
		{"mixed punctuation", "-_*", false},
		{"snake case", "snake_case", false},
		{"inline code alone", "`command`", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDocument(tt.text); got != tt.want {
				t.Errorf("IsDocument(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
