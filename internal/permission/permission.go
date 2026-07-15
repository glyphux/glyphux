// Package permission is the kernel permission engine (§ Phase 1 slice 1.8).
// v1 maps a principal's role to a fixed set of capabilities; the tenant
// dimension is a no-op until multi-tenancy lands.
package permission

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
		ContentTypesManage: true,
	},
	RoleEditor: {
		ContentRead: true, ContentReadDrafts: true, ContentWrite: true, MediaWrite: true,
	},
	RoleViewer: {
		ContentRead: true,
	},
}

// Allows reports whether role holds capability. Unknown roles hold nothing.
func Allows(role string, capability Capability) bool {
	return roleCapabilities[role][capability]
}

// ValidRole reports whether role is one v1's fixed matrix recognizes.
func ValidRole(role string) bool {
	_, ok := roleCapabilities[role]
	return ok
}
