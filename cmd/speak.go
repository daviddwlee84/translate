package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/lang"
	"github.com/daviddwlee84/translate/internal/tts"
)

// speakOptions maps the [tts] config into tts.Options.
func speakOptions(cfg *config.Config) tts.Options {
	t := cfg.TTS
	return tts.Options{
		Order:     t.Order,
		Rate:      t.Rate,
		Voices:    t.Voices,
		GoogleURL: t.GoogleTTSURL,
		UserAgent: t.UserAgent,
		Timeout:   time.Duration(t.TimeoutMs) * time.Millisecond,
		CacheDir:  t.CacheDir,
		Player:    t.Player,
	}
}

// newSpeaker builds a Speaker from config.
func newSpeaker(cfg *config.Config) tts.Speaker { return tts.New(speakOptions(cfg)) }

// tuiSpeaker returns a Speaker for the TUI, or nil when TTS is disabled (so the
// TUI can show a "tts disabled" hint instead of silently doing nothing).
func tuiSpeaker(cfg *config.Config) tts.Speaker {
	if !cfg.TTS.Enabled {
		return nil
	}
	return newSpeaker(cfg)
}

// speakForeign resolves the preferred "副"/foreign language: the explicit config
// value, else the pair-mode "away" language (empty => Select derives it).
func speakForeign(cfg *config.Config, pairWith string) string {
	if cfg.TTS.Foreign != "" {
		return cfg.TTS.Foreign
	}
	return pairWith
}

// shouldSpeak reports whether the one-shot CLI should speak this result.
func shouldSpeak(cfg *config.Config) bool {
	return cfg.TTS.Enabled && (flagSpeak || cfg.TTS.AutoSpeak)
}

// speakResult speaks the foreign side of a one-shot translation result. Failures
// are reported to stderr and never abort the command.
func speakResult(ctx context.Context, cfg *config.Config, res *engine.TranslateResult, srcText, srcLang, resLang, foreign string) {
	ch, ok := tts.Select(tts.SelectInput{
		SourceText: srcText,
		SourceLang: srcLang,
		ResultText: res.Translation,
		ResultLang: resLang,
		Foreign:    foreign,
	})
	if !ok {
		return
	}
	applySpeakLangOverride(&ch)
	if err := newSpeaker(cfg).Speak(ctx, ch.Text, ch.Lang); err != nil {
		fmt.Fprintf(os.Stderr, "translate: speech unavailable (%v)\n", err)
	}
}

// speakDefine speaks the looked-up head word itself (a `define --speak`
// pronounces the entry regardless of the foreign preference).
func speakDefine(ctx context.Context, cfg *config.Config, res *engine.TranslateResult) {
	word := ""
	if res.Dictionary != nil {
		word = res.Dictionary.Word
	}
	if strings.TrimSpace(word) == "" {
		word = res.Translation
	}
	if strings.TrimSpace(word) == "" {
		return
	}
	code := ""
	if flagSpeakLang != "" {
		if lm, _ := lang.Resolve(flagSpeakLang); lm.Code != "" {
			code = lm.Code
		}
	}
	if code == "" {
		code = lang.Detect(word)
	}
	if err := newSpeaker(cfg).Speak(ctx, word, code); err != nil {
		fmt.Fprintf(os.Stderr, "translate: speech unavailable (%v)\n", err)
	}
}

// applySpeakLangOverride forces the spoken language from --speak-lang when set.
func applySpeakLangOverride(ch *tts.Choice) {
	if flagSpeakLang == "" {
		return
	}
	if lm, _ := lang.Resolve(flagSpeakLang); lm.Code != "" {
		ch.Lang = lm.Code
	}
}

// newSpeakCmd is the standalone `translate speak <text...>` subcommand.
func newSpeakCmd() *cobra.Command {
	var langFlag, backend, voice string
	c := &cobra.Command{
		Use:   "speak <text...>",
		Short: "Speak text aloud (free TTS: native offline, then Google)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.Load()
			if err != nil {
				return err
			}
			text := strings.Join(args, " ")

			code := langFlag
			if code == "" {
				code = flagSpeakLang
			}
			if code != "" {
				if lm, _ := lang.Resolve(code); lm.Code != "" {
					code = lm.Code
				}
			} else {
				code = lang.Detect(text)
			}

			opts := speakOptions(cfg)
			if backend != "" {
				opts.Order = []string{backend}
			}
			if voice != "" {
				if opts.Voices == nil {
					opts.Voices = map[string]string{}
				}
				opts.Voices[strings.ToLower(code)] = voice
			}
			if err := tts.New(opts).Speak(cmd.Context(), text, code); err != nil {
				return fmt.Errorf("speak: %w", err)
			}
			return nil
		},
	}
	c.Flags().StringVar(&langFlag, "lang", "", "language of the text (e.g. en, zh-TW); default: detect")
	c.Flags().StringVar(&backend, "backend", "", "force a backend: native|google")
	c.Flags().StringVar(&voice, "voice", "", "native voice override for this language")
	return c
}
