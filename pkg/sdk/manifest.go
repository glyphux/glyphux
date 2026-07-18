// Package sdk is the public extension contract (PRD §8): the Plugin/HostAPI
// surface every plugin tier speaks, regardless of runtime (in-process Tier
// A, WASM Tier B, RPC Tier C). This is the only surface a plugin can reach —
// no plugin/theme code imports kernel internals (§5's layering rule).
package sdk

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// RuntimeTier selects which loader/isolation model a plugin runs under
// (PRD §8.1). All three speak the same unified extension contract; only
// the transport and isolation differ.
type RuntimeTier string

const (
	// RuntimeInProcess is Tier A: compiled into the binary, full trust.
	// First-party/official only — never granted to a third-party plugin.
	RuntimeInProcess RuntimeTier = "inprocess"
	// RuntimeWASM is Tier B: Wazero-hosted, deny-by-default sandbox — the
	// primary third-party tier.
	RuntimeWASM RuntimeTier = "wasm"
	// RuntimeRPC is Tier C: out-of-process via gRPC, for large/trusted
	// system-level plugins with their own dependencies and datastore.
	RuntimeRPC RuntimeTier = "rpc"
)

var validRuntimeTiers = map[RuntimeTier]bool{
	RuntimeInProcess: true,
	RuntimeWASM:      true,
	RuntimeRPC:       true,
}

// Manifest is a plugin's declared identity, requirements, and the two
// distinct axes of what it may touch (PRD §7.3): API (domain surfaces,
// routine) and Permissions (resource grants, scrutinized). The two axes are
// kept as separate fields, not merged into one undifferentiated list,
// because they carry different trust models and review criteria.
type Manifest struct {
	Name     string
	Version  string
	Runtime  RuntimeTier
	Requires    Requires
	API         []APIScope
	Permissions []Permission
}

// Permission is a resource grant (PRD §7.3) — scrutinized and risk-graded,
// unlike the API axis's routine domain-surface declarations. Raw
// database/filesystem access is deliberately absent from knownPermissions:
// it is near-forbidden for third-party plugins (§10.4) and not grantable
// through this axis at all.
type Permission struct {
	Name string
	// Args carries the permission's own parameters — currently only
	// "network" uses it, for its allowlisted outbound domains.
	Args []string
}

var knownPermissions = map[string]bool{
	"admin_ui":       true,
	"scheduled_jobs": true,
	"network":        true,
}

// APIScope declares a domain surface a plugin speaks to and the scopes it
// needs on it (PRD §7.3, §7.4 — capabilities are scoped, not boolean).
type APIScope struct {
	Capability string
	Scopes     []string
}

// knownAPIScopes is the fixed v1 set of domain-surface capabilities a
// plugin may declare in its API axis, and the scopes each recognizes.
// Distinct from internal/permission.Capability (a per-user-role grant on
// the kernel's own transport) — this gates what surface a plugin PACKAGE
// gets at all, independent of which end-user role is acting through it.
var knownAPIScopes = map[string]map[string]bool{
	"content": {"read": true, "write": true, "publish": true},
	"users":   {"read": true, "manage": true},
	"media":   {"read": true, "write": true},
	"events":  {"emit": true, "subscribe": true},
	// payments and membership do not yet back a real HostAPI method surface
	// (commerce/membership are Phase 3 slices 3.3/3.4, PRD §14) — they exist
	// here only so a manifest has a capability to declare in order to
	// subscribe to the corresponding sensitive domain events (§8.4), per
	// slice 2.2's tracking doc. "payments" scopes match the PRD's own §7.4
	// example (`payments: [charge, refund]`) verbatim; "membership" has no
	// PRD example scope, so "manage" is this slice's placeholder guess,
	// expected to be revisited when slice 3.4 defines a real MembershipAPI.
	"payments":   {"charge": true, "refund": true},
	"membership": {"manage": true},
}

// Requires declares the kernel/contract versions a plugin needs (PRD §7.3).
// Core is a comparison-operator-prefixed semver constraint (">=1.0.0"); an
// unprefixed version means exact match. Contract is a contract version
// string a plugin was built against (e.g. "content-composition/v0") —
// checked against a known version by the capability registry (slice 2.3);
// this only checks the string isn't empty.
type Requires struct {
	Core     string
	Contract string
}

// coreConstraintOperators are the comparison prefixes a requires.core
// constraint may start with; no prefix means exact match.
var coreConstraintOperators = []string{">=", "<=", "==", ">", "<"}

func validCoreConstraint(c string) bool {
	for _, op := range coreConstraintOperators {
		if rest, ok := strings.CutPrefix(c, op); ok {
			return semver.IsValid(canonicalSemver(rest))
		}
	}
	return semver.IsValid(canonicalSemver(c))
}

