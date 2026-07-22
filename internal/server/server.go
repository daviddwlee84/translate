package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
)

// Service is the subset of appcore.Service the HTTP handlers depend on.
// *appcore.Service satisfies it; tests inject a fake without touching the network.
type Service interface {
	Translate(ctx context.Context, p appcore.Params) (*engine.TranslateResult, error)
	TranslateStream(ctx context.Context, p appcore.Params, onToken func(string)) (*engine.TranslateResult, error)
	Define(ctx context.Context, word string) (*engine.TranslateResult, error)
	HistoryRecent(ctx context.Context, limit int) ([]store.Record, error)
	HistorySearch(ctx context.Context, query string, limit int) ([]store.Record, error)
}

// Options configures the HTTP server.
type Options struct {
	Bind  string // bind address (loopback enforced unless Token is set)
	Port  int
	Token string // bearer token guarding /v1/history; "" => no auth
}

// Server is the translate HTTP API server (loopback-only by default).
type Server struct {
	httpSrv *http.Server
	addr    string
}

// New builds the server, enforcing the loopback-or-token rule and wiring routes.
func New(svc Service, opt Options) (*Server, error) {
	if err := checkBind(opt.Bind, opt.Token); err != nil {
		return nil, err
	}
	h := &handlers{svc: svc, token: opt.Token}
	addr := net.JoinHostPort(opt.Bind, strconv.Itoa(opt.Port))
	return &Server{
		httpSrv: &http.Server{
			Addr:              addr,
			Handler:           newMux(h),
			ReadHeaderTimeout: 10 * time.Second,
		},
		addr: addr,
	}, nil
}

// newMux registers every route. Kept separate so tests can exercise the router
// directly and later phases (SSE, Swagger) can add routes in one place.
func newMux(h *handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/translate", h.translate)
	mux.HandleFunc("POST /v1/define", h.define)
	mux.HandleFunc("GET /v1/history", h.requireToken(h.history))
	mux.HandleFunc("GET /healthz", h.healthz)
	return mux
}

// Addr is the resolved listen address (host:port).
func (s *Server) Addr() string { return s.addr }

// Run serves until ctx is cancelled, then gracefully shuts down (5s drain).
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "translate: HTTP API on http://%s (try GET /healthz)\n", s.addr)
	errCh := make(chan error, 1)
	go func() { errCh <- s.httpSrv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpSrv.Shutdown(shutCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// checkBind enforces the loopback-or-token rule: a non-loopback bind is refused
// unless a token is configured, since history is personal data.
func checkBind(bind, token string) error {
	if isLoopback(bind) || token != "" {
		return nil
	}
	return fmt.Errorf("refusing to bind non-loopback address %q without a token (history is personal data); bind 127.0.0.1 or set [server].token / token_env", bind)
}

func isLoopback(bind string) bool {
	if bind == "localhost" {
		return true
	}
	ip := net.ParseIP(bind)
	return ip != nil && ip.IsLoopback()
}
