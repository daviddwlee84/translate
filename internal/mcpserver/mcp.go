// Package mcpserver exposes the translate engine as an MCP (Model Context Protocol)
// server over stdio, so LLM hosts (Claude Desktop/Code, Cursor, …) can call
// translate/define/history as tools. It is a thin adapter over appcore.Service,
// built on the official Go MCP SDK.
//
// stdio is deliberate: the host spawns the binary and speaks newline-delimited
// JSON-RPC over stdin/stdout, so there is no port, no token, and history never
// crosses a network boundary. stdout is the JSON-RPC channel — callers must send
// any diagnostics to stderr.
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
)

// Service is the subset of appcore.Service the MCP tools depend on.
// *appcore.Service satisfies it; tests inject a fake.
type Service interface {
	Translate(ctx context.Context, p appcore.Params) (*engine.TranslateResult, error)
	Define(ctx context.Context, word string) (*engine.TranslateResult, error)
	HistoryRecent(ctx context.Context, limit int) ([]store.Record, error)
	HistorySearch(ctx context.Context, query string, limit int) ([]store.Record, error)
}

// newServer builds the MCP server with the translate/define/history tools.
func newServer(svc Service, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "translate", Version: version}, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "translate",
		Description: "Translate text into a target language (faithful, register-aware).",
	}, translateHandler(svc))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "define",
		Description: "Look up the dictionary definition of a word or short term.",
	}, defineHandler(svc))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "history",
		Description: "List or fuzzy-search recent translation history.",
	}, historyHandler(svc))
	return srv
}

// Serve runs the MCP server over stdio until the host closes the connection or ctx
// is cancelled.
func Serve(ctx context.Context, svc Service, version string) error {
	return newServer(svc, version).Run(ctx, &mcp.StdioTransport{})
}
