package api

import (
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/pkg/contract"
)

// handleBundlesList serves every saved Composition Bundle. No capability
// gate — mirrors handlePresetsList's identical public-read reasoning.
func (s *Server) handleBundlesList(w http.ResponseWriter, r *http.Request) {
	if s.bundles == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	list, err := s.bundles.List(r.Context())
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"bundles": list})
}

// handleBundleGet serves one saved Composition Bundle by ID.
func (s *Server) handleBundleGet(w http.ResponseWriter, r *http.Request) {
	if s.bundles == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	b, err := s.bundles.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, b)
}

// handleBundleCreate validates then persists a new Composition Bundle,
// mirroring handlePresetCreate's identical pattern. Requires presets:manage
// and CSRF protection.
func (s *Server) handleBundleCreate(w http.ResponseWriter, r *http.Request) {
	if s.bundles == nil || s.blocks == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var b contract.CompositionBundle
	if !s.decodeJSON(w, r, &b) {
		return
	}
	saved, err := s.bundles.Save(r.Context(), s.principal(r), s.blocks, &b)
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, saved)
}

// handleBundleCheck runs the compatibility contract for the bundle named by
// {id} against the shared registry and the "theme_regions" query parameter
// (see themeRegionsParam), without mutating anything. No capability gate.
func (s *Server) handleBundleCheck(w http.ResponseWriter, r *http.Request) {
	if s.bundles == nil || s.blocks == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	result, err := s.bundles.Check(r.Context(), s.blocks, themeRegionsParam(r.URL.Query().Get("theme_regions")), r.PathValue("id"))
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

// bundleImportRequest is POST /api/v0/bundles/{id}/import's body — just the
// destination theme's declared regions (see themeRegionsParam); unlike a
// preset import, a bundle declares its own page routes, so there is no
// separate "which route" the caller must supply.
type bundleImportRequest struct {
	ThemeRegions []string `json:"theme_regions,omitempty"`
}

// handleBundleImport checks the bundle named by {id} against the shared
// registry and the request body's declared theme_regions, and — only if
// compatible — merges every page's Layout and creates every sample content
// item (internal/bundle.Store.Import). Always responds 200 with the
// bundle.ImportResult (whose embedded Compat carries the same "explainable,
// not a bare rejection" Result every preset import does) even when
// incompatible. Requires presets:manage and CSRF protection.
func (s *Server) handleBundleImport(w http.ResponseWriter, r *http.Request) {
	if s.bundles == nil || s.blocks == nil || s.layouts == nil || s.content == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req bundleImportRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	result, err := s.bundles.Import(r.Context(), s.principal(r), s.blocks, s.layouts, s.content, req.ThemeRegions, r.PathValue("id"))
	if err != nil {
		s.writeBundleError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

// writeBundleError maps internal/bundle domain errors to HTTP status codes.
func (s *Server) writeBundleError(w http.ResponseWriter, err error) {
	if errors.Is(err, bundle.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeDomainError(w, err, "bundle request")
}
