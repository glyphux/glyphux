// Package consent is the kernel's install-time consent engine (PRD §10.2
// mechanism #2, §10.1 adversarial-by-default): the trust surface between a
// plugin's declared manifest (pkg/sdk.Manifest — mechanism #3's input) and
// the boundary enforcement that the WASM host and RPC broker perform at
// call time (slices 2.4/2.5). This package answers exactly one question for
// a future boundary caller: "has an admin consented to exactly this
// manifest's declared scopes, and if only partially, to which subset?"
//
// This is a kernel concern (internal/consent), not a plugin-facing SDK
// surface — plugins never call this package themselves; only the kernel's
// own admin-ui/API layer (producing a ConsentRequest for a human to decide)
// and the future WASM/RPC load path (calling IsConsented) do.
//
// Out of scope for this slice (see docs/implementation/active/
// 0017-phase2-slice2.7-consent-engine.md for the full account): rendering an
// actual consent-screen UI, wiring IsConsented into the WASM/RPC plugin-load
// path, and marketplace review (mechanism #1).
package consent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Status is the outcome of an admin's decision on a ConsentRequest.
type Status string

const (
	// StatusApproved means every requested API scope and permission was
	// granted.
	StatusApproved Status = "approved"
	// StatusPartial means some, but not all, requested scopes were granted
	// (see this slice's tracking doc for why partial consent is supported
	// rather than an all-or-nothing decision).
	StatusPartial Status = "partial"
	// StatusDenied means nothing was granted.
	StatusDenied Status = "denied"
)

// Fingerprint is a stable hash of a manifest's two consent-relevant axes —
// API and Permissions — used to detect when a plugin's declared scopes have
// changed since a prior consent was granted (PRD §10.1: "everything else
// literally does not exist to the plugin" — a stale consent silently
// covering a NEW, unreviewed scope would be a security hole). Runtime,
// Requires, and any other manifest field are deliberately excluded: they
// don't change what an admin is being asked to trust, so changing them
// alone does not require re-consent.
type Fingerprint string

// fingerprintOf canonicalizes api and permissions (sorted, so declaration
// order never affects the hash) and returns a hex-encoded SHA-256 digest of
// their JSON encoding.
func fingerprintOf(api []sdk.APIScope, permissions []sdk.Permission) Fingerprint {
	type canonScope struct {
		Capability string
		Scopes     []string
	}
	type canonPermission struct {
		Name string
		Args []string
	}

	cAPI := make([]canonScope, len(api))
	for i, s := range api {
		scopes := append([]string(nil), s.Scopes...)
		sort.Strings(scopes)
		cAPI[i] = canonScope{Capability: s.Capability, Scopes: scopes}
	}
	sort.Slice(cAPI, func(i, j int) bool { return cAPI[i].Capability < cAPI[j].Capability })

	cPerm := make([]canonPermission, len(permissions))
	for i, p := range permissions {
		args := append([]string(nil), p.Args...)
		sort.Strings(args)
		cPerm[i] = canonPermission{Name: p.Name, Args: args}
	}
	sort.Slice(cPerm, func(i, j int) bool { return cPerm[i].Name < cPerm[j].Name })

	// Marshal errors are impossible here (the inputs are plain strings and
	// slices of them), so this deliberately does not propagate an error.
	payload, _ := json.Marshal(struct {
		API         []canonScope
		Permissions []canonPermission
	}{cAPI, cPerm})

	sum := sha256.Sum256(payload)
	return Fingerprint(hex.EncodeToString(sum[:]))
}

// ConsentRequest is the structured "here is what this plugin wants" data a
// future consent-screen UI renders (PRD §10.2's example: "commerce
// requests: content[read,write], payments[charge,refund], admin UI,
// network→api.stripe.com — Allow?"). It is data, not rendered HTML —
// rendering is a future admin-ui slice's job, not this kernel API's.
type ConsentRequest struct {
	PluginName    string
	PluginVersion string
	// Fingerprint identifies the exact API+Permissions shape this request
	// covers. A Decide call against this request records a decision keyed
	// to this fingerprint; a later manifest with a different fingerprint
	// (even at the same PluginVersion) will not match it.
	Fingerprint Fingerprint
	API         []sdk.APIScope
	Permissions []sdk.Permission
}

