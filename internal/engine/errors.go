package engine

import "errors"

// Sentinel errors returned across the engine layer.
var (
	// ErrEmptyInput is returned when a request has no text to translate.
	ErrEmptyInput = errors.New("engine: empty input")
	// ErrNoResult is returned when a channel closed without a terminal result.
	ErrNoResult = errors.New("engine: stream closed without a result")
	// ErrNoEngineForMode is returned by the chain when no engine supports the mode.
	ErrNoEngineForMode = errors.New("engine: no engine supports this mode")
	// ErrAllEnginesFailed is returned by the chain when every candidate failed.
	ErrAllEnginesFailed = errors.New("engine: all engines failed")
	// ErrNoDictEntry is returned when a dictionary lookup finds nothing.
	ErrNoDictEntry = errors.New("engine: no dictionary entry")
)
