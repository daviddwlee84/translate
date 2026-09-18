package bitext

import (
	"regexp"
	"strings"
)

// documentBlockMarker recognizes unambiguous Markdown block openings, not
// inline punctuation such as snake_case or a word followed by a final newline.
var documentBlockMarker = regexp.MustCompile("^(#{1,6}([ \\t]|$)|>|[-+*][ \\t]+|[0-9]{1,9}[.)][ \\t]+|`{3,}|~{3,})")

// IsDocument reports whether ANSI-stripped input has multiple content lines or
// an explicit Markdown block marker. It is a conservative routing hint, not a
// Markdown parser. A trailing newline from `echo word` does not make a document.
func IsDocument(text string) bool {
	content := strings.TrimSpace(text)
	if content == "" {
		return false
	}
	if strings.ContainsAny(content, "\r\n") || documentBlockMarker.MatchString(content) {
		return true
	}
	// Inspect indentation before trimming: a standalone indented code line is
	// meaningful Markdown even when it contains only one word.
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			return true
		}
		break
	}
	// Markdown thematic breaks allow whitespace between three or more identical
	// markers. Requiring one repeated marker avoids treating ordinary punctuation
	// or a negative number as a document.
	markers := strings.NewReplacer(" ", "", "\t", "").Replace(content)
	return len(markers) >= 3 && (strings.Trim(markers, "-") == "" ||
		strings.Trim(markers, "*") == "" || strings.Trim(markers, "_") == "")
}