// Decision is a persisted, audited record of an admin's answer to a
// ConsentRequest (PRD §10.5: "every sensitive grant... is recorded"). It
// carries both what was requested and what was actually granted, so a
// partial grant's shortfall is itself part of the audit trail, not just the
// end state.
type Decision struct {
	ID            int64
	PluginName    string
	PluginVersion string
	Fingerprint   Fingerprint
	Status        Status

	RequestedAPI         []sdk.APIScope
	RequestedPermissions []sdk.Permission
	GrantedAPI           []sdk.APIScope
	GrantedPermissions   []sdk.Permission

	// DecidedBy is the identity.User.ID of the admin who made this
	// decision. internal/permission.Principal deliberately carries only a
	// Role (see its doc comment) and internal/audit does not exist yet, so
	// this reuses internal/identity's own int64 user-ID convention (the
	// same type identity.User.ID and identity's Sessions.UserID already
	// use) rather than inventing a new identity concept.
	DecidedBy int64
	DecidedAt time.Time
}

// ErrInvalidManifest is returned by Request when the manifest itself is
// malformed (delegates to sdk.Manifest.Validate).
var ErrInvalidManifest = errors.New("consent: invalid manifest")

// ErrGrantExceedsRequest is returned by Decide when the granted API scopes
// or permissions are not a subset of what the ConsentRequest actually
// asked for. An admin can approve less than what was requested (see this
// slice's tracking doc on partial consent) but never more — granting a
// capability or scope nobody declared would silently exceed the manifest
// axis, which is exactly the raw-resource/undeclared-surface hole §10.1 and
// §10.4 rule out.
var ErrGrantExceedsRequest = errors.New("consent: granted scopes exceed the request")

// Engine is the install-time consent engine: it turns a manifest into a
// ConsentRequest, records an admin's decision, and answers whether a given
// manifest shape currently has a live (non-denied) decision on file.
type Engine struct {
	db *db.DB
}

// NewEngine wires the consent engine to the database abstraction. Callers
// must have already run Migrations (via db.Migrate) against database.
func NewEngine(database *db.DB) *Engine {
	return &Engine{db: database}
}