// coreSatisfied reports whether kernelVersion satisfies constraint (a
// requires.core string already known to be well-formed per
// validCoreConstraint — this is NewHostAPI's runtime enforcement step,
// checked in addition to, and after, Validate's shape check). An
// unprefixed constraint means exact version match, matching the doc comment
// on Requires.Core.
func coreSatisfied(constraint, kernelVersion string) bool {
	kv := canonicalSemver(kernelVersion)
	for _, op := range coreConstraintOperators {
		if rest, ok := strings.CutPrefix(constraint, op); ok {
			cmp := semver.Compare(kv, canonicalSemver(rest))
			switch op {
			case ">=":
				return cmp >= 0
			case "<=":
				return cmp <= 0
			case "==":
				return cmp == 0
			case ">":
				return cmp > 0
			case "<":
				return cmp < 0
			}
		}
	}
	return semver.Compare(kv, canonicalSemver(constraint)) == 0
}

// ErrInvalidManifest wraps every manifest validation failure.
var ErrInvalidManifest = errors.New("invalid manifest")

// Validate reports whether m is well-formed. It does not check anything
// about the runtime environment (e.g. whether requires.core is actually
// satisfied by the running kernel) — that is the capability registry's job
// (slice 2.3); this only checks m's own internal shape.
func (m Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidManifest)
	}
	if !semver.IsValid(canonicalSemver(m.Version)) {
		return fmt.Errorf("%w: version %q is not a valid semantic version", ErrInvalidManifest, m.Version)
	}
	if !validRuntimeTiers[m.Runtime] {
		return fmt.Errorf("%w: unknown runtime tier %q", ErrInvalidManifest, m.Runtime)
	}
	if !validCoreConstraint(m.Requires.Core) {
		return fmt.Errorf("%w: requires.core %q is not a valid version constraint", ErrInvalidManifest, m.Requires.Core)
	}
	if m.Requires.Contract == "" {
		return fmt.Errorf("%w: requires.contract must not be empty", ErrInvalidManifest)
	}
	if err := validateAPIScopes(m.API); err != nil {
		return err
	}
	if err := validatePermissions(m.Permissions); err != nil {
		return err
	}
	return nil
}

func validatePermissions(perms []Permission) error {
	seen := make(map[string]bool, len(perms))
	for _, p := range perms {
		if !knownPermissions[p.Name] {
			return fmt.Errorf("%w: unknown or forbidden permission %q", ErrInvalidManifest, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("%w: permission %q declared more than once", ErrInvalidManifest, p.Name)
		}
		seen[p.Name] = true
		switch p.Name {
		case "network":
			if len(p.Args) == 0 {
				return fmt.Errorf("%w: permission %q requires at least one allowlisted domain", ErrInvalidManifest, p.Name)
			}
		default:
			if len(p.Args) != 0 {
				return fmt.Errorf("%w: permission %q does not accept arguments", ErrInvalidManifest, p.Name)
			}
		}
	}
	return nil
}

func validateAPIScopes(scopes []APIScope) error {
	seen := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		validScopes, ok := knownAPIScopes[s.Capability]
		if !ok {
			return fmt.Errorf("%w: unknown api capability %q", ErrInvalidManifest, s.Capability)
		}
		if seen[s.Capability] {
			return fmt.Errorf("%w: api capability %q declared more than once", ErrInvalidManifest, s.Capability)
		}
		seen[s.Capability] = true
		if len(s.Scopes) == 0 {
			return fmt.Errorf("%w: api capability %q must declare at least one scope", ErrInvalidManifest, s.Capability)
		}
		for _, scope := range s.Scopes {
			if !validScopes[scope] {
				return fmt.Errorf("%w: unknown scope %q for api capability %q", ErrInvalidManifest, scope, s.Capability)
			}
		}
	}
	return nil
}

// AllowsNetworkHost reports whether host is permitted by m's declared
// "network" permission allowlist (PRD §7.3's example: `network:
// [api.stripe.com]`). Deny-by-default (§10.1, "adversarial by default"): a
// manifest that declares no "network" permission at all denies every host,
// and an empty/blank host is never allowed regardless of the allowlist.
//
// Matching is EXACT HOSTNAME ONLY, case-insensitive — no wildcard or
// subdomain matching. The PRD's own "api.stripe.com" example does not
// specify subdomain semantics, and inventing an implicit "*.stripe.com"-style
// bypass a plugin author never explicitly declared would cut against
// adversarial-by-default. A plugin that legitimately needs several
// subdomains must declare each one.
//
// This is a decision PRIMITIVE, not an enforcement POINT: nothing calls this
// yet to actually intercept an outbound request, because the interception
// points (the WASM host, the RPC broker — PRD §10.3) don't exist yet
// (they're slices 2.4/2.5). See this slice's tracking doc,
// docs/implementation/active/0014-phase2-slice2.6-permissions-enforcement.md.
func (m Manifest) AllowsNetworkHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, p := range m.Permissions {
		if p.Name != "network" {
			continue
		}
		for _, allowed := range p.Args {
			if strings.ToLower(strings.TrimSpace(allowed)) == host {
				return true
			}
		}
	}
	return false
}

// canonicalSemver adds the "v" prefix golang.org/x/mod/semver requires, so
// manifests can write plain "1.0.0" like the PRD's own examples (§7.3) do.
func canonicalSemver(v string) string {
	if v == "" || v[0] == 'v' {
		return v
	}
	return "v" + v
}
