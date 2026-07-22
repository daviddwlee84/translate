package tui

import "testing"

func TestCollapseBlankLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "collapses a run of blank lines to one",
			in:   "para one\n\n\n\npara two",
			want: "para one\n\npara two",
		},
		{
			name: "keeps a single blank line",
			in:   "para one\n\npara two",
			want: "para one\n\npara two",
		},
		{
			name: "treats whitespace-only lines as blank",
			in:   "para one\n\n   \n\t\npara two",
			want: "para one\n\npara two",
		},
		{
			name: "collapses full-width space (U+3000) blank lines",
			in:   "para one\n　\n　\npara two",
			want: "para one\n\npara two",
		},
		{
			name: "collapses NBSP blank lines",
			in:   "para one\n \n \npara two",
			want: "para one\n\npara two",
		},
		{
			name: "collapses CRLF blank lines and strips the CR",
			in:   "para one\r\n\r\n\r\npara two\r",
			want: "para one\n\npara two",
		},
		{
			name: "strips trailing whitespace on content lines",
			in:   "hello   \nworld\t",
			want: "hello\nworld",
		},
		{
			name: "no blank lines is unchanged",
			in:   "line one\nline two\nline three",
			want: "line one\nline two\nline three",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "adjacent paragraphs across many gaps",
			in:   "a\n\n\nb\n\n\n\n\nc",
			want: "a\n\nb\n\nc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := collapseBlankLines(tt.in); got != tt.want {
				t.Errorf("collapseBlankLines(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
