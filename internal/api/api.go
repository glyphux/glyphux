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
	"strconv"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/permission"
)

// Server exposes the API surface. It is a client of the domain APIs — it holds
// no privileged kernel access of its own.
type Server struct {
	compositions      *composition.Store
	content           *content.API
	media             *media.API
	identities        *identity.Service
	sessions          *identity.Sessions
	log               *slog.Logger
	loginLimiter      *loginLimiter
	trustProxyHeaders bool
}

// Option configures optional Server behavior beyond the required domain APIs.
type Option func(*Server)

// TrustProxyHeaders controls whether X-Forwarded-Proto is honored when
// deciding a request arrived over HTTPS (e.g. for the session cookie's
// Secure flag) — mirrors setup.Wizard's identical trust decision. An
// unvouched header is just a client claim, so this defaults to false; only
// enable it when a reverse proxy in front of the daemon is known to set (and
// strip any client-supplied) X-Forwarded-Proto.
func TrustProxyHeaders(trust bool) Option {
	return func(s *Server) { s.trustProxyHeaders = trust }
}

// New builds the API transport over the given domain APIs.
func New(comps *composition.Store, contentAPI *content.API, mediaAPI *media.API, identities *identity.Service, sessions *identity.Sessions, log *slog.Logger, opts ...Option) *Server {
	s := &Server{
		compositions: comps,
		content:      contentAPI,
		media:        mediaAPI,
		identities:   identities,
		sessions:     sessions,
		log:          log,
		loginLimiter: newLoginLimiter(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Routes registers the API endpoints on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/v0/composition", s.handleComposition)
	mux.HandleFunc("GET /api/v0/content/ping", s.handlePing)

	// Content-type management (structural schema changes; admin-only).
	mux.HandleFunc("GET /api/v0/content-types", s.handleContentTypesList)
	mux.HandleFunc("PUT /api/v0/content-types/{name}", s.requireCapability(permission.ContentTypesManage, s.handleContentTypePut))
	mux.HandleFunc("DELETE /api/v0/content-types/{name}", s.requireCapability(permission.ContentTypesManage, s.handleContentTypeDelete))

	// Authentication (slice 1.7).
	mux.HandleFunc("POST /api/v0/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v0/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v0/auth/me", s.handleMe)

	// User management (admin-only; slice 1.8).
	mux.HandleFunc("POST /api/v0/users", s.requireCapability(permission.UsersManage, s.handleCreateUser))
	mux.HandleFunc("GET /api/v0/users", s.requireCapability(permission.UsersManage, s.handleListUsers))

	// Content CRUD (slice 1.2). The literal /ping route above is more specific
	// than {type}, so ServeMux prefers it — no shadowing. Reads are public;
	// mutations require the content:write capability (slice 1.8).
	mux.HandleFunc("POST /api/v0/content/{type}", s.requireCapability(permission.ContentWrite, s.handleContentCreate))
	mux.HandleFunc("GET /api/v0/content/{type}", s.handleContentList)
	mux.HandleFunc("GET /api/v0/content/{type}/{id}", s.handleContentGet)
	mux.HandleFunc("PUT /api/v0/content/{type}/{id}", s.requireCapability(permission.ContentWrite, s.handleContentUpdate))
	mux.HandleFunc("DELETE /api/v0/content/{type}/{id}", s.requireCapability(permission.ContentWrite, s.handleContentDelete))

	// Drafts, publish, versioning (slice 1.5).
	mux.HandleFunc("POST /api/v0/content/{type}/{id}/publish", s.requireCapability(permission.ContentPublish, s.handleContentPublish))
	mux.HandleFunc("POST /api/v0/content/{type}/{id}/unpublish", s.requireCapability(permission.ContentPublish, s.handleContentUnpublish))
	mux.HandleFunc("GET /api/v0/content/{type}/{id}/versions", s.handleContentListVersions)
	mux.HandleFunc("POST /api/v0/content/{type}/{id}/rollback/{version}", s.requireCapability(permission.ContentWrite, s.handleContentRollback))

	// Media pipeline + library (slice 1.6). Reads are public; upload/delete
	// require media:write.
	mux.HandleFunc("POST /api/v0/media", s.requireCapability(permission.MediaWrite, s.handleMediaUpload))
	mux.HandleFunc("GET /api/v0/media", s.handleMediaList)
	mux.HandleFunc("GET /api/v0/media/{id}", s.handleMediaGet)
	mux.HandleFunc("GET /api/v0/media/{id}/file", s.handleMediaFile)
	mux.HandleFunc("DELETE /api/v0/media/{id}", s.requireCapability(permission.MediaWrite, s.handleMediaDelete))
}

func (s *Server) handleContentCreate(w http.ResponseWriter, r *http.Request) {
	data, ok := s.decodeData(w, r)
	if !ok {
		return
	}
	item, err := s.content.Create(r.Context(), s.principal(r), r.PathValue("type"), data)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, item)
}

// canReadDrafts reports whether the request's principal, if any, holds
// content:read_drafts — the gate between the admin view (every status) and
// the public view (published only) of content reads.
func (s *Server) canReadDrafts(r *http.Request) bool {
	user, ok := s.currentUser(r)
	return ok && permission.Allows(user.Role, permission.ContentReadDrafts)
}

func (s *Server) handleContentList(w http.ResponseWriter, r *http.Request) {
	typeName := r.PathValue("type")
	locale := r.URL.Query().Get("locale")
	drafts := s.canReadDrafts(r)

	var (
		items any
		err   error
	)
	switch {
	case locale != "" && drafts:
		items, err = s.content.ListLocalized(r.Context(), s.principal(r), typeName, locale)
	case locale != "" && !drafts:
		items, err = s.content.ListLocalizedPublished(r.Context(), typeName, locale)
	case drafts:
		items, err = s.content.List(r.Context(), s.principal(r), typeName)
	default:
		items, err = s.content.ListPublished(r.Context(), typeName)
	}
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleContentGet(w http.ResponseWriter, r *http.Request) {
	typeName, id := r.PathValue("type"), r.PathValue("id")
	locale := r.URL.Query().Get("locale")
	drafts := s.canReadDrafts(r)

	var (
		item *content.Item
		err  error
	)
	switch {
	case locale != "" && drafts:
		item, err = s.content.GetLocalized(r.Context(), s.principal(r), typeName, id, locale)
	case locale != "" && !drafts:
		item, err = s.content.GetLocalizedPublished(r.Context(), typeName, id, locale)
	case drafts:
		item, err = s.content.Get(r.Context(), s.principal(r), typeName, id)
	default:
		item, err = s.content.GetPublished(r.Context(), typeName, id)
	}
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
	item, err := s.content.Update(r.Context(), s.principal(r), r.PathValue("type"), r.PathValue("id"), data)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleContentDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.content.Delete(r.Context(), s.principal(r), r.PathValue("type"), r.PathValue("id")); err != nil {
		s.writeContentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleContentPublish(w http.ResponseWriter, r *http.Request) {
	item, err := s.content.Publish(r.Context(), s.principal(r), r.PathValue("type"), r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleContentUnpublish(w http.ResponseWriter, r *http.Request) {
	item, err := s.content.Unpublish(r.Context(), s.principal(r), r.PathValue("type"), r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleContentListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.content.ListVersions(r.Context(), r.PathValue("type"), r.PathValue("id"))
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) handleContentRollback(w http.ResponseWriter, r *http.Request) {
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "version must be an integer")
		return
	}
	item, err := s.content.Rollback(r.Context(), s.principal(r), r.PathValue("type"), r.PathValue("id"), version)
	if err != nil {
		s.writeContentError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

// decodeData reads a JSON object body into a data map, writing a 400 (or 413
// if the body exceeded the request size cap) and returning false on failure.
func (s *Server) decodeData(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var data map[string]any
	if !s.decodeJSON(w, r, &data) {
		return nil, false
	}
	return data, true
}

// decodeJSON decodes a JSON request body into v, writing 413 for a body that
// exceeded the request size cap or 400 for any other malformed input.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		s.writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return false
	}
	return true
}

// writeContentError maps domain errors to HTTP status codes. Unknown type and
// missing item are 404; validation failures are 422 with the field issues.
func (s *Server) writeContentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, permission.ErrDenied):
		// Reachable only if a domain-API capability check fails despite this
		// package's own requireCapability/canReadDrafts fast-fail already
		// having passed (defense-in-depth, PRD §10.5) — 403 either way.
		s.writeError(w, http.StatusForbidden, "insufficient permissions")
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

// handleReady reports whether the daemon can actually serve requests, not
// just that the process is running (handleHealth). An orchestrator should
// stop routing traffic on a non-200 here even while /healthz stays green.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.compositions.Ping(r.Context()); err != nil {
		s.log.Warn("readiness check failed", "error", err)
		s.writeError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
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
