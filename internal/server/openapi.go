package server

import (
	"embed"
	"io/fs"
	"net/http"
)

// openapiJSON is the hand-written OpenAPI 3.1 document, served at /openapi.json.
// Keep components.schemas in sync with the Go structs — TestOpenAPISchemaMatchesStruct
// guards the top-level field names against drift.
//
//go:embed openapi.json
var openapiJSON []byte

// swaggerFS holds the vendored Swagger UI assets (offline; no CDN), served at /docs.
//
//go:embed swagger
var swaggerFS embed.FS

// registerDocs wires GET /openapi.json and the Swagger UI at /docs.
func registerDocs(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(openapiJSON)
	})

	sub, err := fs.Sub(swaggerFS, "swagger")
	if err != nil {
		panic(err) // embedded path is a compile-time constant; this cannot fail at runtime
	}
	fileServer := http.StripPrefix("/docs/", http.FileServerFS(sub))
	mux.Handle("GET /docs/", fileServer)
	// Redirect the bare /docs to /docs/ so relative asset URLs resolve.
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
	})
}
