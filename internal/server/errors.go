// Package server exposes the translate engine over a loopback HTTP API
// (`translate serve`): JSON endpoints, an SSE streaming endpoint, and an embedded
// OpenAPI/Swagger surface. It is a thin transport adapter over appcore.Service —
// all translation logic lives there.
package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/daviddwlee84/translate/internal/engine"
)

// errorEnvelope is the uniform error body: {"error":{"code","message"}}.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON encodes v as an indented JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// writeError writes an error envelope with the given status/code/message.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: msg}})
}

// decodeJSON decodes the request body into v, writing a 400 envelope and
// returning false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// writeEngineError maps a translation error to a status + envelope: empty input is
// a 422, everything else a 500.
func writeEngineError(w http.ResponseWriter, err error) {
	if errors.Is(err, engine.ErrEmptyInput) {
		writeError(w, http.StatusUnprocessableEntity, "empty_input", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "engine_error", err.Error())
}
