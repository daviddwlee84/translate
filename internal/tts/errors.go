package tts

import "errors"

// Sentinel errors returned by the TTS layer.
var (
	// ErrNoBackend is returned when no configured speech backend is available
	// (no native TTS binary and no audio player for the online fallback).
	ErrNoBackend = errors.New("tts: no speech backend available")
	// ErrNoPlayer is returned when the online backend produced audio but no
	// audio player could be found to play it.
	ErrNoPlayer = errors.New("tts: no audio player found")
	// ErrEmptyText is returned when there is nothing to speak.
	ErrEmptyText = errors.New("tts: empty text")
)
