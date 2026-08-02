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

	"github.com/glyphux/glyphux/capabilities/ai"
	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/marketplace"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Server exposes the API surface. It is a client of the domain APIs — it holds
// no privileged kernel access of its own.
type Server struct {
	compositions      *composition.Store
	content           *content.API
	media             *media.API
	identities        *identity.Service
	sessions          *identity.Sessions
	oauth             *identity.OAuthManager
	oauthRedirectBase string
	layouts           *layout.Store
	blocks            *blocks.Registry
	presets           *preset.Store
	bundles           *bundle.Store
	ai                *ai.Service
	consent           *consent.Engine
	audit             *audit.Logger        // nil unless WithAuditLogger wired (T7)
	marketplace       *marketplace.Manager // nil unless WithMarketplace wired (T8)
	pluginManifests   []sdk.Manifest
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

// WithOAuth enables the OAuth2/social-login routes, driven by manager, with
// redirectBase as this instance's externally-reachable base URL (used to
// build each provider's redirect_uri, e.g. redirectBase +
// "/api/v0/auth/oauth/github/callback"). Omitting this option (the zero
// value) leaves the OAuth routes returning 404 — OAuth login is opt-in
// server-side configuration, same as MFA is opt-in per account.
func WithOAuth(manager *identity.OAuthManager, redirectBase string) Option {
	return func(s *Server) {
		s.oauth = manager
		s.oauthRedirectBase = redirectBase
	}
}

// WithLayouts enables the Layer-2 block/layout transport (PRD §14 slice
// 4.4a): GET /api/v0/blocks, GET/PUT /api/v0/layouts/{route}. store and
// registry are the daemon's real, shared internal/layout.Store and
// pkg/blocks.Registry — the same registry blocks/firstparty.RegisterAll
// populates at daemon construction time (cmd/glyphuxd/main.go), so what this
// endpoint lists and validates against is exactly what the running instance
// actually has registered, not a private copy.
//
// Omitting this option (the zero value) leaves the three routes 404ing,
// mirroring WithOAuth's identical "opt-in, 404 unless configured" precedent
// — every pre-existing api.New call site across the test suite predates
// this slice and has no reason to configure block/layout support just to
// keep testing unrelated routes.
func WithLayouts(store *layout.Store, registry *blocks.Registry) Option {
	return func(s *Server) {
		s.layouts = store
		s.blocks = registry
	}
}

// WithPresets enables the Composition Preset/Bundle transport (PRD §13.2/
// §13.3, Ticket P4.6): GET/POST /api/v0/presets, GET /api/v0/presets/{id},
// GET /api/v0/presets/{id}/check, POST /api/v0/presets/{id}/import, and the
// equivalent /api/v0/bundles routes. presetStore and bundleStore are the
// daemon's real, shared internal/preset.Store and internal/bundle.Store.
//
// Requires WithLayouts to have also been configured — a preset/bundle
// import merges into a Layout, so this option is meaningless without a live
// *layout.Store and *blocks.Registry already wired in; New itself performs
// no such check (mirroring every other Option's "just an assignment, no
// cross-option validation" convention), but omitting WithLayouts alongside
// this one leaves every preset/bundle handler's own s.layouts/s.blocks nil
// guard 404ing instead of working.
//
// Omitting this option (the zero value) leaves the new routes 404ing,
// mirroring WithLayouts/WithOAuth's identical "opt-in, 404 unless
// configured" precedent.
func WithPresets(presetStore *preset.Store, bundleStore *bundle.Store) Option {
	return func(s *Server) {
		s.presets = presetStore
		s.bundles = bundleStore
	}
}

// WithAI enables Ticket P4.8's "AI authoring in the builder" endpoint (POST
// /api/v0/ai/compose, PRD §14.1 Surface 2): service is the daemon's real,
// shared *capabilities/ai.Service (the same Service a first-party or
// third-party plugin would call via its own HostAPI, PRD §14 Ticket P3.6),
// configured with whatever real provider Adapter the operator wired up.
//
// This Server itself becomes the "calling plugin" for the purpose of
// exercising that Service — see ai.go's aiCallerHost for the manifest this
// builds and why (no prior internal/api precedent for this existed before
// this ticket; see this ticket's tracking doc for that investigation).
//
// Requires WithLayouts and WithPresets to have also been configured — the
// compose endpoint validates a proposed fragment through
// internal/preset's exact save-time validation (contract.CompositionPreset.
// Validate + pkg/compat.CheckPreset) and previews it through
// internal/layout's exact draft-preview validation (layout.ValidateDraft),
// so it is meaningless without a live *blocks.Registry and *layout.Store
// already wired in. New performs no such check (mirroring WithPresets'
// identical "just an assignment, no cross-option validation" precedent);
// omitting WithLayouts/WithPresets alongside this one leaves the compose
// handler's own s.blocks/s.layouts nil guard 404ing instead of working.
//
// Omitting this option (the zero value) leaves POST /api/v0/ai/compose
// 404ing, mirroring WithOAuth/WithLayouts/WithPresets' identical "opt-in,
// 404 unless configured" precedent — cmd/glyphuxd does not wire this in yet
// because no operator-facing AI provider credential configuration exists in
// internal/config today (see this ticket's tracking doc); tests exercise
// this option directly with an in-process fake Adapter.
func WithAI(service *ai.Service) Option {
	return func(s *Server) { s.ai = service }
}

// WithConsent wires the install-time consent engine (Ticket T4 / gap 2): it
// registers the plugin consent routes (GET /api/v0/plugins, GET
// /api/v0/plugins/consent-requests, POST
// /api/v0/plugins/consent-requests/{plugin}/decide), all admin-only
// (plugins:manage; the decide mutation also requires CSRF). plugins is the
// set of registered plugins whose manifests are the consent subjects — the
// daemon passes the T5 registrar's Registered() set. Omitting this option
// (the zero value) leaves the consent routes returning 404, exactly like
// the other opt-in transports.
func WithConsent(engine *consent.Engine, plugins []sdk.Plugin) Option {
	return func(s *Server) {
		s.consent = engine
		s.pluginManifests = make([]sdk.Manifest, 0, len(plugins))
		for _, p := range plugins {
			s.pluginManifests = append(s.pluginManifests, p.Manifest())
		}
	}
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
	// Mutations get requireCSRF too (slice 1.9): a cookie-authenticated
	// state-changing request must mirror the CSRF cookie in a header.
	mux.HandleFunc("GET /api/v0/content-types", s.handleContentTypesList)
	mux.HandleFunc("PUT /api/v0/content-types/{name}", s.requireCSRF(s.requireCapability(permission.ContentTypesManage, s.handleContentTypePut)))
	mux.HandleFunc("DELETE /api/v0/content-types/{name}", s.requireCSRF(s.requireCapability(permission.ContentTypesManage, s.handleContentTypeDelete)))

	// Layer-2 block/layout transport (slice 4.4a). Block listing is a public
	// read, same as content-types list; layout writes are admin-only
	// (layouts:manage) and CSRF-protected like every other mutation. 404s if
	// WithLayouts wasn't configured — see that option's doc comment.
	mux.HandleFunc("GET /api/v0/blocks", s.handleBlocksList)
	mux.HandleFunc("GET /api/v0/layouts/{route...}", s.handleLayoutGet)
	mux.HandleFunc("PUT /api/v0/layouts/{route...}", s.requireCSRF(s.requireCapability(permission.LayoutsManage, s.handleLayoutPut)))
	// Ticket P4.5 (live preview): renders an unsaved draft Layout through the
	// real themes/starter theme, no persistence. Gated the same as a real
	// save (layouts:manage + CSRF) since it accepts and renders arbitrary
	// caller-supplied block trees — see handleLayoutPreview's doc comment.
	mux.HandleFunc("POST /api/v0/layouts/preview", s.requireCSRF(s.requireCapability(permission.LayoutsManage, s.handleLayoutPreview)))

	// Composition Presets & Bundles (slice 4.6, PRD §13.2/§13.3): reads and
	// compatibility checks are public, same reasoning as blocks/layouts
	// above; saving and importing (which mutates Layouts, and for bundles,
	// creates sample content) require presets:manage and CSRF. 404s if
	// WithPresets wasn't configured.
	mux.HandleFunc("GET /api/v0/presets", s.handlePresetsList)
	mux.HandleFunc("POST /api/v0/presets", s.requireCSRF(s.requireCapability(permission.PresetsManage, s.handlePresetCreate)))
	mux.HandleFunc("GET /api/v0/presets/{id}", s.handlePresetGet)
	mux.HandleFunc("GET /api/v0/presets/{id}/check", s.handlePresetCheck)
	mux.HandleFunc("POST /api/v0/presets/{id}/import", s.requireCSRF(s.requireCapability(permission.PresetsManage, s.handlePresetImport)))

	mux.HandleFunc("GET /api/v0/bundles", s.handleBundlesList)
	mux.HandleFunc("POST /api/v0/bundles", s.requireCSRF(s.requireCapability(permission.PresetsManage, s.handleBundleCreate)))
	mux.HandleFunc("GET /api/v0/bundles/{id}", s.handleBundleGet)
	mux.HandleFunc("GET /api/v0/bundles/{id}/check", s.handleBundleCheck)
	mux.HandleFunc("POST /api/v0/bundles/{id}/import", s.requireCSRF(s.requireCapability(permission.PresetsManage, s.handleBundleImport)))

	// AI authoring in the builder (Ticket P4.8, PRD §14.1 Surface 2): emits
	// a Layer-2 composition fragment from a prompt, validated through the
	// exact preset-import validation path (never a parallel AI-specific
	// one) and rendered through slice 4.5's live-preview machinery. Gated
	// like presets/layouts writes (layouts:manage AND presets:manage,
	// checked inside the handler — see handleAICompose's own doc comment)
	// plus CSRF; 404s if WithAI wasn't configured. Never persists anything
	// itself — accepting a proposed fragment reuses POST /api/v0/presets
	// then POST /api/v0/presets/{id}/import unchanged.
	mux.HandleFunc("POST /api/v0/ai/compose", s.requireCSRF(s.requireCapability(permission.LayoutsManage, s.requireCapability(permission.PresetsManage, s.handleAICompose))))

	// Plugin consent (Ticket T4 / gap 2): install-time consent decisions
	// change the site's trust boundary, so the whole surface is admin-only
	// (plugins:manage) — listing plugins and reading pending requests
	// exposes each plugin's full requested permission surface, and the
	// decide mutation is CSRF-protected like every other state-changing
	// route. 404s if WithConsent wasn't configured (handlers check
	// s.consent == nil), matching the other opt-in transports.
	mux.HandleFunc("GET /api/v0/plugins", s.requireCapability(permission.PluginsManage, s.handlePluginsList))
	mux.HandleFunc("GET /api/v0/plugins/consent-requests", s.requireCapability(permission.PluginsManage, s.handleConsentRequestsList))
	mux.HandleFunc("POST /api/v0/plugins/consent-requests/{plugin}/decide", s.requireCSRF(s.requireCapability(permission.PluginsManage, s.handleConsentDecide)))

	// Audit trail (gap 4 / Ticket T7): admin-only, gated on plugins:manage
	// like the consent surface — the audit log exposes every plugin boundary
	// decision and item-level write, so it is operator-only infrastructure.
	// The ?plugin= query parameter is required (ListByPlugin is the only
	// accessor). 404s if WithAuditLogger wasn't configured, matching the
	// other opt-in transports.
	mux.HandleFunc("GET /api/v0/audit", s.requireCapability(permission.PluginsManage, s.handleAuditList))

	// Marketplace (gap 8 / Ticket T8): the catalog read, the install
	// mutation, and the entitlement registration/list. The whole surface is
	// admin-only (plugins:manage) — the catalog lists what the host would
	// install and the mutations change the host's installed surface — and
	// mutations get requireCSRF like every other state-changing route. 404s
	// if WithMarketplace wasn't configured, matching the other opt-in
	// transports.
	mux.HandleFunc("GET /api/v0/marketplace/catalog", s.requireCapability(permission.PluginsManage, s.handleMarketplaceCatalog))
	mux.HandleFunc("POST /api/v0/marketplace/packages/{id}/install", s.requireCSRF(s.requireCapability(permission.PluginsManage, s.handleMarketplaceInstall)))
	mux.HandleFunc("GET /api/v0/marketplace/entitlements", s.requireCapability(permission.PluginsManage, s.handleMarketplaceEntitlementsList))
	mux.HandleFunc("POST /api/v0/marketplace/entitlements", s.requireCSRF(s.requireCapability(permission.PluginsManage, s.handleMarketplaceEntitlementsRegister)))

	// Authentication (slice 1.7). Login has no session cookie yet on a
	// fresh visit, so requireCSRF is a no-op there; it still protects an
	// already-authenticated victim from a forged cross-site re-login/logout.
	mux.HandleFunc("POST /api/v0/auth/login", s.requireCSRF(s.handleLogin))
	mux.HandleFunc("POST /api/v0/auth/logout", s.requireCSRF(s.handleLogout))
	mux.HandleFunc("GET /api/v0/auth/me", s.handleMe)

	// TOTP MFA: opt-in per account (slice 0009). Enroll/confirm/disable are
	// self-service cookie-authenticated mutations, so they get requireCSRF
	// like every other mutation (slice 1.9); the verify step is deliberately
	// public and CSRF-exempt — the caller isn't fully logged in yet, that's
	// the point of a second factor, and it has no session cookie to forge
	// against.
	mux.HandleFunc("POST /api/v0/auth/mfa/enroll", s.requireCSRF(s.requireUser(s.handleMFAEnroll)))
	mux.HandleFunc("POST /api/v0/auth/mfa/confirm", s.requireCSRF(s.requireUser(s.handleMFAConfirm)))
	mux.HandleFunc("POST /api/v0/auth/mfa/disable", s.requireCSRF(s.requireUser(s.handleMFADisable)))
	mux.HandleFunc("POST /api/v0/auth/mfa/verify", s.handleMFAVerify)

	// OAuth2/social login (slice 0009). 404s unless WithOAuth configured a
	// manager — see that option's doc comment. No requireCSRF: the start
	// route only redirects to the provider, and the callback is a GET the
	// provider itself issues (no cookie jar to forge from); the OAuth state
	// parameter is this flow's CSRF protection instead.
	mux.HandleFunc("GET /api/v0/auth/oauth/{provider}/start", s.handleOAuthStart)
	mux.HandleFunc("GET /api/v0/auth/oauth/{provider}/callback", s.handleOAuthCallback)

	// User management (admin-only; slice 1.8, extended in slice 0009 with
	// role change and deactivate/reactivate).
	mux.HandleFunc("POST /api/v0/users", s.requireCSRF(s.requireCapability(permission.UsersManage, s.handleCreateUser)))
	mux.HandleFunc("GET /api/v0/users", s.requireCapability(permission.UsersManage, s.handleListUsers))
	mux.HandleFunc("PATCH /api/v0/users/{id}/role", s.requireCSRF(s.requireCapability(permission.UsersManage, s.handleUpdateUserRole)))
	mux.HandleFunc("POST /api/v0/users/{id}/deactivate", s.requireCSRF(s.requireCapability(permission.UsersManage, s.handleDeactivateUser)))
	mux.HandleFunc("POST /api/v0/users/{id}/reactivate", s.requireCSRF(s.requireCapability(permission.UsersManage, s.handleReactivateUser)))

	// Content CRUD (slice 1.2). The literal /ping route above is more specific
	// than {type}, so ServeMux prefers it — no shadowing. Reads are public;
	// mutations require the content:write capability (slice 1.8).
	mux.HandleFunc("POST /api/v0/content/{type}", s.requireCSRF(s.requireCapability(permission.ContentWrite, s.handleContentCreate)))
	mux.HandleFunc("GET /api/v0/content/{type}", s.handleContentList)
	mux.HandleFunc("GET /api/v0/content/{type}/{id}", s.handleContentGet)
	mux.HandleFunc("PUT /api/v0/content/{type}/{id}", s.requireCSRF(s.requireCapability(permission.ContentWrite, s.handleContentUpdate)))
	mux.HandleFunc("DELETE /api/v0/content/{type}/{id}", s.requireCSRF(s.requireCapability(permission.ContentWrite, s.handleContentDelete)))

	// Drafts, publish, versioning (slice 1.5).
	mux.HandleFunc("POST /api/v0/content/{type}/{id}/publish", s.requireCSRF(s.requireCapability(permission.ContentPublish, s.handleContentPublish)))
	mux.HandleFunc("POST /api/v0/content/{type}/{id}/unpublish", s.requireCSRF(s.requireCapability(permission.ContentPublish, s.handleContentUnpublish)))
	mux.HandleFunc("GET /api/v0/content/{type}/{id}/versions", s.handleContentListVersions)
	mux.HandleFunc("POST /api/v0/content/{type}/{id}/rollback/{version}", s.requireCSRF(s.requireCapability(permission.ContentWrite, s.handleContentRollback)))

	// Media pipeline + library (slice 1.6). Reads are public; upload/delete
	// require media:write.
	mux.HandleFunc("POST /api/v0/media", s.requireCSRF(s.requireCapability(permission.MediaWrite, s.handleMediaUpload)))
	mux.HandleFunc("GET /api/v0/media", s.handleMediaList)
	mux.HandleFunc("GET /api/v0/media/{id}", s.handleMediaGet)
	mux.HandleFunc("GET /api/v0/media/{id}/file", s.handleMediaFile)
	mux.HandleFunc("PATCH /api/v0/media/{id}", s.requireCSRF(s.requireCapability(permission.MediaWrite, s.handleMediaUpdateMetadata)))
	mux.HandleFunc("DELETE /api/v0/media/{id}", s.requireCSRF(s.requireCapability(permission.MediaWrite, s.handleMediaDelete)))
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
