// Package server assembles the daemon's HTTP surface: the API transport, the
// first-run wizard, and a root route that redirects to whichever is relevant.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/glyphux/glyphux/internal/adminui"
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

// maxRequestBodyBytes bounds any single request body — a DoS defense so an
// oversized payload cannot exhaust memory before a handler ever runs it
// through JSON decoding or validation (slice 1.9).
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// Production HTTP timeout defaults. Without these a slow or hanging client
// can hold a connection open indefinitely, exhausting file descriptors —
// ReadHeaderTimeout alone (the only one previously set) only bounds the
// header phase, not a slow body or a slow handler.
const (
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 30 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultReadHeaderTimeout = 5 * time.Second
)

// Timeouts reports the HTTP timeouts a Server was configured with.
type Timeouts struct {
	Read       time.Duration
	Write      time.Duration
	Idle       time.Duration
	ReadHeader time.Duration
}

// Handler assembles the daemon's full route table: API transport, wizard,
// and the root redirect. graphqlHandler is optional (slice 1.12) — pass
// none to omit /graphql entirely, or exactly one to mount it at
// POST /graphql. It is variadic rather than a plain parameter so every
// existing caller (internal/bootstrap's tests included, which this package
// must not require changes to) keeps compiling unchanged.
func Handler(apiServer *api.Server, wizard *setup.Wizard, graphqlHandler ...http.Handler) http.Handler {
	mux := http.NewServeMux()
	apiServer.Routes(mux)
	wizard.Routes(mux)
	if len(graphqlHandler) > 0 && graphqlHandler[0] != nil {
		mux.Handle("POST /graphql", graphqlHandler[0])
	}

	// The admin shell (PRD §5.6 Surface 2) is mounted on this same
	// long-lived mux, so — same reasoning as the root redirect below — it
	// must be gated on first-run setup having completed. Without the gate
	// it would be reachable (serving a broken, data-less UI) before /setup
	// ever runs, since bootstrap only swaps which *database* this mux is
	// wired to, never which routes exist.
	mux.Handle("GET /admin/", requireSetupComplete(wizard, http.StripPrefix("/admin", adminui.Handler())))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !wizard.Complete() {
			http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
			return
		}
		http.Redirect(w, r, "/api/v0/content/ping", http.StatusTemporaryRedirect)
	})
	return limitBody(securityHeaders(mux))
}

// requireSetupComplete gates next behind first-run setup having completed,
// mirroring the root route's own redirect-to-/setup behavior (§6.2).
func requireSetupComplete(wizard *setup.Wizard, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !wizard.Complete() {
			http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody caps every request body at maxRequestBodyBytes. A handler that
// reads past the cap gets an *http.MaxBytesError it can map to 413. Media
// uploads are exempt here — they carry real file bytes and apply their own,
// larger cap directly in the handler (slice 1.6).
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v0/media") {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets baseline hardening headers on every response. There is
// no Access-Control-Allow-Origin here or anywhere else in the daemon, so
// browsers deny cross-origin reads by default — CORS is opt-in only, and
// nothing currently opts in (slice 1.9).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// New builds a Server that serves handler on addr. Callers assemble the
// route table themselves — via Handler(apiServer, wizard) for the ordinary
// case, or a bootstrap.Gateway when setup may still switch which database
// the daemon serves from (§6.2) — New itself is agnostic to which.
func New(addr string, handler http.Handler, log *slog.Logger) *Server {
	return &Server{
		http: &http.Server{
			Addr:              addr,
			Handler:           requestLog(log, handler),
			ReadHeaderTimeout: DefaultReadHeaderTimeout,
			ReadTimeout:       DefaultReadTimeout,
			WriteTimeout:      DefaultWriteTimeout,
			IdleTimeout:       DefaultIdleTimeout,
		},
		log:           log,
		addr:          addr,
		listenerReady: make(chan string, 1),
	}
}

// Timeouts reports the HTTP timeouts this Server was configured with.
func (s *Server) Timeouts() Timeouts {
	return Timeouts{
		Read:       s.http.ReadTimeout,
		Write:      s.http.WriteTimeout,
		Idle:       s.http.IdleTimeout,
		ReadHeader: s.http.ReadHeaderTimeout,
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
