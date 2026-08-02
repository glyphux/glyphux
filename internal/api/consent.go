package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Wire shapes for the consent surface (Ticket T4 / gap 2). The engine's
// own types (consent.Decision, sdk.APIScope, sdk.Permission) carry no JSON
// tags — they are kernel/domain types, not wire types — so every response
// here is built from these explicit wire structs instead of leaking Go
// field names onto the API.

type apiScopeWire struct {
	Capability string   `json:"capability"`
	Scopes     []string `json:"scopes"`
}

type permissionWire struct {
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
}

type grantWire struct {
	API         []apiScopeWire   `json:"api"`
	Permissions []permissionWire `json:"permissions"`
}

type pluginStatusWire struct {
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Fingerprint string     `json:"fingerprint"`
	Status      string     `json:"status"` // "approved" | "partial" | "denied" | "undecided"
	Requested   grantWire  `json:"requested"`
	Granted     *grantWire `json:"granted,omitempty"` // present only when a live decision exists
}

type consentRequestWire struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Fingerprint string           `json:"fingerprint"`
	API         []apiScopeWire   `json:"api"`
	Permissions []permissionWire `json:"permissions"`
}

type decisionWire struct {
	ID                 int64            `json:"id"`
	PluginName         string           `json:"plugin_name"`
	PluginVersion      string           `json:"plugin_version"`
	Fingerprint        string           `json:"fingerprint"`
	Status             string           `json:"status"`
	GrantedAPI         []apiScopeWire   `json:"granted_api"`
	GrantedPermissions []permissionWire `json:"granted_permissions"`
	DecidedBy          int64            `json:"decided_by"`
	DecidedAt          time.Time        `json:"decided_at"`
}

func toScopeWire(scopes []sdk.APIScope) []apiScopeWire {
	out := make([]apiScopeWire, len(scopes))
	for i, s := range scopes {
		out[i] = apiScopeWire{Capability: s.Capability, Scopes: append([]string(nil), s.Scopes...)}
	}
	return out
}

func toPermissionWire(perms []sdk.Permission) []permissionWire {
	out := make([]permissionWire, len(perms))
	for i, p := range perms {
		out[i] = permissionWire{Name: p.Name, Args: append([]string(nil), p.Args...)}
	}
	return out
}

func toSDKScopes(w []apiScopeWire) []sdk.APIScope {
	out := make([]sdk.APIScope, len(w))
	for i, s := range w {
		out[i] = sdk.APIScope{Capability: s.Capability, Scopes: append([]string(nil), s.Scopes...)}
	}
	return out
}

func toSDKPermissions(w []permissionWire) []sdk.Permission {
	out := make([]sdk.Permission, len(w))
	for i, p := range w {
		out[i] = sdk.Permission{Name: p.Name, Args: append([]string(nil), p.Args...)}
	}
	return out
}

func (s *Server) consentManifest(pluginName string) (sdk.Manifest, bool) {
	for _, m := range s.pluginManifests {
		if m.Name == pluginName {
			return m, true
		}
	}
	return sdk.Manifest{}, false
}

// handlePluginsList serves GET /api/v0/plugins: every registered plugin
// with its consent status — the full requested surface plus, when a live
// decision exists, exactly the granted subset.
func (s *Server) handlePluginsList(w http.ResponseWriter, r *http.Request) {
	if s.consent == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	out := make([]pluginStatusWire, 0, len(s.pluginManifests))
	for _, m := range s.pluginManifests {
		req, err := s.consent.Request(m)
		if err != nil {
			// A registered plugin with an invalid manifest is a boot-time
			// invariant violation; treat it as undecided rather than 500ing
			// the whole list.
			s.log.Error("consent: registered plugin has invalid manifest", "plugin", m.Name, "error", err)
			continue
		}
		item := pluginStatusWire{
			Name:        m.Name,
			Version:     m.Version,
			Fingerprint: string(req.Fingerprint),
			Status:      string(consent.StatusDenied), // placeholder; replaced below when live
			Requested: grantWire{
				API:         toScopeWire(req.API),
				Permissions: toPermissionWire(req.Permissions),
			},
		}
		dec, has, err := s.consent.Latest(r.Context(), m)
		if err != nil {
			s.log.Error("consent: Latest", "plugin", m.Name, "error", err)
			s.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Status is the most recent decision's status even when it is not
		// live: the consent screen must show "denied" (a decision exists,
		// grants nothing) distinctly from "undecided" (never asked).
		// Granted is populated only for a live (approved/partial) decision.
		if has {
			item.Status = string(dec.Status)
			if dec.Status != consent.StatusDenied {
				item.Granted = &grantWire{
					API:         toScopeWire(dec.GrantedAPI),
					Permissions: toPermissionWire(dec.GrantedPermissions),
				}
			}
		} else {
			item.Status = "undecided"
		}
		out = append(out, item)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"plugins": out})
}

