package config

import (
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestDefaultTTS(t *testing.T) {
	tts := Default().TTS
	if !tts.Enabled {
		t.Error("TTS should be enabled by default")
	}
	if tts.AutoSpeak {
		t.Error("AutoSpeak should default off (opt-in via --speak / ^s)")
	}
	if !tts.PreferForeign {
		t.Error("PreferForeign should default on")
	}
	if len(tts.Order) != 2 || tts.Order[0] != "native" || tts.Order[1] != "google" {
		t.Errorf("Order = %v, want [native google]", tts.Order)
	}
	if tts.TimeoutMs != 5000 {
		t.Errorf("TimeoutMs = %d, want 5000", tts.TimeoutMs)
	}
	if tts.GoogleTTSURL == "" {
		t.Error("GoogleTTSURL should be seeded")
	}
}

// TestTTSPartialUnmarshal mirrors config.Load: unmarshalling a partial [tts]
// table onto Default() overrides only the named keys and keeps the rest seeded.
func TestTTSPartialUnmarshal(t *testing.T) {
	cfg := Default()
	src := "[tts]\nenabled = false\nforeign = \"en\"\n"
	if err := toml.Unmarshal([]byte(src), cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.TTS.Enabled {
		t.Error("enabled should be overridden to false")
	}
	if cfg.TTS.Foreign != "en" {
		t.Errorf("foreign = %q, want en", cfg.TTS.Foreign)
	}
	// Order was not mentioned in the TOML, so it must keep the default.
	if len(cfg.TTS.Order) != 2 {
		t.Errorf("Order should stay seeded, got %v", cfg.TTS.Order)
	}
}
