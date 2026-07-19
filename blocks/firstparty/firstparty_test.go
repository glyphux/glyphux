package firstparty_test

import (
	"testing"

	"github.com/glyphux/glyphux/blocks/firstparty"
	"github.com/glyphux/glyphux/pkg/blocks"
)

func TestRegisterAllRegistersEveryFirstPartyBlock(t *testing.T) {
	r := blocks.New()
	if err := firstparty.RegisterAll(r); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"heading", "paragraph", "image", "container"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected first-party block %q to be registered", name)
		}
	}
}

func TestHeadingDeclaresTextProp(t *testing.T) {
	r := blocks.New()
	if err := firstparty.RegisterAll(r); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	def, _ := r.Get("heading")
	if _, ok := def.Props["text"]; !ok {
		t.Fatalf("expected heading to declare a %q prop, got %+v", "text", def.Props)
	}
}

func TestImageDeclaresSrcAndAltProps(t *testing.T) {
	r := blocks.New()
	if err := firstparty.RegisterAll(r); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	def, _ := r.Get("image")
	if _, ok := def.Props["src"]; !ok {
		t.Fatalf("expected image to declare a %q prop", "src")
	}
	if _, ok := def.Props["alt"]; !ok {
		t.Fatalf("expected image to declare an %q prop", "alt")
	}
}

func TestContainerDeclaresAContentSlotAndNoProps(t *testing.T) {
	r := blocks.New()
	if err := firstparty.RegisterAll(r); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	def, _ := r.Get("container")
	if len(def.Slots) != 1 || def.Slots[0] != "content" {
		t.Fatalf("expected container to declare exactly one %q slot, got %+v", "content", def.Slots)
	}
}

func TestRegisterAllIsIdempotentOnASharedRegistryOnlyOnce(t *testing.T) {
	r := blocks.New()
	if err := firstparty.RegisterAll(r); err != nil {
		t.Fatalf("RegisterAll (first call): %v", err)
	}
	if err := firstparty.RegisterAll(r); err == nil {
		t.Fatal("expected a second RegisterAll call on the same registry to fail (duplicate names), proving names are stable and real duplicates are rejected")
	}
}
