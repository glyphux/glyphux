package api

import (
	"net/http"

	"github.com/glyphux/glyphux/internal/audit"
)

// WithAuditLogger wires the audit trail endpoint (Ticket T7 / gap 4):
// GET /api/v0/audit?plugin=<name> lists every record stamped with that
// plugin name — a configured plugin's boundary-gate rows (hostapi.* actions
// via KernelDeps.Audit) or the item-level stamps "content" / "media" /
// "identity" — oldest-first. Admin-only: the route wraps the handler in
// requireCapability(permission.PluginsManage), the same admin-only gate as
// the consent surface. Omitting this option leaves the route 404ing,
// matching the other opt-in transports (WithOAuth, WithLayouts, ...).
func WithAuditLogger(logger *audit.Logger) Option {
	return func(s *Server) { s.audit = logger }
}

// handleAuditList serves GET /api/v0/audit?plugin=. The plugin query
// parameter is required — the endpoint's only accessor is ListByPlugin
// (owner-confirmed resolution of the T7 ambiguity) — and a missing one is a
// 400. Anonymous requests never reach the handler (401 at the
// requireCapability wrapper); authenticated-but-not-admin get 403; an admin
// gets 200 with {"records": [...]}.
func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		s.writeError(w, http.StatusNotFound, "audit logging not configured")
		return
	}
	plugin := r.URL.Query().Get("plugin")
	if plugin == "" {
		s.writeError(w, http.StatusBadRequest, "missing required query parameter: plugin")
		return
	}
	records, err := s.audit.ListByPlugin(r.Context(), plugin)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "list audit records")
		return
	}
	if records == nil {
		records = []audit.Record{} // wire contract: "records" is always an array, never null
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"records": records})
}
