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

	// Content CRUD (slice 1.2). The literal /ping route above is more specific
	// than {type}, so ServeMux prefers it — no shadowing.
	mux.HandleFunc("POST /api/v0/content/{type}", s.handleContentCreate)
	mux.HandleFunc("GET /api/v0/content/{type}", s.handleContentList)
	mux.HandleFunc("GET /api/v0/content/{type}/{id}", s.handleContentGet)
	mux.HandleFunc("PUT /api/v0/content/{type}/{id}", s.handleContentUpdate)
	mux.HandleFunc("DELETE /api/v0/content/{type}/{id}", s.handleContentDelete)
}

func (s *Server) handleContentCreate(w http.ResponseWriter, r *http.Request) {
	data, ok := s.decodeData(w, r)
	if !ok {
		return
	}
	item, err := s.content.Create(r.Context(), r.PathValue("type"), data)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleContentList(w http.ResponseWriter, r *http.Request) {
	items, err := s.content.List(r.Context(), r.PathValue("type"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleContentGet(w http.ResponseWriter, r *http.Request) {
	item, err := s.content.Get(r.Context(), r.PathValue("type"), r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleContentUpdate(w http.ResponseWriter, r *http.Request) {
	data, ok := s.decodeData(w, r)
	if !ok {
		return
	}
	item, err := s.content.Update(r.Context(), r.PathValue("type"), r.PathValue("id"), data)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleContentDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.content.Delete(r.Context(), r.PathValue("type"), r.PathValue("id")); err != nil {
		s.writeContentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeData reads a JSON object body into a data map, writing a 400 and
// returning false on malformed input.
func (s *Server) decodeData(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		s.writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return nil, false
	}
	return data, true
}

// writeContentError maps domain errors to HTTP status codes. Unknown type and
// missing item are 404; validation failures are 422 with the field issues.
func (s *Server) writeContentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrUnknownType), errors.Is(err, content.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, content.ErrValidation):
		var ve *content.ValidationError
		if errors.As(err, &ve) {
			s.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  "validation failed",
				"issues": ve.Issues,
			})
			return
		}
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, composition.ErrNotFound):
		s.writeError(w, http.StatusConflict, "setup not completed; visit /setup")
	default:
		s.log.Error("content request", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
	}
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
