package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	capmarket "github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/marketplace"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/kernel"
	"github.com/glyphux/glyphux/pkg/packagefmt"
)

// WithMarketplace wires the marketplace surface (Ticket T8 / gap 8): the
// catalog read, the package install mutation, and the entitlement
// registration/listing routes. mp is the daemon's real, shared
// internal/marketplace.Manager — the resolved catalog (embedded sample +
// operator file), the entitlement registry over the real database, and the
// key-ID-aware trust set the install path verifies packages against.
//
// The whole surface is admin-only (plugins:manage), mirroring the plugin
// consent/audit surfaces — the catalog lists what the host would install
// and the mutations change the host's installed surface. Mutations get
// requireCSRF like every other state-changing route. Defense in depth: the
// install endpoint additionally hands the preset/bundle stores a
// Decode-verified package, and those stores' own PresetsManage gate is the
// second check (they return permission.ErrDenied if the route gate was ever
// bypassed).
//
// Omitting this option (the zero value) leaves the routes 404ing, mirroring
// the other opt-in transports.
func WithMarketplace(mp *marketplace.Manager) Option {
	return func(s *Server) { s.marketplace = mp }
}

// catalogEntryView is the wire shape of one catalog listing, annotated with
// core compatibility (vs kernel.Version) and update eligibility.
type catalogEntryView struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Kind           packagefmt.Kind `json:"kind"`
	Tier           string          `json:"tier"`
	Version        string          `json:"version"`
	RequiresCore   string          `json:"requires_core"`
	License        string          `json:"license"`
	Commercial     bool            `json:"commercial"`
	UpdateEligible bool            `json:"update_eligible"`
	CoreCompatible bool            `json:"core_compatible"`
	CoreStatus     string          `json:"core_status"`
	// EntitlementStatus is only present in the "entitlements" category:
	// active | expired | not_yet_valid | none.
	EntitlementStatus string `json:"entitlement_status,omitempty"`
}

// handleMarketplaceCatalog serves GET /api/v0/marketplace/catalog: the
// resolved catalog grouped under the four locked categories — free official
// plugins (capability-kind), free official themes (preset-kind), free
// community packages, and commercial packages (the "manually issued
// commercial entitlement tokens" category, annotated with each entry's
// entitlement status derived from the registered tokens). Core
// compatibility is annotated per entry against kernel.Version — the host's
// version, not anything the package declares.
func (s *Server) handleMarketplaceCatalog(w http.ResponseWriter, r *http.Request) {
	if s.marketplace == nil {
		s.writeError(w, http.StatusNotFound, "marketplace not configured")
		return
	}

	// Entitlement status by extension, computed once per catalog read. A
	// store error (e.g. migration not run) degrades to empty statuses —
	// the catalog listing is a read and must not fail wholesale.
	statusByExt := map[string]string{}
	if tokens, err := s.marketplace.Entitlements.List(r.Context()); err == nil {
		for _, tok := range tokens {
			ext := tok.Entitlement.ExtensionName
			if _, seen := statusByExt[ext]; !seen {
				statusByExt[ext] = s.marketplace.TokenStatus(tok, time.Now().UTC())
			}
		}
	} else {
		s.log.Warn("marketplace catalog: list entitlements", "error", err)
	}

	var plugins, themes, community, entitlements []catalogEntryView
	for _, e := range s.marketplace.Catalog.List() {
		compatible := capmarket.CoreConstraintSatisfied(e.RequiresCore, kernel.Version)
		coreStatus := "compatible"
		if !compatible {
			coreStatus = "too_new"
		}
		eligible := true
		if e.Commercial {
			eligible = statusByExt[e.ID] == "active"
		}
		view := catalogEntryView{
			ID: e.ID, Name: e.Name, Kind: e.Kind, Tier: e.Tier, Version: e.Version,
			RequiresCore: e.RequiresCore, License: e.License, Commercial: e.Commercial,
			UpdateEligible: eligible, CoreCompatible: compatible, CoreStatus: coreStatus,
		}
		// The four categories are view groupings, not partitions: every
		// entry appears in its kind/tier category, and a commercial entry
		// additionally appears in the "entitlements" category annotated
		// with its entitlement status.
		switch {
		case e.Tier == "official" && e.Kind == packagefmt.KindPlugin:
			plugins = append(plugins, view)
		case e.Tier == "official" && e.Kind == packagefmt.KindPreset:
			themes = append(themes, view)
		default:
			community = append(community, view)
		}
		if e.Commercial {
			view.EntitlementStatus = statusByExt[e.ID]
			if view.EntitlementStatus == "" {
				view.EntitlementStatus = "none"
			}
			entitlements = append(entitlements, view)
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"categories": map[string]any{
			"official_plugins": plugins,
			"official_themes":  themes,
			"community":        community,
			"entitlements":     entitlements,
		},
	})
}

