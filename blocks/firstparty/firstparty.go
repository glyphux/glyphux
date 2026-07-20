// Package firstparty ships glyphux's own Layer-2 blocks in-tree (PRD §14
// slice 4.2: "first-party blocks ship in-tree, third-party blocks arrive
// as plugins"). Every block here is registered through the exact same
// pkg/blocks.Registry API a third-party plugin's RegisterBlock call goes
// through — no private registration path exists.
//
// These are deliberately non-logic-bearing (pure prop/slot declarations,
// no behavior): PRD §8.1 reserves logic-bearing blocks for Tier-B WASM
// plugins ("a block with logic runs as a sandboxed, capability-scoped
// plugin"), so a first-party static block stays a plain Definition, never
// gains a Go method a WASM-hosted block couldn't also provide.
package firstparty

import (
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

// RegisterAll registers every first-party block into r. Returns an error
// (without partially registering) if r already has any of these names —
// the same duplicate-rejection every plugin's RegisterBlock call is
// subject to.
func RegisterAll(r *blocks.Registry) error {
	for _, def := range []blocks.Definition{
		heading,
		paragraph,
		image,
		container,
	} {
		if err := r.Register(def); err != nil {
			return err
		}
	}
	return nil
}

var heading = blocks.Definition{
	Name:        "heading",
	DisplayName: "Heading",
	Props: map[string]contract.Field{
		"text":  {Type: contract.FieldString, Required: true},
		"level": {Type: contract.FieldNumber},
	},
}

var paragraph = blocks.Definition{
	Name:        "paragraph",
	DisplayName: "Paragraph",
	Props: map[string]contract.Field{
		"text": {Type: contract.FieldRichText, Required: true},
	},
}

var image = blocks.Definition{
	Name:        "image",
	DisplayName: "Image",
	Props: map[string]contract.Field{
		"src": {Type: contract.FieldMedia, Required: true},
		"alt": {Type: contract.FieldString, Required: true},
	},
}

// container is the one first-party block with a slot — a plain box that
// lays out whatever nested blocks a layout places into its "content" slot.
var container = blocks.Definition{
	Name:        "container",
	DisplayName: "Container",
	Slots:       []string{"content"},
}
