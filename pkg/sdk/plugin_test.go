package sdk_test

import (
	"testing"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// fakePlugin is a minimal sdk.Plugin used only to prove the interface shape
// is exactly PRD §8.3's conceptual contract: Manifest() + Register(host).
type fakePlugin struct {
	manifest     sdk.Manifest
	registerHost sdk.HostAPI
}

func (p *fakePlugin) Manifest() sdk.Manifest { return p.manifest }

func (p *fakePlugin) Register(host sdk.HostAPI) error {
	p.registerHost = host
	return nil
}

func TestPluginInterfaceIsSatisfiedByManifestAndRegister(t *testing.T) {
	p := &fakePlugin{manifest: validManifest()}
	var _ sdk.Plugin = p // compile-time proof of the contract shape

	if p.Manifest().Name != "forms" {
		t.Fatalf("unexpected manifest: %+v", p.Manifest())
	}

	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if p.registerHost != host {
		t.Fatal("expected Register to receive the exact HostAPI instance passed to it")
	}
}
