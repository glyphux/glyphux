package sdk

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Capability dependency graph and resolver (PRD §7.2, §7.5).
//
// This deliberately operates on plain capability-name strings, not
// Manifest's APIScope.Capability field, and is NOT layered on top of
// knownAPIScopes in manifest.go. See this slice's tracking doc
// (docs/implementation/active/0013-phase2-slice2.3-capability-registry.md)
// for the full reasoning; in short: knownAPIScopes only validates the four
// domain surfaces a single plugin manifest's API axis may declare today
// (content, users, media, events) and HostAPI has concrete implementations
// for. The PRD §7.2 capability graph is a broader, composition/deployment-
// level vocabulary (identity, permissions, forms, seo, mailer,
// notifications, membership, payments, commerce, marketplace, tenancy, ai,
// stock-media, plus the implied leaf nodes storage/packaging/signing) most
// of which have no HostAPI surface built yet. Conflating the two now would
// force premature growth of knownAPIScopes for capabilities nothing
// implements, and manifest.go's validation logic is explicitly out of scope
// for this slice (owned by slice 2.6 in parallel). A future slice can add a
// thin translation from a manifest's declared API scopes into capability
// names (e.g. APIScope{Capability: "users"} -> capability "identity") once
// it decides that mapping; this package does not assume or perform it.

// ErrUnknownCapability is returned when Resolve is asked about a capability
// name that is not a node in the graph — either explicitly requested, or
// named as a dependency of another node (which would indicate the graph
// itself is malformed, not just the caller's input).
var ErrUnknownCapability = errors.New("sdk: unknown capability")

// CycleError reports a circular capability dependency detected during
// resolution (PRD §7.5 rule 2: "circular dependencies are rejected at
// composition-validation time"). Path is the cycle in traversal order,
// ending back at its own start (e.g. ["a", "b", "c", "a"]).
type CycleError struct {
	Path []string
}

func (e *CycleError) Error() string {
	return "sdk: circular capability dependency: " + strings.Join(e.Path, " -> ")
}

// Graph is a capability dependency graph: each capability name maps to the
// set of capabilities it requires (possibly none). Graph is immutable once
// constructed — NewGraph copies its input so later mutation of the caller's
// map can't corrupt a shared Graph.
type Graph struct {
	deps map[string][]string
}

// NewGraph builds a Graph from a dependency map (capability name -> its
// required capability names). Nodes with no dependencies may map to nil or
// an empty slice.
func NewGraph(deps map[string][]string) *Graph {
	cp := make(map[string][]string, len(deps))
	for k, v := range deps {
		cp[k] = append([]string(nil), v...)
	}
	return &Graph{deps: cp}
}

// Resolution is the result of resolving a set of explicitly-declared
// capabilities against a Graph: the full transitive dependency closure,
// split into what was explicitly declared and what was implicitly pulled in
// (PRD §7.5 rule 4: "all resolved capabilities are recorded in the compiled
// composition"; rule 1: "explicit capabilities take precedence over
// implicit ones" — operationally, a capability that is both explicitly
// declared AND would be pulled in as a dependency of something else is
// recorded exactly once, under Explicit, never duplicated into Implicit).
type Resolution struct {
	// Explicit is the deduplicated set of capabilities the caller declared
	// directly, in first-seen order.
	Explicit []string
	// Implicit is the set of capabilities pulled in only as transitive
	// dependencies of an explicit capability — i.e. the closure minus
	// Explicit. Sorted for deterministic output (PRD §7.2: "implicit
	// inclusions are surfaced as warnings", which wants a stable list to
	// report, not visitation-order noise).
	Implicit []string
}

// All returns the full resolved set (Explicit ∪ Implicit), sorted.
func (r Resolution) All() []string {
	all := make([]string, 0, len(r.Explicit)+len(r.Implicit))
	all = append(all, r.Explicit...)
	all = append(all, r.Implicit...)
	sort.Strings(all)
	return all
}

