# `translate serve` (HTTP + OpenAPI/Swagger) + `translate mcp` (MCP over stdio)

## Context

Every client today (Raycast extension, script commands) **spawns the `translate`
binary per call** and parses `--json`/`--stream` stdout. `backlog/api-server.md`
(P?/L, deferred 2026-07) scoped a persistent loopback HTTP service to unlock
streaming-first UX, multi-client reuse (Raycast/Alfred/curl/web UI), and OpenAPI
docs. While building the Raycast extension the follow-on idea surfaced: *also expose
an MCP server* so LLM hosts (Claude Desktop/Code, Cursor) can translate through this
tool as a first-class tool call.

This plan delivers both as **thin adapters over the existing `internal/engine`
core**, which is already TUI-free, context-aware, concurrency-safe, and streaming-
native (`Engine.Translate(ctx, Request) (<-chan Chunk, error)`; `TranslateResult` is
already fully JSON-tagged). The one structural obstacle — the engine *builders* live
in package `cmd`, which a server package cannot import — is resolved once, up front,
by lifting them into a new shared `internal/appcore` package. After that, REST, SSE,
Swagger, and MCP are each small additive layers.

**Decisions (confirmed with the user):** MCP ships as a separate **`translate mcp`
stdio subcommand** (no port/token; the transport is a private stdio pipe the host
already trusts), built on the **official Go MCP SDK v1.6.1** (`github.com/modelcontextprotocol/go-sdk`,
stable v1.x, maintained with Google — it owns protocol-version negotiation and future
spec churn; the 3 tools map 1:1 to the shared service).

## Architecture — package layout & the layering fix

Import direction (arrows = "imports"):

```
internal/engine  internal/config  internal/store  internal/lang     (unchanged core)
                       ▲ ▲ ▲ ▲
              internal/appcore            NEW: engine builders + Service (warm engine + store)
                    ▲        ▲
        internal/server   internal/mcpserver   NEW transports (each imports only appcore)
                    ▲        ▲
                       cmd                  registers serve/mcp; keeps buildEngineSet (imports tui)
```

`internal/appcore` imports only `config`/`engine`/`store`/`lang`/`debug` — **never
`tui` or `cmd`** — so `cmd` and both server packages import it with no cycle.

New files:
- `internal/appcore/build.go` — the 8 builders lifted from `cmd/build.go`, exported.
- `internal/appcore/service.go` — the `Service` type (warm engines + store + glue).
- `internal/appcore/record.go` — `ToRecord` + `effectiveTarget` (pair routing).
- `internal/server/{server.go,handlers.go,sse.go,openapi.go,errors.go}` + `internal/server/swagger/` (embedded UI).
- `internal/mcpserver/{mcp.go,tools.go}`.
- `cmd/serve.go`, `cmd/mcp.go` — thin cobra wrappers.
- `_test.go` alongside each new `internal/` package (picked up by the existing `./internal/...` globs).

---

## Phase 0 — Refactor to `internal/appcore` + `Service` (no behavior change)

Ships alone; existing CLI stays byte-identical. Gated by `just check` + `just test`.

1. **Move builders** `cmd/build.go` → `internal/appcore/build.go`, exporting each (they
   already take only `config.Resolved`/`*config.Config`, so no signature changes):
   `buildEngine→BuildEngine`, `buildAutoChain→BuildAutoChain`, `llmFromProvider→LLMFromProvider`,
   `googleFromConfig→GoogleFromConfig`, `dictFromConfig→DictFromConfig`,
   `smartDictFromConfig→SmartDictFromConfig`, `smartAutoFromConfig→SmartAutoFromConfig`,
   `learnEngineFromConfig→LearnEngineFromConfig`. **Leave `buildEngineSet` in `cmd`**
   (it imports `internal/tui`) and rewrite its body to call the `appcore.*` builders.
2. **Move glue**: `toRecord` (`cmd/root.go:364`) → `appcore.ToRecord`; the body of
   `effectiveTarget` (`cmd/root.go:268`, calls `lang.PairTarget`) → unexported
   `appcore.effectiveTarget`. Add `appcore.DefineEngine(res, plain, smart bool) engine.Engine`
   capturing `defineEngine` (`cmd/define.go`) so CLI and Service agree on the exact define engine.
3. **Rewire call sites** (the only churn): `cmd/root.go` (`buildEngine`→`appcore.BuildEngine`
   at ~:191; `learnEngineFromConfig`/`llmFromProvider` refs; `recordAndRemember`→`appcore.ToRecord`),
   `cmd/build.go` `buildEngineSet`, and `cmd/define.go` `defineEngine` (becomes a one-liner).
