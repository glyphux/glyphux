package api

import (
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

// handleBlocksList serves every block Definition registered in the shared,
// running daemon's *blocks.Registry (WithLayouts) — the same registry a
// layout PUT validates block types against, so this listing is always
// exactly what's actually registerable right now, not a stale or private
// copy. 404s if WithLayouts wasn't configured (see that option's doc
// comment); no capability gate, mirroring content-types list's public-read
// convention (block definitions are schema, not content, so there is
// nothing sensitive in exposing them to any caller).
func (s *Server) handleBlocksList(w http.ResponseWriter, r *http.Request) {
	if s.blocks == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	list := s.blocks.List()
	if list == nil {
		list = []blocks.Definition{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"blocks": list})
}

// handleLayoutGet serves the Layer-2 Layout document saved for the route
// path segment(s), or 404 if none has been saved yet (or WithLayouts wasn't
// configured). No capability gate — reading a layout is a public read, same
// as content-types list and content reads.
func (s *Server) handleLayoutGet(w http.ResponseWriter, r *http.Request) {
	if s.layouts == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	l, err := s.layouts.Load(r.Context(), r.PathValue("route"))
	if err != nil {
		s.writeLayoutError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, l)
}

// handleLayoutPut validates then persists a Layer-2 Layout document for the
// route path segment(s) — structural validation (contract.Layout.Validate)
// and registry-existence validation (blocks.ValidateLayout against the
// shared registry) both happen inside layout.Store.Save, mirroring
// content-types PUT's validate-then-persist pattern. Requires
// layouts:manage (checked here at the transport boundary and again inside
// the store, defense-in-depth per PRD §10.5) and CSRF protection.
func (s *Server) handleLayoutPut(w http.ResponseWriter, r *http.Request) {
	if s.layouts == nil || s.blocks == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var l contract.Layout
	if !s.decodeJSON(w, r, &l) {
		return
	}
	route := r.PathValue("route")
	if err := s.layouts.Save(r.Context(), s.principal(r), s.blocks, route, &l); err != nil {
		s.writeLayoutError(w, err)
		return
	}
	saved, err := s.layouts.Load(r.Context(), route)
	if err != nil {
		s.log.Error("reload saved layout", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, saved)
}

// writeLayoutError maps internal/layout domain errors to HTTP status codes,
// mirroring writeContentTypeError's shape: not-found is 404, permission
// denial is 403, and both flavors of validation failure (structural
// contract.ValidationErrors and route-format errors, which layout.Store.Save
// also reports as contract.ValidationErrors) are 422 with the issue list.
func (s *Server) writeLayoutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, layout.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, permission.ErrDenied):
		s.writeError(w, http.StatusForbidden, "insufficient permissions")
	default:
		var verrs contract.ValidationErrors
		if errors.As(err, &verrs) {
			issues := make([]string, len(verrs))
			for i, v := range verrs {
				issues[i] = v.Error()
			}
			s.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  "validation failed",
				"issues": issues,
			})
			return
		}
		s.log.Error("layout request", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
	}
}
