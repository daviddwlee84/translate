package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
)

// TestOpenAPISchemaMatchesStruct guards against schema drift: every serialized
// (non-"-") field of the core response structs must be documented as a property in
// openapi.json. Transient fields (json:"-") are intentionally skipped.
func TestOpenAPISchemaMatchesStruct(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}

	cases := []struct {
		schema string
		typ    reflect.Type
	}{
		{"TranslateResult", reflect.TypeOf(engine.TranslateResult{})},
		{"DictEntry", reflect.TypeOf(engine.DictEntry{})},
		{"Record", reflect.TypeOf(store.Record{})},
	}
	for _, c := range cases {
		props := doc.Components.Schemas[c.schema].Properties
		if props == nil {
			t.Errorf("openapi.json: schema %q missing or has no properties", c.schema)
			continue
		}
		for i := 0; i < c.typ.NumField(); i++ {
			name := strings.Split(c.typ.Field(i).Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue // unexported or transient (json:"-")
			}
			if _, ok := props[name]; !ok {
				t.Errorf("%s: field %q is serialized but not documented in openapi.json", c.schema, name)
			}
		}
	}
}

func TestServeOpenAPIJSON(t *testing.T) {
	rec := do(t, &handlers{svc: &fakeService{}}, "GET", "/openapi.json", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("served /openapi.json is not valid JSON: %v", err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi version = %v, want 3.1.0", doc["openapi"])
	}
}

func TestServeSwaggerUI(t *testing.T) {
	// Bare /docs redirects to /docs/.
	rec := do(t, &handlers{svc: &fakeService{}}, "GET", "/docs", "", nil)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("/docs status = %d, want 301", rec.Code)
	}

	// /docs/ serves the Swagger UI index.
	rec = do(t, &handlers{svc: &fakeService{}}, "GET", "/docs/", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/docs/ status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "swagger-ui") {
		t.Fatalf("/docs/ body does not look like Swagger UI:\n%s", rec.Body.String()[:min(200, rec.Body.Len())])
	}

	// The vendored assets are served.
	rec = do(t, &handlers{svc: &fakeService{}}, "GET", "/docs/swagger-ui.css", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("swagger-ui.css status = %d, want 200", rec.Code)
	}
}
