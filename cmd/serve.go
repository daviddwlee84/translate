package cmd

import (
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/config"
	"github.com/daviddwlee84/translate/internal/server"
)

func newServeCmd() *cobra.Command {
	var (
		port      int
		bind      string
		token     string
		noHistory bool
	)
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run the local HTTP API server (loopback; JSON + SSE + OpenAPI)",
		Long: "serve starts a persistent loopback HTTP service exposing the translate\n" +
			"engine over JSON (and SSE streaming). It binds 127.0.0.1 by default; a\n" +
			"non-loopback bind is refused unless a token is set. History is guarded by\n" +
			"the token when one is configured.\n\n" +
			"  translate serve                 # http://127.0.0.1:4155\n" +
			"  translate serve --port 8080\n" +
			"  translate serve --token \"$TOK\" --bind 0.0.0.0",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := config.Load()
			if err != nil {
				return err
			}
			sc := cfg.ResolveServer(config.ServerOverrides{Port: port, Bind: bind, Token: token})
			svc, err := appcore.NewService(cfg, appcore.Options{NoHistory: noHistory})
			if err != nil {
				return err
			}
			defer svc.Close()
			srv, err := server.New(svc, server.Options{Bind: sc.Bind, Port: sc.Port, Token: sc.Token})
			if err != nil {
				return err
			}
			// Add SIGTERM to the inherited Ctrl-C context so a daemon stops cleanly.
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM)
			defer stop()
			return srv.Run(ctx)
		},
	}
	c.Flags().IntVar(&port, "port", 0, "listen port (default from [server].port, else 4155)")
	c.Flags().StringVar(&bind, "bind", "", "bind address (default 127.0.0.1; loopback enforced without a token)")
	c.Flags().StringVar(&token, "token", "", "bearer token guarding /v1/history (overrides config)")
	c.Flags().BoolVar(&noHistory, "no-history", false, "do not read or write history")
	return c
}
