package api

import (
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/theme"
	"github.com/glyphux/glyphux/themes/starter"
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

// handleLayoutPreview renders an UNSAVED draft Layer-2 Layout document
// through the real themes/starter theme and returns the resulting HTML —
// Ticket P4.5's "editor renders the same composition the API serves,"
// applied to a draft that may never have been persisted via
// handleLayoutPut at all. The request body is a bare contract.Layout, the
// same shape PUT accepts (not wrapped in an envelope), so a caller can
// preview exactly the document it would otherwise PUT.
//
// This handler never calls s.layouts.Save (or anything else that writes to
// storage) — Save is what persists a Layout; this endpoint only renders one.
// It is validated exactly like a real save (structural
// contract.Layout.Validate, then registry-existence blocks.ValidateLayout)
// before rendering, so a preview's pass/fail on validation matches what a
// subsequent real Save would do, and the same writeLayoutError mapping
// (422 with issues) is reused for both.
//
// No Layer-1 content item is passed to the CompositionView: a preview
// request previews a route's structural draft in isolation, not any one
// specific content item, and themes/starter's only optional use of
// view.Item() is an HTML <title> (see starter.go) — omitting it just means
// the preview document's <title> is empty, not that anything fails to
// render. Requires layouts:manage (the same capability that gates saving a
// layout — this endpoint accepts and renders arbitrary caller-supplied
// block trees, which is exactly the surface only the builder's authors
// should be able to exercise) and CSRF protection, matching every other
// state-changing-shaped POST in this package even though this one performs
// no persistence.
func (s *Server) handleLayoutPreview(w http.ResponseWriter, r *http.Request) {
	if s.layouts == nil || s.blocks == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var l contract.Layout
	if !s.decodeJSON(w, r, &l) {
		return
	}
	if err := l.Validate(); err != nil {
		s.writeLayoutError(w, err)
		return
	}
	if err := blocks.ValidateLayout(&l, s.blocks); err != nil {
		s.writeLayoutError(w, err)
		return
	}

	view := theme.NewCompositionView(nil, &l)
	html, contentType, err := starter.New(s.blocks).Render(r.Context(), view)
	if err != nil {
		s.log.Error("render layout preview", "error", err)
		s.writeError(w, http.StatusInternalServerError, "render failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"html":         string(html),
		"content_type": contentType,
	})
}

// writeLayoutError maps internal/layout domain errors to HTTP status codes.
// Its own not-found sentinel is checked here; everything else (permission
// denial, both flavors of validation failure — structural
// contract.ValidationErrors and route-format errors, which
// layout.Store.Save also reports as contract.ValidationErrors — and the
// unknown-error fallback) is the identical tail writeDomainError shares with
// writeContentTypeError.
func (s *Server) writeLayoutError(w http.ResponseWriter, err error) {
	if errors.Is(err, layout.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeDomainError(w, err, "layout request")
}
