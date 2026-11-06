package cmd

import (
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/hostinfo"
)

func TestTranslateGemmaHint(t *testing.T) {
	for name, tc := range map[string]struct {
		host hostinfo.Host
		want []string
	}{
		"apple": {hostinfo.Host{RAM: 24 * hostinfo.GB, Unified: true},
			[]string{"24 GB unified memory", "ollama pull translategemma:12b", "8.1 GB"}},
		"nvidia": {hostinfo.Host{RAM: 64 * hostinfo.GB, VRAM: 24 * hostinfo.GB},
			[]string{"24 GB GPU memory", "ollama pull translategemma:27b", "17.0 GB"}},
		"cpu": {hostinfo.Host{RAM: 16 * hostinfo.GB},
			[]string{"16 GB RAM, no GPU detected", "ollama pull translategemma:4b", "3.3 GB"}},
	} {
		t.Run(name, func(t *testing.T) {
			hint := translateGemmaHint(tc.host)
			for _, want := range tc.want {
				if !strings.Contains(hint, want) {
					t.Errorf("hint %q is missing %q", hint, want)
				}
			}
		})
	}
	if hint := translateGemmaHint(hostinfo.Host{}); hint != "" {
		t.Errorf("unknown host should print nothing, got %q", hint)
	}
}
