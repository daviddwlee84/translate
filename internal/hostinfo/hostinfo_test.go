package hostinfo

import (
	"errors"
	"testing"
)

func TestRecommendTranslateGemma(t *testing.T) {
	for name, tc := range map[string]struct {
		host Host
		want string
	}{
		"apple 8":          {Host{RAM: 8 * GB, Unified: true}, "translategemma:4b"},
		"apple 16":         {Host{RAM: 16 * GB, Unified: true}, "translategemma:12b"},
		"apple 24":         {Host{RAM: 24 * GB, Unified: true}, "translategemma:12b"},
		"apple 32":         {Host{RAM: 32 * GB, Unified: true}, "translategemma:27b"},
		"nvidia 8":         {Host{RAM: 64 * GB, VRAM: 8 * GB}, "translategemma:4b"},
		"nvidia 10":        {Host{RAM: 16 * GB, VRAM: 10 * GB}, "translategemma:12b"},
		"nvidia 24":        {Host{RAM: 16 * GB, VRAM: 24 * GB}, "translategemma:27b"},
		"nvidia, ram lost": {Host{VRAM: 12 * GB}, "translategemma:12b"},
		"cpu only":         {Host{RAM: 128 * GB}, "translategemma:4b"},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := RecommendTranslateGemma(tc.host)
			if !ok || got.Tag != tc.want {
				t.Errorf("got %+v ok=%v, want %s", got, ok, tc.want)
			}
		})
	}
	if _, ok := RecommendTranslateGemma(Host{}); ok {
		t.Error("unknown host should give no recommendation")
	}
}

func TestParseNvidiaSMI(t *testing.T) {
	if got := parseNvidiaSMI("8192\n24576\n\nN/A\n"); got != 24576<<20 {
		t.Errorf("got %d", got)
	}
	if got := parseNvidiaSMI(""); got != 0 {
		t.Errorf("empty output: got %d", got)
	}
}

func TestDetectReadsMemory(t *testing.T) {
	ram, err := totalMemory()
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip("memory detection is not implemented on this platform")
	}
	if err != nil || ram == 0 {
		t.Errorf("totalMemory()=%d, %v", ram, err)
	}
}
