package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/glyphux/glyphux/internal/permission"
)

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	u, err := s.identities.CreateUser(r.Context(), body.Email, body.Password, body.Role)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.identities.ListUsers(r.Context())
	if err != nil {
		s.log.Error("list users", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// handleUpdateUserRole changes {id}'s role (mirrors internal/graphql's
// UpdateUserRole mutation). Admin-only (users:manage), enforced both here
// (fast-fail) and inside identity.Service.UpdateRole itself (PRD §10.5
// domain-boundary defense-in-depth).
func (s *Server) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	id, ok := s.userIDFromPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	u, err := s.identities.UpdateRole(r.Context(), s.principal(r), id, body.Role)
	if err != nil {
		s.writeUserManagementError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, u)
}

// handleDeactivateUser deactivates {id}'s account and revokes every session
// it currently holds — a deactivated account must not keep working off a
// session opened before deactivation until that session's TTL happens to
// expire on its own.
func (s *Server) handleDeactivateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := s.userIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.identities.Deactivate(r.Context(), s.principal(r), id); err != nil {
		s.writeUserManagementError(w, err)
		return
	}
	if err := s.sessions.RevokeAllForUser(r.Context(), id); err != nil {
		s.log.Error("revoke sessions after deactivation", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReactivateUser re-enables a previously deactivated account.
func (s *Server) handleReactivateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := s.userIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.identities.Reactivate(r.Context(), s.principal(r), id); err != nil {
		s.writeUserManagementError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) userIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid user id")
		return 0, false
	}
	return id, true
}

func (s *Server) writeUserManagementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, permission.ErrDenied):
		// Reachable only if the domain-layer check fails despite this
		// package's own requireCapability already having passed
		// (defense-in-depth, PRD §10.5) — 403 either way.
		s.writeError(w, http.StatusForbidden, "insufficient permissions")
	default:
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
	}
}