// handleConsentRequestsList serves GET /api/v0/plugins/consent-requests:
// every registered plugin with NO live decision (undecided, denied, or a
// stale fingerprint) — i.e. what the consent screen must surface as
// pending, with the full permission request and fingerprint.
func (s *Server) handleConsentRequestsList(w http.ResponseWriter, r *http.Request) {
	if s.consent == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	out := make([]consentRequestWire, 0, len(s.pluginManifests))
	for _, m := range s.pluginManifests {
		_, live, err := s.consent.IsConsented(r.Context(), m)
		if err != nil {
			s.log.Error("consent: IsConsented", "plugin", m.Name, "error", err)
			s.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if live {
			continue
		}
		req, err := s.consent.Request(m)
		if err != nil {
			s.log.Error("consent: registered plugin has invalid manifest", "plugin", m.Name, "error", err)
			continue
		}
		out = append(out, consentRequestWire{
			Name:        req.PluginName,
			Version:     req.PluginVersion,
			Fingerprint: string(req.Fingerprint),
			API:         toScopeWire(req.API),
			Permissions: toPermissionWire(req.Permissions),
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// handleConsentDecide serves POST
// /api/v0/plugins/consent-requests/{plugin}/decide. The body is the
// admin's decision: a declared outcome (approved | partial | denied) plus
// the granted subset (granted_api / granted_permissions), which must be a
// subset of the plugin's request (422 ErrGrantExceedsRequest otherwise).
// DecidedBy is the acting admin's user ID.
func (s *Server) handleConsentDecide(w http.ResponseWriter, r *http.Request) {
	if s.consent == nil {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}
	pluginName := r.PathValue("plugin")
	m, ok := s.consentManifest(pluginName)
	if !ok {
		s.writeError(w, http.StatusNotFound, "unknown plugin")
		return
	}
	req, err := s.consent.Request(m)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body struct {
		Decision           string           `json:"decision"`
		GrantedAPI         []apiScopeWire   `json:"granted_api"`
		GrantedPermissions []permissionWire `json:"granted_permissions"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Decision != "approved" && body.Decision != "partial" && body.Decision != "denied" {
		s.writeError(w, http.StatusBadRequest, "decision must be approved, partial or denied")
		return
	}
	grantedAPI := toSDKScopes(body.GrantedAPI)
	grantedPerms := toSDKPermissions(body.GrantedPermissions)

	user, _ := userFrom(r.Context())
	var dec consent.Decision
	switch body.Decision {
	case "denied":
		if len(grantedAPI) > 0 || len(grantedPerms) > 0 {
			s.writeError(w, http.StatusUnprocessableEntity, "a denied decision must grant nothing")
			return
		}
		dec, err = s.consent.Deny(r.Context(), req, user.ID)
	case "approved":
		if len(grantedAPI) > 0 && !grantEqualsRequest(req, grantedAPI, grantedPerms) {
			s.writeError(w, http.StatusUnprocessableEntity, "an approved decision must grant the full request (or omit the grant)")
			return
		}
		dec, err = s.consent.Approve(r.Context(), req, user.ID)
	case "partial":
		if len(grantedAPI) == 0 && len(grantedPerms) == 0 {
			s.writeError(w, http.StatusUnprocessableEntity, "a partial decision must grant at least one scope or permission")
			return
		}
		dec, err = s.consent.Decide(r.Context(), req, grantedAPI, grantedPerms, user.ID)
	}
	if errors.Is(err, consent.ErrGrantExceedsRequest) {
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		s.log.Error("consent: decide", "plugin", pluginName, "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, decisionWire{
		ID:                 dec.ID,
		PluginName:         dec.PluginName,
		PluginVersion:      dec.PluginVersion,
		Fingerprint:        string(dec.Fingerprint),
		Status:             string(dec.Status),
		GrantedAPI:         toScopeWire(dec.GrantedAPI),
		GrantedPermissions: toPermissionWire(dec.GrantedPermissions),
		DecidedBy:          dec.DecidedBy,
		DecidedAt:          dec.DecidedAt,
	})
}

// grantEqualsRequest reports whether the granted subset is exactly the
// full request — the "approved" contract (capability/scope and
// permission/arg exact match, order-insensitive).
func grantEqualsRequest(req consent.ConsentRequest, grantedAPI []sdk.APIScope, grantedPerms []sdk.Permission) bool {
	if len(grantedAPI) != len(req.API) || len(grantedPerms) != len(req.Permissions) {
		return false
	}
	for _, g := range grantedAPI {
		if !containsScope(req.API, g) {
			return false
		}
	}
	for _, g := range grantedPerms {
		if !containsPermission(req.Permissions, g) {
			return false
		}
	}
	return true
}

func containsScope(scopes []sdk.APIScope, want sdk.APIScope) bool {
	for _, s := range scopes {
		if s.Capability != want.Capability || len(s.Scopes) != len(want.Scopes) {
			continue
		}
		matched := true
		for _, sc := range want.Scopes {
			found := false
			for _, have := range s.Scopes {
				if have == sc {
					found = true
					break
				}
			}
			if !found {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func containsPermission(perms []sdk.Permission, want sdk.Permission) bool {
	for _, p := range perms {
		if p.Name != want.Name || len(p.Args) != len(want.Args) {
			continue
		}
		matched := true
		for _, a := range want.Args {
			found := false
			for _, have := range p.Args {
				if have == a {
					found = true
					break
				}
			}
			if !found {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
