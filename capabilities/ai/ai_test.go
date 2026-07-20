package ai_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/glyphux/glyphux/capabilities/ai"
	"github.com/glyphux/glyphux/pkg/sdk"
)

func TestPluginManifestIsWellFormed(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()
	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	p := ai.New(adapter)
	if err := p.Manifest().Validate(); err != nil {
		t.Fatalf("expected the ai plugin's own manifest to be valid, got %v", err)
	}
}

func TestPluginManifestDeclaresAdapterHostInNetworkAllowlist(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()
	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	p := ai.New(adapter)
	manifest := p.Manifest()

	srvURL, _ := url.Parse(srv.URL)
	if !manifest.AllowsNetworkHost(srvURL.Hostname()) {
		t.Fatalf("expected manifest's network permission to allowlist %q", srvURL.Hostname())
	}
	if manifest.AllowsNetworkHost("evil.example.com") {
		t.Fatal("expected manifest's network permission to deny an undeclared host")
	}
}

func TestPluginRegisterIsANoOpThatSucceeds(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()
	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	p := ai.New(adapter)
	host, err := sdk.NewHostAPI(p.Manifest(), sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// TestEndToEndCallerPluginUsesAIPluginsService is the whole vertical slice:
// a fictitious CALLING plugin (a separate manifest declaring api: [ai:
// [generate]] and a network allowlist for the SAME adapter host the ai
// Plugin itself was configured with) invokes Service.Generate against its
// own HostAPI and gets back a real answer from a local fake OpenAI-shaped
// server — proving the full PRD §14.1 promise ("a plugin wanting AI
// declares api: [ai: [generate]] ... and receives a scoped, rate-limited
// surface") end to end, not just each piece in isolation.
func TestEndToEndCallerPluginUsesAIPluginsService(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()
	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	aiPlugin := ai.New(adapter)

	srvURL, _ := url.Parse(srv.URL)
	callerManifest := sdk.Manifest{
		Name: "product-recommendation-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API:      []sdk.APIScope{{Capability: "ai", Scopes: []string{"generate"}}},
		Permissions: []sdk.Permission{
			{Name: "network", Args: []string{srvURL.Hostname()}},
		},
	}
	callerHost, err := sdk.NewHostAPI(callerManifest, sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI (caller): %v", err)
	}

	resp, err := aiPlugin.Service.Generate(context.Background(), callerHost, ai.GenerateRequest{
		Model:  "gpt-4o-mini",
		Prompt: "Recommend a product",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text == "" {
		t.Fatal("expected a non-empty generated response")
	}
}

func TestEndToEndCallerPluginDeniedWithoutDeclaringAIScope(t *testing.T) {
	srv := newFakeOpenAIServer(t, "sk-test-key", true)
	defer srv.Close()
	adapter, err := ai.NewOpenAICompatibleAdapter(srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("NewOpenAICompatibleAdapter: %v", err)
	}
	aiPlugin := ai.New(adapter)

	callerManifest := sdk.Manifest{
		Name: "some-other-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
	}
	callerHost, err := sdk.NewHostAPI(callerManifest, sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI (caller): %v", err)
	}

	_, err = aiPlugin.Service.Generate(context.Background(), callerHost, ai.GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected Generate to be denied for a plugin that never declared api: [ai: [generate]]")
	}
}