// Request validates m and produces the ConsentRequest a consent-screen UI
// would present for it. It does not touch the database — Request is a pure
// function of the manifest (a Decide call is what persists anything).
func (e *Engine) Request(m sdk.Manifest) (ConsentRequest, error) {
	if err := m.Validate(); err != nil {
		return ConsentRequest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	return ConsentRequest{
		PluginName:    m.Name,
		PluginVersion: m.Version,
		Fingerprint:   fingerprintOf(m.API, m.Permissions),
		API:           append([]sdk.APIScope(nil), m.API...),
		Permissions:   append([]sdk.Permission(nil), m.Permissions...),
	}, nil
}

// Approve grants req in full — every requested API scope and permission —
// on behalf of decidedBy.
func (e *Engine) Approve(ctx context.Context, req ConsentRequest, decidedBy int64) (Decision, error) {
	return e.Decide(ctx, req, req.API, req.Permissions, decidedBy)
}

// Deny grants nothing from req, on behalf of decidedBy.
func (e *Engine) Deny(ctx context.Context, req ConsentRequest, decidedBy int64) (Decision, error) {
	return e.Decide(ctx, req, nil, nil, decidedBy)
}

// Decide records an admin's decision on req: grantedAPI and
// grantedPermissions are the subset of req's declared scopes the admin
// actually allows (see ADR note in this slice's tracking doc: partial
// consent, per-capability and per-permission, is supported — this is NOT
// forced to be all-or-nothing). grantedAPI/grantedPermissions must each be a
// subset of req's own API/Permissions (checked per capability/permission
// name AND per scope/arg within it) — ErrGrantExceedsRequest otherwise.
//
// The resulting Decision's Status is StatusApproved if the grant equals the
// full request, StatusDenied if the grant is empty, and StatusPartial
// otherwise.
func (e *Engine) Decide(ctx context.Context, req ConsentRequest, grantedAPI []sdk.APIScope, grantedPermissions []sdk.Permission, decidedBy int64) (Decision, error) {
	if err := validateAPISubset(req.API, grantedAPI); err != nil {
		return Decision{}, err
	}
	if err := validatePermissionSubset(req.Permissions, grantedPermissions); err != nil {
		return Decision{}, err
	}

	d := Decision{
		PluginName:           req.PluginName,
		PluginVersion:        req.PluginVersion,
		Fingerprint:          req.Fingerprint,
		Status:               statusFor(req, grantedAPI, grantedPermissions),
		RequestedAPI:         req.API,
		RequestedPermissions: req.Permissions,
		GrantedAPI:           grantedAPI,
		GrantedPermissions:   grantedPermissions,
		DecidedBy:            decidedBy,
		DecidedAt:            time.Now().UTC(),
	}

	id, err := insert(ctx, e.db, d)
	if err != nil {
		return Decision{}, err
	}
	d.ID = id
	return d, nil
}

// IsConsented reports whether m's exact declared shape (name, version, and
// API+Permissions fingerprint) currently has a live decision on file: ok is
// true if the most recent matching decision is StatusApproved or
// StatusPartial (something was granted), and false if no decision exists
// for this exact shape at all, or the most recent one is StatusDenied. The
// returned Decision's GrantedAPI/GrantedPermissions is exactly what a
// boundary enforcement point should expose — the full request when
// StatusApproved, a strict subset when StatusPartial.
//
// Because the lookup includes m's fingerprint, a manifest whose API or
// Permissions axis changed since the decision was recorded — even at an
// unchanged PluginVersion — never matches a stale decision: it must be
// re-requested and re-decided.
func (e *Engine) IsConsented(ctx context.Context, m sdk.Manifest) (Decision, bool, error) {
	fp := fingerprintOf(m.API, m.Permissions)
	d, err := latest(ctx, e.db, m.Name, m.Version, fp)
	if errors.Is(err, ErrNotFound) {
		return Decision{}, false, nil
	}
	if err != nil {
		return Decision{}, false, err
	}
	if d.Status == StatusDenied {
		return d, false, nil
	}
	return d, true, nil
}

// statusFor classifies a grant relative to the full request: Approved if it
// equals the request, Denied if it grants nothing, Partial otherwise.
func statusFor(req ConsentRequest, grantedAPI []sdk.APIScope, grantedPermissions []sdk.Permission) Status {
	if len(grantedAPI) == 0 && len(grantedPermissions) == 0 {
		return StatusDenied
	}
	if apiScopeCount(grantedAPI) == apiScopeCount(req.API) && len(grantedPermissions) == len(req.Permissions) {
		return StatusApproved
	}
	return StatusPartial
}

// apiScopeCount counts total (capability, scope) pairs across scopes, so a
// grant that drops even one scope from one capability (while keeping every
// other capability/scope) is correctly detected as partial, not approved.
func apiScopeCount(scopes []sdk.APIScope) int {
	n := 0
	for _, s := range scopes {
		n += len(s.Scopes)
	}
	return n
}

// validateAPISubset reports an error unless every capability and scope in
// granted also appears in requested.
func validateAPISubset(requested, granted []sdk.APIScope) error {
	allowed := make(map[string]map[string]bool, len(requested))
	for _, s := range requested {
		scopes := make(map[string]bool, len(s.Scopes))
		for _, sc := range s.Scopes {
			scopes[sc] = true
		}
		allowed[s.Capability] = scopes
	}
	for _, g := range granted {
		scopes, ok := allowed[g.Capability]
		if !ok {
			return fmt.Errorf("%w: capability %q was not requested", ErrGrantExceedsRequest, g.Capability)
		}
		for _, sc := range g.Scopes {
			if !scopes[sc] {
				return fmt.Errorf("%w: scope %q on capability %q was not requested", ErrGrantExceedsRequest, sc, g.Capability)
			}
		}
	}
	return nil
}

// validatePermissionSubset reports an error unless every permission and arg
// in granted also appears in requested.
func validatePermissionSubset(requested, granted []sdk.Permission) error {
	allowed := make(map[string]map[string]bool, len(requested))
	for _, p := range requested {
		args := make(map[string]bool, len(p.Args))
		for _, a := range p.Args {
			args[a] = true
		}
		allowed[p.Name] = args
	}
	for _, g := range granted {
		args, ok := allowed[g.Name]
		if !ok {
			return fmt.Errorf("%w: permission %q was not requested", ErrGrantExceedsRequest, g.Name)
		}
		for _, a := range g.Args {
			if !args[a] {
				return fmt.Errorf("%w: arg %q on permission %q was not requested", ErrGrantExceedsRequest, a, g.Name)
			}
		}
	}
	return nil
}
