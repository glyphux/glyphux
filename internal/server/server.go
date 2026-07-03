// Package server assembles the daemon's HTTP surface: the API transport, the
// first-run wizard, and a root route that redirects to whichever is relevant.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/setup"
)

// Server is the daemon's HTTP listener.
type Server struct {
	http *http.Server
	log  *slog.Logger
	addr string

	listenerReady chan string // resolved address once listening
}

// Handler assembles the daemon's full route table: API transport, wizard,
// and the root redirect.
func Handler(apiServer *api.Server, wizard *setup.Wizard) http.Handler {
	mux := http.NewServeMux()
	apiServer.Routes(mux)
	wizard.Routes(mux)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !wizard.Complete() {
			http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
			return
		}
		http.Redirect(w, r, "/api/v0/content/ping", http.StatusTemporaryRedirect)
	})
	return mux
}

// New assembles the full route table.
func New(addr string, apiServer *api.Server, wizard *setup.Wizard, log *slog.Logger) *Server {
	return &Server{
		http: &http.Server{
			Addr:              addr,
			Handler:           requestLog(log, Handler(apiServer, wizard)),
			ReadHeaderTimeout: 5 * time.Second,
		},
		log:           log,
		addr:          addr,
		listenerReady: make(chan string, 1),
	}
}

// Run listens and serves until ctx is cancelled, then shuts down gracefully
// within shutdownTimeout.
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listenerReady <- ln.Addr().String()
	s.log.Info("glyphuxd listening", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.log.Info("shutting down", "timeout", shutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}

// Addr blocks until the listener is bound and returns its resolved address.
func (s *Server) Addr() string {
	return <-s.listenerReady
}

func requestLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}