4. Verify `just check` (`go vet . ./cmd/... ./internal/...` + build) stays green.

### `internal/appcore.Service` (built once, warm, shared across goroutines)

```go
type Service struct {
    cfg    *config.Config
    res    config.Resolved   // resolved once at startup, empty flag overrides
    trans  engine.Engine     // warm translate engine (BuildEngine)
    define engine.Engine     // warm define engine (DefineEngine)
    store  store.Store        // shared history; nil when disabled / --no-history
    home, away string; pair bool   // pair-mode routing, resolved once
}
type Options struct{ NoHistory bool }
func NewService(cfg *config.Config, opt Options) (*Service, error) // warms engines, opens store
func newService(trans, define engine.Engine, st store.Store, ...) *Service // unexported, for tests (no network)

type Params struct { Text, Source, Target, Preset, Instructions, Model string; MaxAlts int; Pair *bool }

func (s *Service) Translate(ctx, Params) (*engine.TranslateResult, error)
func (s *Service) TranslateStream(ctx, Params, onToken func(string)) (*engine.TranslateResult, error)
func (s *Service) Define(ctx, word string) (*engine.TranslateResult, error)
func (s *Service) HistoryRecent(ctx, limit int) ([]store.Record, error)
func (s *Service) HistorySearch(ctx, query string, limit int) ([]store.Record, error)
func (s *Service) Close() error
```

Internals reuse existing paths, not new logic: a `buildRequest(p)` helper mirrors
`oneShot` (`cmd/root.go:462`) + `effectiveTarget`; consume the channel with the
existing `engine.Drain(ch, onToken)` (`internal/engine/engine.go:225`); on success
**and `!res.Truncated`**, persist via `s.store.Add(ctx, appcore.ToRecord(...))`.
Do **not** touch `state.json`/`saveLastPair` — the server must never fight the CLI's
remembered pair. `Define` = `s.define.Translate(ctx, Request{Text: word, Mode: ModeDict})` + `Drain`.

---

## Phase 1 — `translate serve`: REST JSON + config + security + shutdown

Router: stdlib `net/http.ServeMux` with Go 1.22+ method+path patterns
(`mux.HandleFunc("POST /v1/translate", …)`) — no framework, matching the lean `go.mod`.

`cmd/serve.go` → `newServeCmd()`, registered in the `root.AddCommand(...)` call at
`cmd/root.go:95`. Flags override `[server]` config: `--port`, `--bind`, `--token`,
`--no-history`. `RunE`: `config.Load()` → resolve `[server]` (flags > config >
default) → `appcore.NewService` → `server.New(svc, sc)` → run with graceful shutdown.

Endpoints (response bodies reuse `engine.TranslateResult` / `store.Record` verbatim):

| Method + path | Request | Response |
|---|---|---|
| `POST /v1/translate` | `{text, source?, target?, preset?, instructions?, model?, max_alts?, pair?}` | `TranslateResult` |
| `POST /v1/define` | `{word}` | `TranslateResult` (dictionary/suggestions) |
| `GET /v1/history?q=&limit=` | — | `[]store.Record` (token-guarded) |
| `GET /healthz` | — | `{"status":"ok"}` |

Handlers: `json.Decode` → validate (empty text/word → 422) → `Service` → `json.Encode`.
Error envelope `{"error":{"code","message"}}` via `writeError(w,status,code,msg)`;
status map: 400 malformed JSON · 422 empty input (`engine.ErrEmptyInput`) · 401 bad
token · 404/405 routing · 500 engine error. Graceful shutdown: the serve command wires
its own `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`; on `ctx.Done()` →
`srv.Shutdown(5s)` then `svc.Close()`. Log the bound address on start.

### Config `[server]` table (`internal/config/config.go`)

```go
Server Server `toml:"server"`
type Server struct {
    Port     int    `toml:"port"`                 // default 4155
    Bind     string `toml:"bind"`                 // default "127.0.0.1"
    Token    string `toml:"token,omitempty"`      // optional inline bearer token
    TokenEnv string `toml:"token_env,omitempty"`  // env var holding the token (preferred)
}
```
Add to `Default()`; **bump `SchemaVersion` 1→2** (`config.go:43`) with a history comment.
No migration code needed — the additive auto-upgrade re-save at `cmd/root.go:146-158`
materializes it. Add a small `Config.ResolveServer(flags)` helper.

