package sdk

// FilterManifest narrows a plugin's declared Manifest to the subset of
// permissions an admin actually granted, producing the manifest a boundary
// enforcement point (the WASM host, the RPC broker — Ticket T3) should build
// the plugin's HostAPI from. It is the missing link between
// internal/consent's decision record and pkg/sdk's decision primitive: the
// grant is where network access is decided, FilterManifest is where that
// decision becomes the plugin's only view of the world.
//
// granted is exactly what internal/consent's Engine.IsConsented returns in
// Decision.GrantedPermissions ([]sdk.Permission) — pass it straight
// through; pkg/sdk deliberately does not import internal/consent (internal
// packages may import pkg/sdk, never the other way), so the filter takes
// the plain permission slice instead of the Decision itself.
//
// Semantics (unchanged from Manifest.AllowsNetworkHost, deny-by-default,
// exact-match, case-insensitive):
//
//   - granted wins over declared: a permission survives only if consent
//     granted it, and with exactly the granted Args (for "network" that is
//     the granted host allowlist — a host the admin did not grant is not
//     in the filtered manifest no matter what the plugin declared).
//
//   - the API axis is narrowed identically: a capability survives only if
//     consent granted it, and with exactly the granted scopes (a plugin
//     granted content:[read] of a declared content:[read,write] gets a
//     host whose hasScope gates answer only for content:read).
//
//   - a permission consent dropped entirely is absent from the filtered
//     manifest, so its decision primitive answers deny-by-default.
//
//   - a granted "network" permission with an EMPTY Args list is dropped too
//     (an empty allowlist grant means "allow nothing"; a zero-host network
//     permission would also fail Manifest.Validate, and the filtered
//     manifest must remain valid).
//
//   - a granted permission the manifest never declared is never added (the
//     filter cannot invent surface; consent itself rejects such grants).
//
// The filtered manifest always satisfies Manifest.Validate when m does, so
// it can be fed straight to NewHostAPI.
func FilterManifest(m Manifest, granted []Permission, grantedAPI []APIScope) Manifest {
	grantedByName := make(map[string]Permission, len(granted))
	for _, g := range granted {
		grantedByName[g.Name] = g
	}

	filtered := m
	// Fresh backing array: the filtered manifest must never alias (and
	// thereby mutate) the caller's declared Permissions.
	filtered.Permissions = make([]Permission, 0, len(granted))
	for _, declared := range m.Permissions {
		g, ok := grantedByName[declared.Name]
		if !ok {
			continue // consent dropped this permission — denied, absent.
		}
		// A zero-host network grant is "allow nothing": drop the permission
		// rather than emit an invalid empty allowlist (Validate rejects a
		// "network" permission without at least one host).
		if declared.Name == "network" && len(g.Args) == 0 {
			continue
		}
		filtered.Permissions = append(filtered.Permissions, g)
	}
	return filtered
}
