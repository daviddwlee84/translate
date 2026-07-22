package cmd

import (
	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/mcpserver"
)

func newMcpCmd() *cobra.Command {
	var noHistory bool
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP server over stdio (for Claude Desktop/Code, Cursor, …)",
		Long: "mcp runs a Model Context Protocol server over stdin/stdout, exposing the\n" +
			"translate, define, and history tools to any MCP host that spawns this binary.\n" +
			"There is no port or token — the transport is a private stdio pipe.\n\n" +
			"Register it in an MCP host with:\n" +
			"  { \"command\": \"translate\", \"args\": [\"mcp\"] }\n\n" +
			"stdout carries JSON-RPC; do not pipe it anywhere but the host.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := config.Load()
			if err != nil {
				return err
			}
			svc, err := appcore.NewService(cfg, appcore.Options{NoHistory: noHistory})
			if err != nil {
				return err
			}
			defer svc.Close()
			return mcpserver.Serve(cmd.Context(), svc, shortVersion())
		},
	}
	c.Flags().BoolVar(&noHistory, "no-history", false, "do not read or write history")
	return c
}
