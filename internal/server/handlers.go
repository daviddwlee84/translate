package server

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/store"
)

// handlers holds the shared service and the (optional) bearer token.
type handlers struct {
	svc   Service
	token string
}

// translateRequest is the POST /v1/translate body. Only text is required; the rest
// fall back to the server's resolved defaults.
type translateRequest struct {
	Text         string `json:"text"`
	Source       string `json:"source,omitempty"`
	Target       string `json:"target,omitempty"`
	Preset       string `json:"preset,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Model        string `json:"model,omitempty"`
	MaxAlts      int    `json:"max_alts,omitempty"`
	Pair         *bool  `json:"pair,omitempty"`
}

func (r translateRequest) params() appcore.Params {
	return appcore.Params{
		Text:         r.Text,
		Source:       r.Source,
		Target:       r.Target,
		Preset:       r.Preset,
		Instructions: r.Instructions,
		Model:        r.Model,
		MaxAlts:      r.MaxAlts,
		Pair:         r.Pair,
	}
}

func (h *handlers) translate(w http.ResponseWriter, r *http.Request) {
	var req translateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusUnprocessableEntity, "empty_input", `field "text" is required`)
		return
	}
	res, err := h.svc.Translate(r.Context(), req.params())
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// defineRequest is the POST /v1/define body.
type defineRequest struct {
	Word string `json:"word"`
}

func (h *handlers) define(w http.ResponseWriter, r *http.Request) {
	var req defineRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Word) == "" {
		writeError(w, http.StatusUnprocessableEntity, "empty_input", `field "word" is required`)
		return
	}
	res, err := h.svc.Define(r.Context(), req.Word)
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// history serves GET /v1/history?q=&limit=. With q it fuzzy-searches, else it
// returns the most recent records. Token-guarded (personal data).
func (h *handlers) history(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseLimit(r.URL.Query().Get("limit"), 20)
	var (
		recs []store.Record
		err  error
	)
	if q != "" {
		recs, err = h.svc.HistorySearch(r.Context(), q, limit)
	} else {
		recs, err = h.svc.HistoryRecent(r.Context(), limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history_error", err.Error())
		return
	}
	if recs == nil {
		recs = []store.Record{} // encode [] not null when history is empty/disabled
	}
	writeJSON(w, http.StatusOK, recs)
}

func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// requireToken wraps a handler with bearer-token auth when a token is configured.
// With no token, it is a pass-through (loopback is the only guard).
func (h *handlers) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			got := bearerToken(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
		}
		next(w, r)
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) >= len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

// parseLimit parses a positive integer limit, falling back to def.
func parseLimit(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