// IsExplicit reports whether name was declared explicitly, as opposed to
// only pulled in as a transitive dependency.
func (r Resolution) IsExplicit(name string) bool {
	for _, c := range r.Explicit {
		if c == name {
			return true
		}
	}
	return false
}

// Resolve computes the full transitive dependency closure of explicit
// against g: every capability in explicit, plus every capability reachable
// from them by following the graph's dependency edges. It rejects unknown
// capability names and circular dependencies.
func (g *Graph) Resolve(explicit []string) (Resolution, error) {
	explicitSet := make(map[string]bool, len(explicit))
	explicitOrdered := make([]string, 0, len(explicit))
	for _, c := range explicit {
		if explicitSet[c] {
			continue
		}
		if _, ok := g.deps[c]; !ok {
			return Resolution{}, fmt.Errorf("%w: %q", ErrUnknownCapability, c)
		}
		explicitSet[c] = true
		explicitOrdered = append(explicitOrdered, c)
	}

	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	var allOrdered []string

	var visit func(node string, path []string) error
	visit = func(node string, path []string) error {
		if onStack[node] {
			return &CycleError{Path: append(append([]string{}, path...), node)}
		}
		if visited[node] {
			return nil
		}
		onStack[node] = true
		defer delete(onStack, node)

		nextPath := append(append([]string{}, path...), node)
		for _, dep := range g.deps[node] {
			if _, ok := g.deps[dep]; !ok {
				return fmt.Errorf("%w: %q (required by %q)", ErrUnknownCapability, dep, node)
			}
			if err := visit(dep, nextPath); err != nil {
				return err
			}
		}

		visited[node] = true
		allOrdered = append(allOrdered, node)
		return nil
	}

	for _, c := range explicitOrdered {
		if err := visit(c, nil); err != nil {
			return Resolution{}, err
		}
	}

	implicit := make([]string, 0, len(allOrdered))
	for _, c := range allOrdered {
		if !explicitSet[c] {
			implicit = append(implicit, c)
		}
	}
	sort.Strings(implicit)

	return Resolution{Explicit: explicitOrdered, Implicit: implicit}, nil
}

// CapabilityGraph is the PRD §7.2 capability dependency graph, exactly as
// specified there. Four nodes referenced as dependencies in §7.2's diagram
// are not themselves headline capabilities in §7.1's list but must exist as
// graph nodes for the graph to be closed under resolution: "storage"
// (media's dependency), "events" (notifications/payments/commerce/ai's
// dependency — the event bus, slice 2.2's subject matter), and
// "packaging"/"signing" (marketplace's dependencies). They are modeled here
// as ordinary root nodes with no further dependencies, the same way
// "identity" is described as "kernel-backed" in §7.2 yet still participates
// as a normal graph node.
//
// "tenancy" appears with its declared dependencies (identity, permissions)
// per §11.5's instruction that the graph include it as forward-compatible —
// it is otherwise completely inert: no enforcement, no defaults, no special
// casing anywhere in this package or elsewhere. Do not mistake its presence
// here for a built feature.
var CapabilityGraph = NewGraph(map[string][]string{
	"content":       nil,
	"identity":      nil,
	"permissions":   {"identity"},
	"storage":       nil,
	"media":         {"storage"},
	"forms":         {"content"},
	"seo":           {"content"},
	"mailer":        nil,
	"events":        nil,
	"notifications": {"events", "mailer"},
	"membership":    {"identity", "permissions", "content", "notifications"},
	"payments":      {"events"},
	"commerce":      {"content", "identity", "permissions", "payments", "events", "notifications"},
	"packaging":     nil,
	"signing":       nil,
	"marketplace":   {"commerce", "identity", "permissions", "packaging", "signing"},
	"tenancy":       {"identity", "permissions"}, // PLANNED, deferred — see doc comment above.
	"ai":            {"content", "events"},
	"stock-media":   {"media"},
})