### Security
- **Loopback fail-closed**: default `Bind:"127.0.0.1"`; refuse to start on a non-loopback
  bind (`net.ParseIP(...).IsLoopback()` in `server.New`) unless a token is set.
- **Bearer token**: resolve `--token` > `[server].token` > `os.Getenv(token_env)`
  (recommend `token_env` so the secret stays out of `config.toml`); require
  `Authorization: Bearer <token>` on `/v1/history` (personal data), compared with
  `crypto/subtle.ConstantTimeCompare`. `/healthz`, `/openapi.json`, `/docs` stay open on loopback.

---

## Phase 2 — SSE `GET /v1/translate/stream`

GET + query params (browser `EventSource` is GET-only). `internal/server/sse.go`:
set `Content-Type: text/event-stream`, get `http.Flusher` (503 if absent), pass
`r.Context()` (already cancels the upstream LLM call end-to-end via the engine's
`case <-ctx.Done()` on client disconnect — no extra plumbing). `onToken` writes
`event: token` + `flusher.Flush()`; terminal `event: done` carries the final
`TranslateResult` JSON. Because `TranslateResult.Truncated` is `json:"-"`
(`engine.go:88`), emit it explicitly as an `event: warning` before `done`.

**Honor the caveat, don't over-promise**: copilot-proxy buffers Claude `/v1/messages`,
so *visible* token cadence stays coarse for Claude models regardless of SSE (upstream,
transport-independent). SSE's real win is dict/google/OpenAI-style providers and
multi-client fan-out. Document this on `/docs`.

---

## Phase 3 — OpenAPI + Swagger UI

**Hand-written spec + embedded UI** (matches the backlog's stated preference and the
zero-framework philosophy; a static OpenAPI 3.1 doc never drifts at runtime, unlike
codegen/framework alternatives):
- `internal/server/openapi.json` (OpenAPI 3.1), `go:embed`-ded, served at
  `GET /openapi.json`. A `TestOpenAPISchemaMatchesStruct` reflection test guards the
  top-level `components.schemas` field names against `TranslateResult`/`DictEntry`/`Record`.
- Vendor a pinned `swagger-ui-dist` (`swagger-ui.css`, `swagger-ui-bundle.js`, and an
  `index.html` calling `SwaggerUIBundle({url:'/openapi.json'})`) under
  `internal/server/swagger/`, `go:embed`-ded and served at `GET /docs` via
  `http.FileServerFS`. No CDN (offline/loopback-friendly). Adds ~1.5 MB to the binary —
  accepted; gate behind a `swaggerui` build tag only if binary size becomes an issue.

---

## Phase 4 — `translate mcp`: MCP over stdio (official Go SDK)

Depends only on Phase 0 (the `Service`), **not** on Phases 1–3 — can ship in parallel
with or before the HTTP work. It's the cheapest high-value deliverable.

Dependency: `go get github.com/modelcontextprotocol/go-sdk@v1.6.1` (Go 1.26.4 satisfies it).

`internal/mcpserver/mcp.go` — `Serve(ctx, svc *appcore.Service) error`:
```go
srv := mcp.NewServer(&mcp.Implementation{Name: "translate", Version: version}, nil)
mcp.AddTool(srv, &mcp.Tool{Name: "translate", Description: "…"}, translateHandler(svc))
mcp.AddTool(srv, &mcp.Tool{Name: "define",    Description: "…"}, defineHandler(svc))
mcp.AddTool(srv, &mcp.Tool{Name: "history",   Description: "…"}, historyHandler(svc))
return srv.Run(ctx, &mcp.StdioTransport{})   // stdin/stdout JSON-RPC
```
Handler signature (SDK derives the input JSON Schema from struct tags):
`func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)`.

| Tool | Input struct (json+jsonschema tags) | Returns |
|---|---|---|
| `translate` | `{Text string; Target, Source string}` | `CallToolResult` text = translation (+ alternatives/notes); `StructuredContent` = `TranslateResult` |
| `define` | `{Word string}` | text = formatted definition; structured = `TranslateResult` |
| `history` | `{Query string; Limit int}` | text = compact record list; structured = `[]store.Record` |

`cmd/mcp.go` → `newMcpCmd()` (registered at `cmd/root.go:95`): builds
`appcore.NewService(cfg, {})` then calls `mcpserver.Serve(cmd.Context(), svc)`.
**Critical**: stdout is the JSON-RPC channel — route all diagnostics/logs to stderr.

