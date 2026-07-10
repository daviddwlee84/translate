package cmd

import (
	"runtime/debug"
	"strings"
)

// buildVersion reports a version string derived from the Go build info, so
// `translate --version` distinguishes a freshly-built dev binary from a stale
// installed one. For `go install <path>@<ver>` it shows the module version; for
// a local `go build`/`go install .` off a working tree it shows the VCS revision,
// a "+dirty" marker when the tree had uncommitted changes, and the build time.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown; no build info)"
	}
	ver := info.Main.Version // e.g. "v0.1.0" or "(devel)"
	var rev, when string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	var b strings.Builder
	b.WriteString(ver)
	if rev != "" {
		if len(rev) > 12 {
			rev = rev[:12]
		}
		b.WriteString(" (" + rev)
		if dirty {
			b.WriteString("+dirty")
		}
		b.WriteString(")")
	}
	if when != "" {
		b.WriteString(" commit " + when)
	}
	return b.String()
}
