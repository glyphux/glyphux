package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/contract"
)

// themeRegionsParam parses the "theme_regions" query/body parameter into the
// []string pkg/compat's Check functions expect — a comma-separated list of
// region names the destination theme declares (mirroring Theme.Regions()),
// or its absence/empty string meaning nil ("no declared restriction"). This
// host currently has no live "selected theme" concept wired into the
// running daemon (see this ticket's tracking doc), so a caller — today, an
// admin-ui client that already knows which theme it's targeting — supplies
// this explicitly rather than the server resolving it from server-side
// state that doesn't exist yet.
func themeRegionsParam(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// handlePresetsList serves every saved Composition Preset. No capability
// gate — mirrors block/layout reads' public-read convention (PRD §13's
// artifacts are schema/template-shaped, not content).
func (s *Server) handlePresetsList(w http.ResponseWriter, r *http.Request) {
	if s.presets == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	list, err := s.presets.List(r.Context())
	if err != nil {
		s.writePresetError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"presets": list})
}

// handlePresetGet serves one saved Composition Preset by ID.
func (s *Server) handlePresetGet(w http.ResponseWriter, r *http.Request) {
	if s.presets == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	p, err := s.presets.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writePresetError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

// handlePresetCreate validates then persists a new Composition Preset —
// structural (contract.CompositionPreset.Validate) and registry-existence
// (compat.CheckPreset against the shared registry) validation both happen
// inside preset.Store.Save, mirroring layout PUT's validate-then-persist
// pattern. Requires presets:manage and CSRF protection.
func (s *Server) handlePresetCreate(w http.ResponseWriter, r *http.Request) {
	if s.presets == nil || s.blocks == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var p contract.CompositionPreset
	if !s.decodeJSON(w, r, &p) {
		return
	}
	saved, err := s.presets.Save(r.Context(), s.principal(r), s.blocks, &p)
	if err != nil {
		s.writePresetError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, saved)
}

// handlePresetCheck runs the compatibility contract for the preset named by
// the {id} path segment against the shared registry and the "theme_regions"
// query parameter (see themeRegionsParam), without mutating anything. No
// capability gate — a read-only "would importing this work?" query.
func (s *Server) handlePresetCheck(w http.ResponseWriter, r *http.Request) {
	if s.presets == nil || s.blocks == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	result, err := s.presets.Check(r.Context(), s.blocks, themeRegionsParam(r.URL.Query().Get("theme_regions")), r.PathValue("id"))
	if err != nil {
		s.writePresetError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

// presetImportRequest is POST /api/v0/presets/{id}/import's body: which
// route to merge the preset's Layout fragment into, and (optionally) which
// theme regions the destination declares — see themeRegionsParam.
type presetImportRequest struct {
	Route        string   `json:"route"`
	ThemeRegions []string `json:"theme_regions,omitempty"`
}

// handlePresetImport checks the preset named by {id} against the shared
// registry and the request body's declared theme_regions, and — only if
// compatible — merges it into the named route's Layout. Always responds 200
// with the compat.Result even when incompatible (PRD §13.3's "explainable,
// not a bare rejection" requirement) — a caller distinguishes "declined to
// import" from "server error" by reading `.compatible`, not the HTTP status.
// Requires presets:manage and CSRF protection (it mutates a Layout).
func (s *Server) handlePresetImport(w http.ResponseWriter, r *http.Request) {
	if s.presets == nil || s.blocks == nil || s.layouts == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req presetImportRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Route == "" {
		s.writeError(w, http.StatusBadRequest, "route must not be empty")
		return
	}
	result, err := s.presets.Import(r.Context(), s.principal(r), s.blocks, s.layouts, req.ThemeRegions, r.PathValue("id"), req.Route)
	if err != nil {
		s.writePresetError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

// writePresetError maps internal/preset domain errors to HTTP status codes.
func (s *Server) writePresetError(w http.ResponseWriter, err error) {
	if errors.Is(err, preset.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeDomainError(w, err, "preset request")
}
