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
	ContentRead       Capability = "content:read"
	ContentReadDrafts Capability = "content:read_drafts"
	ContentWrite      Capability = "content:write"
	ContentPublish    Capability = "content:publish"
	MediaWrite        Capability = "media:write"
	UsersManage       Capability = "users:manage"
	// ContentTypesManage gates defining, updating, and deleting content
	// types themselves (as opposed to content:write, which gates items of
	// an already-declared type) — a structural, site-wide schema change,
	// held only by admin.
	ContentTypesManage Capability = "content_types:manage"
	// LayoutsManage gates writing a Layer-2 Layout document (pkg/contract.
	// Layout) for a route — arranging blocks into a template's regions
	// (PRD §14 slice 4.4a). This is a new, distinct capability rather than a
	// reuse of ContentTypesManage or ContentWrite: a Layout is neither
	// Layer-1 schema (content types) nor a Layer-1 content item — it is
	// Layer 2's own structural document ("additive to Layer 1, never
	// polluting it"), so it gets its own capability rather than overloading
	// an existing one whose name and existing call sites mean something
	// else. Held only by admin, for the same "structural, site-wide change"
	// reason ContentTypesManage is admin-only.
	LayoutsManage Capability = "layouts:manage"
	// PresetsManage gates saving/importing Composition Presets and
	// Composition Bundles (pkg/contract.CompositionPreset/CompositionBundle,
	// PRD §13.2/§13.6 ticket P4.6) — both artifacts are, like a Layout,
	// Layer-2-adjacent structural documents rather than Layer-1 content
	// items, so they get their own capability rather than reusing
	// ContentWrite. Bundled into one capability (not split into
	// PresetsManage/BundlesManage) because the two artifacts share the same
	// "arrange composition at site scale" concern and, in this v1 role
	// matrix, always the same admin-only holder — splitting them would add
	// a second capability name with an identical grant set and no call site
	// that ever checks one without the other. Held only by admin, matching
	// LayoutsManage's "structural, site-wide change" precedent — importing
	// a preset/bundle can rewrite an arbitrary route's Layout, the same
	// blast radius layouts:manage already gates.
	PresetsManage Capability = "presets:manage"
	// PluginsManage gates the plugin consent surface (Ticket T4 / gap 2):
	// listing plugins, reading pending consent requests, and — most
	// importantly — making install-time consent decisions, each of which
	// changes the trust boundary of the whole site (what a plugin may do
	// beyond its declared manifest). Held only by admin, for the same
	// "structural, site-wide change" reason ContentTypesManage is
	// admin-only. PRD §10.2's consent flow is an admin act by definition.
	PluginsManage Capability = "plugins:manage"
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
		ContentPublish: true, MediaWrite: true, UsersManage: true,
		ContentTypesManage: true, LayoutsManage: true, PresetsManage: true,
		PluginsManage: true,
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
