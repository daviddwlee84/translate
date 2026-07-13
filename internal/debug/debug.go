// Package debug provides an opt-in diagnostic log. When enabled it writes
// timestamped trace lines describing the intermediate decisions (config
// resolution, pair-mode routing, word-vs-phrase classification, dictionary
// hit/miss, chain fallback) so an unexpected translation can be diagnosed.
//
// It is a cheap no-op until Enable is called. The one-shot CLI enables it to
// stderr; the TUI enables it to a log file, because its alt-screen hides stderr.
package debug

import (
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	mu  sync.Mutex
	out io.Writer // nil => disabled
)

// Enable turns debug logging on, directing trace lines to w. Passing nil turns
// it back off. Safe to call once at startup.
func Enable(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	out = w
}

// Enabled reports whether debug logging is currently on.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return out != nil
}

// Logf writes one trace line when enabled; otherwise it does nothing.
func Logf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if out == nil {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(out, "%s translate[debug] "+format+"\n", append([]any{ts}, args...)...)
}