---

## Decided design points (no further input needed)
- **Per-request engine/provider switching is out of scope for v1.** The Service warms
  one engine; per-request overrides are limited to what `engine.Request` already honors
  (source/target/preset/instructions/`model`/max_alts). An engine cache keyed by
  `{engine,provider,model}` is a later add if needed.
- **History cross-process safety**: harden `jsonlStore.Add` (`internal/store/jsonl.go`)
  with an advisory `syscall.Flock(fd, LOCK_EX)` on the append fd (unix build tag; Windows
  no-op) so a running server and a concurrent CLI can't interleave >4 KB appends. Small,
  contained; do it in Phase 1.
- **Bilingual/learn** get no API/MCP surface in v1 (structured, non-streaming, CLI-shaped).
- **Daemon lifecycle** (LaunchAgent/menu-bar/auto-start) stays out of scope — `serve`
  only offers the process; keeping it running is a separate backlog item.
- **Raycast extension** stays spawn-based; a "server URL" preference with spawn fallback
  is a separate, opt-in follow-up so non-server users are unaffected.

## Risks / open questions
- `history.jsonl` `Recent`/`Search` re-read the whole file per call (`readAll`,
  `jsonl.go:61`) — fine at personal scale, O(file) per request under a server.
- Port `4155` is a fixed default (collision risk; no discovery in v1) — config'd, per the backlog.
- MCP SDK is a new external dependency; isolated in `internal/mcpserver` so it stays contained.

## Verification
- **Per phase**: `just check` (`fmt` + `vet` + `build`) and `just test`
  (`go test ./cmd/... ./internal/...`) — the new `internal/*` packages are already in scope.
- **Service** (`appcore/service_test.go`): table tests with a stub `engine.Engine`
  emitting canned `Chunk`s; assert pair-target routing (`lang.PairTarget`), history
  recording, and that `Truncated` results are **not** persisted. Real store round-trip
  via `store.OpenJSONL(t.TempDir()+"/h.jsonl")`.
- **HTTP** (`internal/server/*_test.go`): follow the existing `httptest` pattern in
  `internal/engine/llm_test.go`; `httptest.NewRecorder` + mux for JSON endpoints (assert
  status + decoded body; 401 without token, 200 with). SSE: a recorder implements
  `http.Flusher` — assert `event: token`×N + terminal `event: done`; one `httptest.NewServer`
  test reads the body line-by-line and cancels the client to assert ctx propagation.
- **MCP** (`internal/mcpserver/mcp_test.go`): use the SDK's in-memory transport (or an
  `exec` of `translate mcp`) to drive `initialize` → `tools/list` → `tools/call`; assert
  3 tools advertised and a stubbed translation returned.
- **End-to-end manual**:
  - `go run . serve --port 4155` then
    `curl -s localhost:4155/v1/translate -d '{"text":"hello","target":"zh-TW"}'`,
    `curl -N 'localhost:4155/v1/translate/stream?text=hello&target=zh-TW'`, open `localhost:4155/docs`.
  - `just build` then register `{"command":"…/translate","args":["mcp"]}` in an MCP host
    (Claude Desktop/Code) and confirm the `translate`/`define`/`history` tools resolve and run.

## Critical files
- `cmd/build.go` — builders to lift into `internal/appcore` (only `buildEngineSet` keeps `tui`).
- `cmd/root.go` — register `serve`/`mcp` at `:95`; rewire `buildEngine`/`effectiveTarget`(:268)/`toRecord`(:364); `oneShot`(:462) is the template for `Service.buildRequest`; schema auto-upgrade at :146-158.
- `internal/engine/engine.go` — `TranslateResult`(:72) as the JSON/response body; `Engine`/`Chunk`/`Drain`(:225) contract the Service consumes.
- `internal/config/config.go` — add `[server]` table, bump `SchemaVersion`(:43), extend `Default()`.
- `internal/store/{store.go,jsonl.go}` — shared `Store`; add flock to `jsonlStore.Add`.
- `cmd/define.go` — `defineEngine` → `appcore.DefineEngine`.

## References
- `backlog/api-server.md` — original scope, endpoints, open questions.
- Official Go MCP SDK (v1.6.1, stable): https://github.com/modelcontextprotocol/go-sdk · docs https://go.sdk.modelcontextprotocol.io/
- MCP transports (stdio vs streamable HTTP): https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
