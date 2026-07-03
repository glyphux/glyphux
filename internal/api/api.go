// Package api is the HTTP/JSON transport over the domain APIs (§5.1,
// CLIENTS/EXTENSION boundary). It serves the resolved composition and the
// domain-API reads; it holds no state and no privileged kernel access —
// it is a client of the contract like every other surface.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
)

// Server exposes the Phase-0 API surface.
type Server struct {
	compositions *composition.Store
	content      *content.API
	log          *slog.Logger
}

// New builds the API transport over the given domain APIs.
func New(comps *composition.Store, contentAPI *content.API, log *slog.Logger) *Server {
	return &Server{compositions: comps, content: contentAPI, log: log}
}

// Routes registers the API endpoints on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/v0/composition", s.handleComposition)
	mux.HandleFunc("GET /api/v0/content/ping", s.handlePing)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleComposition serves the resolved composition document — the contract
// as clients see it.
func (s *Server) handleComposition(w http.ResponseWriter, r *http.Request) {
	comp, err := s.compositions.Load(r.Context())
	if errors.Is(err, composition.ErrNotFound) {
		s.writeError(w, http.StatusConflict, "setup not completed; visit /setup")
		return
	}
	if err != nil {
		s.log.Error("load composition", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, comp)
}

// handlePing serves the slice-0.4 contract-driven domain-API read.
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	ping, err := s.content.GetPing(r.Context())
	if err != nil {
		if errors.Is(err, composition.ErrNotFound) {
			s.writeError(w, http.StatusConflict, "setup not completed; visit /setup")
			return
		}
		s.log.Error("content ping", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, ping)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("encode response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}
