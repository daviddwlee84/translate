# Local HTTP API server (`translate serve`) + OpenAPI/Swagger

**Status**: ✅ Shipped 2026-07-22 (`translate serve` + `translate mcp`)
**Effort**: L
**Related**: `TODO.md` Done · `internal/appcore/` · `internal/server/` · `internal/mcpserver/` · [raycast-extension.md](raycast-extension.md) · [../docs/raycast-extension.md](../docs/raycast-extension.md)

## Outcome (what shipped)

Built as thin transport adapters over a new shared core, `internal/appcore.Service`,
which lifts the engine builders + the warm engine + history glue out of package
`cmd` (so `cmd`, `server`, and `mcpserver` all drive one core, no import cycle):

- **`translate serve`** — stdlib `net/http` (Go 1.22 method+path routing, no
  framework). `POST /v1/translate`, `POST /v1/define`, `GET /v1/history[?q=&limit=]`,
  SSE `GET /v1/translate/stream`, `GET /healthz`. Responses reuse
  `engine.TranslateResult` / `store.Record` verbatim. **Security**: loopback-only
  bind (fail-closed unless a token is set); optional bearer token guards
  `/v1/history` (`crypto/subtle` compare); config via a new `[server]` table
  (`port` 4155, `bind`, `token`/`token_env`). Graceful shutdown (SIGINT/SIGTERM →
  `http.Server.Shutdown`). History append hardened with `flock` for concurrent
  server+CLI writes.
- **OpenAPI/Swagger** — hand-written `internal/server/openapi.json` (OpenAPI 3.1) at
  `/openapi.json`; vendored `swagger-ui-dist` (`go:embed`) at `/docs`, fully offline.
  A reflection test guards schema↔struct drift.
- **`translate mcp`** — MCP over **stdio** (no port/token) via the official Go SDK
  `github.com/modelcontextprotocol/go-sdk` v1.6.1, exposing `translate` / `define` /
  `history` tools (typed structured output inferred from the Go structs). For Claude
  Desktop/Code, Cursor, etc.: `{ "command": "translate", "args": ["mcp"] }`.

The streaming caveat below still holds: copilot-proxy buffers Claude `/v1/messages`,
so *visible* SSE/MCP token cadence stays coarse for Claude regardless of transport.

Original investigation & options are preserved below for context.

---

## Original context (2026-07, pre-build)

2026-07, surfaced while building the Raycast extension. Today every client (the
Raycast extension, script commands) **spawns the `translate` binary per call**. A
persistent local HTTP server (`translate serve`) with an OpenAPI/Swagger page was
proposed as "more usable." This doc records whether it is, and how.

A related idea — folding `just raycast-dev` into the binary as `translate raycast
dev` — is **rejected** here (option C): it couples the Go CLI to the Node/`ray`
toolchain and the extension's on-disk location. Dev-tooling orchestration stays in
the `Justfile`. The useful subcommand is `translate serve`, not a `ray develop` wrapper.

## Investigation

- **Latency win is smaller than it looks.** Per-call spawn cost = config load +
  sqlite dict open + engine init. But LLM latency (~3–4 s) dominates, so a warm
  server mainly speeds up the **local dict/define** path, *not* LLM translation.
- **Streaming is the real win.** An HTTP server controls its own flushing → SSE
  (`text/event-stream`); clients consume via `fetch`/EventSource. Cleaner than the
  TTY / `--stream` / spawn path. (Caveat: upstream copilot-proxy still buffers claude
  `/v1/messages`, so visible streaming stays limited there regardless of transport.)
- **Multi-client + docs.** One loopback service → Raycast, Alfred, a browser
  extension, editor plugins, `curl`, a web UI. OpenAPI/Swagger documents it for all.
- **Sidesteps launchd-PATH** for request-time — the extension hits
  `http://127.0.0.1:PORT` instead of resolving/spawning the binary. But the *first*
  "start the daemon" still needs a spawn/LaunchAgent (PATH problem just moves).
- **Cheap to build.** Reuses `internal/engine` (pure Go, context-aware, TUI-free) as
  a thin HTTP adapter. stdlib `net/http` suffices (no framework dep). OpenAPI can be a
  hand-written `/openapi.json` + `go:embed`-ded Swagger UI, or generated
  (`swaggo/swag`, `danielgtaylor/huma`). Endpoints: `POST /v1/translate`,
  `POST /v1/define`, `GET /v1/history[?q=]`, SSE `GET /v1/translate/stream`.

## Options considered

| Option | What | Verdict |
|---|---|---|
| A. Keep spawn (current) | extension spawns `translate --json` / `--stream` per call | simplest, works, no daemon; per-call startup cost is small vs LLM latency |
| B. `translate serve` (HTTP + SSE + OpenAPI) | persistent loopback service; extension opt-in via a "server URL" pref, spawn fallback | best for streaming + multi-client + no-PATH; adds daemon lifecycle + security surface |
| C. `translate raycast dev` subcommand | binary shells out to npm/`ray` | rejected — couples Go CLI to Node toolchain + extension location; keep in Justfile |

## Current blocker / open questions

- **Daemon lifecycle**: how to keep it running — menu-bar app, a `LaunchAgent`, or the
  extension auto-starting it (which reintroduces the PATH problem for the first spawn)?
- **Security**: loopback-only bind is mandatory (history is personal data); consider a
  token for `/v1/history`.
- **Concurrent history writes**: server + CLI both appending `history.jsonl` — the
  store must be concurrency-safe (append + lock).
- **Port**: fixed default (collision risk) vs dynamic (needs discovery). Lean to a
  config'd default (e.g. `4155`) + preference.
- **Is it worth it vs spawn?** Only if we want a local translation *service* (streaming
  UX, multiple clients, web UI). The raw-latency argument is weak (LLM-dominated).

## Decision (if any)

`2026-07 deferred` — spawn mode works and ships. Revisit if we want a streaming-first
UX, multiple clients, or a web UI. If built: start minimal (stdlib `net/http`,
loopback, SSE, hand-written OpenAPI + embedded Swagger), extension **opt-in** with a
spawn fallback so non-server users are unaffected.

## References

- stdlib `net/http`; `go:embed` for the Swagger UI assets
- OpenAPI generators: https://github.com/swaggo/swag · https://github.com/danielgtaylor/huma
- Raycast integration + the streaming caveat: [../docs/raycast-extension.md](../docs/raycast-extension.md)
