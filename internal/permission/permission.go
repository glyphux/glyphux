// Package permission is the kernel permission engine (§ Phase 1 slice 1.8).
// v1 maps a principal's role to a fixed set of capabilities; the tenant
// dimension is a no-op until multi-tenancy lands.
//
// Per PRD §10.5 ("the engine enforces at the domain-API boundary"), domain
// packages (internal/content, internal/composition, internal/media) import
// this package and check Allows/AllowsPrincipal themselves — they do not
// trust that a transport handler already checked. Transport handlers
// (internal/api, internal/graphql) also keep their own fast-fail checks for
// good UX (reject before even calling the domain layer); both layers check,
// per §10.2's "Three Points of Consent and Enforcement" (defense-in-depth).
package permission

import "errors"

// Capability is a scoped action a principal may be granted.
type Capability string

const (
	ContentRead        Capability = "content:read"
	ContentReadDrafts  Capability = "content:read_drafts"
	ContentWrite       Capability = "content:write"
	ContentPublish     Capability = "content:publish"
	ContentTypesManage Capability = "content-types:manage"
	MediaWrite         Capability = "media:write"
	UsersManage        Capability = "users:manage"
)

// Roles known to v1's fixed capability matrix.
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// roleCapabilities maps roles to the capabilities they hold.
var roleCapabilities = map[string]map[Capability]bool{
	RoleAdmin: {
		ContentRead: true, ContentReadDrafts: true, ContentWrite: true,
		ContentPublish: true, ContentTypesManage: true, MediaWrite: true, UsersManage: true,
	},
	RoleEditor: {
		ContentRead: true, ContentReadDrafts: true, ContentWrite: true, MediaWrite: true,
	},
	RoleViewer: {
		ContentRead: true,
	},
}

// Allows reports whether role holds capability. Unknown roles (including the
// empty string, which every anonymous/unauthenticated caller resolves to)
// hold nothing.
func Allows(role string, capability Capability) bool {
	return roleCapabilities[role][capability]
}

// ValidRole reports whether role is one v1's fixed matrix recognizes.
func ValidRole(role string) bool {
	_, ok := roleCapabilities[role]
	return ok
}

// Principal is the caller identity domain APIs check capability grants
// against. It is deliberately a narrow view of an authenticated identity —
// just the role — so domain packages (internal/content, internal/composition,
// internal/media) never need to import internal/identity to enforce
// capabilities at their own boundary (layering: content shouldn't need to
// know identity's full shape). A nil *Principal represents an anonymous,
// unauthenticated caller.
type Principal struct {
	Role string
}

// RoleOf returns p's role, or "" (which holds no capabilities) for a nil,
// anonymous principal.
func RoleOf(p *Principal) string {
	if p == nil {
		return ""
	}
	return p.Role
}

// AllowsPrincipal reports whether p (nil meaning an anonymous caller) holds
// capability.
func AllowsPrincipal(p *Principal, capability Capability) bool {
	return Allows(RoleOf(p), capability)
}

// ErrDenied is returned by a domain API when the calling principal does not
// hold the capability an operation requires. Transports map it to 403
// (Forbidden) — 401 (Unauthenticated) is still produced by the transport's
// own faster pre-check for a request with no principal at all.
var ErrDenied = errors.New("permission denied")