// handleMarketplaceInstall serves POST /api/v0/marketplace/packages/{id}/install:
// the locked pipeline archive → packagefmt.Decode (verify) → InstallFromPackage,
// transactional at the store (a failed install persists nothing). Decode
// rejects a tampered or untrusted-key container before any store is
// touched; InstallFromPackage rejects a requires.core newer than this host.
// 422 for a bad container, a tampered package, or a too-new package; 404
// for an id not in the catalog or an unwired preset/bundle store.
func (s *Server) handleMarketplaceInstall(w http.ResponseWriter, r *http.Request) {
	if s.marketplace == nil {
		s.writeError(w, http.StatusNotFound, "marketplace not configured")
		return
	}
	entry, ok := s.marketplace.Catalog.ByID(r.PathValue("id"))
	if !ok {
		s.writeError(w, http.StatusNotFound, "package not in catalog")
		return
	}
	if len(entry.PackageBytes) == 0 {
		s.writeError(w, http.StatusUnprocessableEntity, "package has no installable artifact")
		return
	}
	sp, err := packagefmt.Decode(entry.PackageBytes, s.marketplace.Keys)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	principal := s.principal(r)
	switch entry.Kind {
	case packagefmt.KindPreset:
		if s.presets == nil {
			s.writeError(w, http.StatusNotFound, "presets not configured")
			return
		}
		rec, err := s.presets.InstallFromPackage(r.Context(), principal, *sp, kernel.Version)
		if err != nil {
			s.writeMarketplaceInstallError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"id": rec.ID})
	case packagefmt.KindBundle:
		if s.bundles == nil {
			s.writeError(w, http.StatusNotFound, "bundles not configured")
			return
		}
		rec, err := s.bundles.InstallFromPackage(r.Context(), principal, *sp, kernel.Version)
		if err != nil {
			s.writeMarketplaceInstallError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"id": rec.ID})
	default:
		// plugin-kind install is structurally out of T8 scope (plugins are
		// loaded from config, not installed); the format supports it, the
		// surface does not yet.
		s.writeError(w, http.StatusUnprocessableEntity, "plugin-kind install is not supported yet")
	}
}

// writeMarketplaceInstallError maps InstallFromPackage domain errors: a
// requires.core gate failure is 422 (the package demands a newer kernel —
// the RED-pinned case), a capability denial is 403 (defense in depth — the
// route gate should have caught it), anything else is a server error.
func (s *Server) writeMarketplaceInstallError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, permission.ErrDenied):
		s.writeError(w, http.StatusForbidden, "insufficient permissions")
	case errors.Is(err, preset.ErrRequiresCoreNotSatisfied), errors.Is(err, bundle.ErrRequiresCoreNotSatisfied):
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		s.log.Error("marketplace install", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// handleMarketplaceEntitlementsRegister serves POST
// /api/v0/marketplace/entitlements: body {"token": "<base64 of JSON
// EntitlementToken>"}. The token's signature is verified against the trust
// set before registration (a tampered or untrusted token is 422); a token
// that verifies is registered regardless of its validity window — an
// expired token is a normal lifecycle state reported at list time, and
// registration gates updates only, never first install (the locked
// decision). Duplicate license ids re-register (latest wins).
func (s *Server) handleMarketplaceEntitlementsRegister(w http.ResponseWriter, r *http.Request) {
	if s.marketplace == nil {
		s.writeError(w, http.StatusNotFound, "marketplace not configured")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Token == "" {
		s.writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(body.Token)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "token is not valid base64")
		return
	}
	var tok capmarket.EntitlementToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "token is not a valid entitlement token")
		return
	}
	if !s.verifyEntitlementSignature(tok) {
		s.writeError(w, http.StatusUnprocessableEntity, "entitlement token has an invalid signature")
		return
	}
	if err := s.marketplace.Entitlements.Register(r.Context(), tok); err != nil {
		s.log.Error("marketplace entitlements register", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// verifyEntitlementSignature reports whether tok's signature verifies under
// any key in the trust set. Purpose discrimination (package-signing vs
// entitlement-signing) is deferred per the locked T8 decision — every
// trusted key can verify tokens.
func (s *Server) verifyEntitlementSignature(tok capmarket.EntitlementToken) bool {
	for _, pub := range s.marketplace.Keys {
		if _, err := capmarket.VerifyEntitlement(pub, tok, time.Now().UTC()); err == nil ||
			errors.Is(err, capmarket.ErrEntitlementExpired) || errors.Is(err, capmarket.ErrEntitlementNotYetValid) {
			// A genuinely-signed token reports its window status; only an
			// invalid signature means "not signed by a trusted key".
			return true
		}
	}
	return false
}

// handleMarketplaceEntitlementsList serves GET /api/v0/marketplace/entitlements:
// every registered token with its computed status (active | expired |
// not_yet_valid | invalid), newest registration first. The status is
// recomputed at read time — a token's NotAfter crossing is reflected on the
// next read, no background job needed.
func (s *Server) handleMarketplaceEntitlementsList(w http.ResponseWriter, r *http.Request) {
	if s.marketplace == nil {
		s.writeError(w, http.StatusNotFound, "marketplace not configured")
		return
	}
	tokens, err := s.marketplace.Entitlements.List(r.Context())
	if err != nil {
		s.log.Error("marketplace entitlements list", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type tokenView struct {
		Extension string `json:"extension"`
		LicenseID string `json:"license_id"`
		Status    string `json:"status"`
	}
	views := make([]tokenView, 0, len(tokens))
	for _, tok := range tokens {
		views = append(views, tokenView{
			Extension: tok.Entitlement.ExtensionName,
			LicenseID: tok.Entitlement.LicenseID,
			Status:    s.marketplace.TokenStatus(tok, time.Now().UTC()),
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"tokens": views})
}
