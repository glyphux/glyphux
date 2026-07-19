// Package blocks is the public Layer-2 block registry (PRD §14 slice
// 4.2: "plugins/themes register blocks via the public API (§13.2);
// first-party blocks ship in-tree, third-party blocks arrive as
// plugins"). It is importable by plugin/theme authors, exactly like
// pkg/contract and pkg/sdk — a block definition declares a block type's
// prop schema and, for container-shaped blocks, the named slots it
// exposes for nested blocks.
package blocks

import (
	"fmt"
	"sync"

	"github.com/glyphux/glyphux/pkg/contract"
)

// Definition declares one registered block type: its prop schema (reusing
// pkg/contract.Field, the same shape a content type's own fields use, so
// authors learn one field-typing vocabulary) and, for a container-shaped
// block, the named slots it accepts nested blocks into. A leaf block (e.g.
// "heading") simply declares no slots.
type Definition struct {
	Name        string
	DisplayName string
	Props       map[string]contract.Field
	Slots       []string
}

// Registry is a thread-safe collection of registered block definitions —
// the live counterpart to pkg/contract.Layout's purely structural
// validation (which cannot check a Block.Type against real definitions,
// since pkg/contract must not depend on a runtime registry).
type Registry struct {
	mu   sync.RWMutex
	defs map[string]Definition
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{defs: make(map[string]Definition)}
}

// Register adds def to the registry. Returns an error if def.Name is empty
// or already registered — block type names are a flat, global namespace
// (like content type names), so a later registration never silently
// shadows an earlier one.
func (r *Registry) Register(def Definition) error {
	if def.Name == "" {
		return fmt.Errorf("blocks: definition name must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.defs[def.Name]; exists {
		return fmt.Errorf("blocks: %q is already registered", def.Name)
	}
	r.defs[def.Name] = def
	return nil
}

// Get returns the definition registered under name, if any.
func (r *Registry) Get(name string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[name]
	return def, ok
}

// List returns every registered definition, in no particular order.
func (r *Registry) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Definition, 0, len(r.defs))
	for _, def := range r.defs {
		list = append(list, def)
	}
	return list
}

// ValidateLayout checks that every block in layout (recursively, through
// every nested slot) names a block type that exists in registry. This is
// the live-registry-aware validation pkg/contract.Layout.Validate cannot
// itself perform — call layout.Validate() first for structural checks,
// then ValidateLayout for existence checks against what's actually
// registered.
func ValidateLayout(layout *contract.Layout, registry *Registry) error {
	var errs contract.ValidationErrors
	for regionName, region := range layout.Regions {
		errs = append(errs, contract.WalkBlocks("regions."+regionName+".blocks", region.Blocks,
			func(path string, b contract.Block) contract.ValidationErrors {
				if _, ok := registry.Get(b.Type); !ok {
					return contract.ValidationErrors{{
						Path:    path + ".type",
						Message: fmt.Sprintf("block type %q is not registered", b.Type),
					}}
				}
				return nil
			})...)
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}
