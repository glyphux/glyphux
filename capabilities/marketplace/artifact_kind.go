package marketplace

// ArtifactKind discriminates what a Package's Artifact bytes decode to (PRD
// §13.4: "all four distributable artifacts flow through the one signed-
// package marketplace" — block plugins, composition presets, composition
// bundles, themes). Added by Ticket P4.7; every Package predating this field
// implicitly meant KindPlugin (the zero value "" is treated identically to
// KindPlugin everywhere this package branches on Kind).
//
// Only KindPreset and KindBundle are exercised by real, tested code in this
// slice (composition.go's PackagePreset/PackageBundle/DecodePreset/
// DecodeBundle, plus internal/preset and internal/bundle's InstallFromPackage
// methods). KindPlugin, KindTheme, and KindBlock are declared here as the
// documented shape a future slice would fill in, NOT wired to any
// packaging/decoding helper of their own — see Package's own doc comment and
// this repo's tracking doc (docs/implementation/{active,completed}/
// 0032-phase4-slice4.7-marketplace-builder-artifacts.md) for the full
// investigation of why blocks/themes stop at "structurally supported" rather
// than "implemented" in this ticket:
//
//   - A pkg/blocks.Definition is pure data (a prop schema + slot names, no
//     code) — so packaging a *definition* as a marketplace artifact would be
//     as straightforward as a preset/bundle. But a block's actual runtime
//     behavior (what it does with those props, how a theme renders it) is
//     NOT expressed by Definition at all in this codebase; first-party
//     blocks (blocks/firstparty) are compiled-in Go with no plugin-loading
//     path, and pkg/sdk.HostAPI.RegisterBlock is only ever called by
//     in-process (Tier A) or already-loaded WASM/RPC (Tier B/C) plugin code
//     — there is no existing "install a block plugin from a downloaded
//     signed package" runtime path for this slice to wire KindBlock through.
//     That would mean building new WASM/RPC plugin-installation
//     infrastructure (fetch a package, load its module into a
//     *wasm.Runtime or spawn its RPC process, call RegisterBlock from
//     inside it), which is a plugin-runtime-lifecycle project of its own,
//     not an additive extension of this ticket's packaging/compatibility
//     scope.
//   - A pkg/theme.Theme is likewise pure Go code implementing an interface
//     (themes/starter, themes/headless) with no serialized "theme document"
//     analogous to a CompositionPreset — there is nothing for KindTheme's
//     Artifact to BE today other than "a compiled plugin module," which
//     collapses to the same WASM/RPC plugin-loading gap KindBlock has.
//
// Both gaps are the same one gap: this repo's WASM/RPC runtime
// (pkg/runtime/wasm, pkg/runtime/rpc) hosts and executes an already-loaded
// plugin; nothing in this repo today resolves "signed package bytes on
// disk" into "a loaded, running plugin instance" for ANY kind, including
// the plugin/theme kind KindPlugin nominally already covers. Building that
// resolution step is real, separate infrastructure work, so KindPlugin,
// KindTheme, and KindBlock are left as documented, valid enum values (a
// manifest/package CAN declare them, Sign/Verify handle them identically to
// any other Kind) without a packaging/decoding helper — exactly the
// "structurally-supported-but-not-yet-exercised extension point" framing
// this ticket's own scope investigation called for.
type ArtifactKind string

const (
	// KindPlugin is a block plugin or theme shipped as a compiled WASM
	// module or RPC plugin binary — the original (pre-P4.7) Package shape,
	// governed by Manifest (sdk.Manifest). Not exercised end-to-end by any
	// packaging/decoding helper in this slice; see this type's doc comment.
	KindPlugin ArtifactKind = "plugin"
	// KindTheme is a theme distributed as a marketplace artifact. Declared
	// for forward compatibility only — no packaging/decoding helper exists
	// for it in this slice; see this type's doc comment for why.
	KindTheme ArtifactKind = "theme"
	// KindBlock is a block plugin distributed as a marketplace artifact,
	// distinct from KindPlugin only in registry-namespace intent (a "block"
	// listing vs. a generic "plugin" listing) — both ultimately need the
	// same not-yet-built plugin-loading resolution step. Declared for
	// forward compatibility only; see this type's doc comment.
	KindBlock ArtifactKind = "block"
	// KindPreset is a pkg/contract.CompositionPreset document, JSON-encoded
	// into Package.Artifact by PackagePreset and decoded back by
	// DecodePreset (composition.go). Real, tested, exercised end-to-end by
	// internal/preset.Store.InstallFromPackage.
	KindPreset ArtifactKind = "preset"
	// KindBundle is a pkg/contract.CompositionBundle document, JSON-encoded
	// into Package.Artifact by PackageBundle and decoded back by
	// DecodeBundle (composition.go). Real, tested, exercised end-to-end by
	// internal/bundle.Store.InstallFromPackage.
	KindBundle ArtifactKind = "bundle"
)
