// Package permission is the kernel permission engine (§ Phase 1 slice 1.8).
// v1 maps a principal's role to a fixed set of capabilities; the tenant
// dimension is a no-op until multi-tenancy lands.
package permission

// Capability is a scoped action a principal may be granted.
type Capability string

const (
	ContentRead  Capability = "content:read"
	ContentWrite Capability = "content:write"
)

// roleCapabilities maps roles to the capabilities they hold.
var roleCapabilities = map[string]map[Capability]bool{
	"admin": {ContentRead: true, ContentWrite: true},
}

// Allows reports whether role holds capability. Unknown roles hold nothing.
func Allows(role string, capability Capability) bool {
	return roleCapabilities[role][capability]
}
