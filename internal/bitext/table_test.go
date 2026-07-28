package bitext

import "testing"

func TestIsTabular(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "the reported failure: pipe table with box rules",
			in: "────────────────────────\n" +
				"| Model | V1 | V2 |\n" +
				"────────────────────────\n" +
				"| dense-95 | 0.1210 | 0.0679 |\n" +
				"| dense-250 | 0.1208 | 0.0826 |",
			want: true,
		},
		{
			name: "well-formed markdown table",
			in:   "| a | b |\n| --- | --- |\n| 1 | 2 |",
			want: true,
		},
		{
			name: "column-aligned output",
			in:   "NAME      SIZE   OWNER\nfoo       12     alice\nbar       34     bob",
			want: true,
		},
		{
			name: "two-column aligned output",
			in:   "--json    emit JSON\n--to      target language\n--from    source language",
			want: true,
		},
		{
			name: "ordinary prose is not a table",
			in:   "Hello world.\nThis is a second sentence.\nAnd a third one here.",
			want: false,
		},
		{
			name: "a single pipe line is not a table",
			in:   "run `foo | bar` to pipe it",
			want: false,
		},
		{
			name: "prose mentioning a pipe twice on one line stays prose",
			in:   "use a | b | c syntax",
			want: false,
		},
		{
			name: "indented prose is not columns",
			in:   "  first line here\n  second line here\n  third line here",
			want: false,
		},
		{
			name: "empty",
			in:   "",
			want: false,
		},
		{
			name: "single line",
			in:   "| a | b | c |",
			want: false,
		},
		{
			name: "two aligned lines are a coincidence, not a table",
			in:   "NAME      SIZE\nfoo       12",
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTabular(tc.in); got != tc.want {
				t.Errorf("IsTabular() = %v, want %v\ninput:\n%s", got, tc.want, tc.in)
			}
		})
	}
}
