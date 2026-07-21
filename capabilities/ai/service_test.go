package ai

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// stubAdapter is a hand-rolled, in-process Adapter used only to isolate
// Service's OWN gating/rate-limiting logic (this file) from any real
// provider wire shape — the real provider wire shapes (request-building,
// response-parsing, error-handling) are proven separately against local
// httptest fake servers in claude_test.go/openai_test.go/gemini_test.go,
// per this slice's TDD plan: the adapter seam and the capability's own
// scoping/rate-limiting seam are two different things worth testing
// independently.
type stubAdapter struct {
	host          string
	generateCalls int
	embedCalls    int
	classifyCalls int
	err           error
}

func (s *stubAdapter) AllowlistHost() string { return s.host }

func (s *stubAdapter) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	s.generateCalls++
	if s.err != nil {
		return nil, s.err
	}
	return &GenerateResponse{Text: "generated: " + req.Prompt, Model: req.Model, FinishReason: "stop"}, nil
}

func (s *stubAdapter) Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	s.embedCalls++
	if s.err != nil {
		return nil, s.err
	}
	return &EmbedResponse{Vector: []float64{0.1, 0.2, 0.3}, Model: req.Model}, nil
}

func (s *stubAdapter) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResponse, error) {
	s.classifyCalls++
	if s.err != nil {
		return nil, s.err
	}
	if len(req.Labels) == 0 {
		return &ClassifyResponse{Model: req.Model}, nil
	}
	return &ClassifyResponse{Label: req.Labels[0], Model: req.Model}, nil
}

// callerHost builds a HostAPI for a fictitious caller plugin declaring
// exactly scopes on the "ai" capability, plus whatever extra is passed in
// extra — mirroring capabilities/commerce's own test convention of building
// a fresh HostAPI per manifest shape under test rather than reusing one
// plugin's own Register.
func callerHost(t *testing.T, deps sdk.KernelDeps, scopes []string, extra ...sdk.APIScope) sdk.HostAPI {
	t.Helper()
	var api []sdk.APIScope
	if len(scopes) > 0 {
		api = append(api, sdk.APIScope{Capability: "ai", Scopes: scopes})
	}
	api = append(api, extra...)
	manifest := sdk.Manifest{
		Name: "caller-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API:      api,
		Permissions: []sdk.Permission{
			{Name: "network", Args: []string{"provider.example.com"}},
		},
	}
	host, err := sdk.NewHostAPI(manifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	return host
}

func TestGenerateDeniedWithoutAIGenerateScope(t *testing.T) {
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	// Declares ai:embed but not ai:generate.
	host := callerHost(t, sdk.KernelDeps{}, []string{"embed"})

	_, err := svc.Generate(context.Background(), host, GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected Generate to be denied without ai:generate scope")
	}
	if !errors.Is(err, sdk.ErrScopeNotDeclared) {
		t.Errorf("expected error to wrap sdk.ErrScopeNotDeclared, got %v", err)
	}
	if adapter.generateCalls != 0 {
		t.Errorf("expected the adapter never to be called when scope is denied, got %d calls", adapter.generateCalls)
	}
}

func TestGenerateSucceedsWithAIGenerateScope(t *testing.T) {
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	host := callerHost(t, sdk.KernelDeps{}, []string{"generate"})

	resp, err := svc.Generate(context.Background(), host, GenerateRequest{Model: "m", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text != "generated: hi" {
		t.Errorf("got Text=%q, want %q", resp.Text, "generated: hi")
	}
	if adapter.generateCalls != 1 {
		t.Errorf("expected exactly 1 adapter call, got %d", adapter.generateCalls)
	}
}

func TestEmbedDeniedWithoutAIEmbedScope(t *testing.T) {
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	host := callerHost(t, sdk.KernelDeps{}, []string{"generate"})

	_, err := svc.Embed(context.Background(), host, EmbedRequest{Model: "m", Input: "hi"})
	if err == nil {
		t.Fatal("expected Embed to be denied without ai:embed scope")
	}
	if adapter.embedCalls != 0 {
		t.Errorf("expected the adapter never to be called when scope is denied, got %d calls", adapter.embedCalls)
	}
}

func TestClassifyDeniedWithoutAIClassifyScope(t *testing.T) {
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	host := callerHost(t, sdk.KernelDeps{}, []string{"generate"})

	_, err := svc.Classify(context.Background(), host, ClassifyRequest{Model: "m", Input: "hi", Labels: []string{"a", "b"}})
	if err == nil {
		t.Fatal("expected Classify to be denied without ai:classify scope")
	}
	if adapter.classifyCalls != 0 {
		t.Errorf("expected the adapter never to be called when scope is denied, got %d calls", adapter.classifyCalls)
	}
}

func TestGenerateRefusedAgainstDisallowedProviderHost(t *testing.T) {
	// The adapter's own host ("evil.example.com") is never what the caller's
	// manifest allowlists ("provider.example.com" — see callerHost) —
	// proving AllowsNetworkHost's deny-by-default gate refuses the call
	// before the adapter is ever invoked, mirroring
	// capabilities/commerce.StartCheckout's identical gate.
	adapter := &stubAdapter{host: "evil.example.com"}
	svc := NewService(adapter)
	host := callerHost(t, sdk.KernelDeps{}, []string{"generate"})

	_, err := svc.Generate(context.Background(), host, GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected Generate to be refused against a provider host the caller's manifest doesn't allowlist")
	}
	if !errors.Is(err, ErrProviderHostNotAllowed) {
		t.Errorf("expected error to wrap ErrProviderHostNotAllowed, got %v", err)
	}
	if adapter.generateCalls != 0 {
		t.Errorf("expected the adapter never to be called when the host is disallowed, got %d calls", adapter.generateCalls)
	}
}

func TestGenerateEnforcesRateLimitPerCallingPlugin(t *testing.T) {
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	svc.Limits.Generate = RateLimit{MaxCalls: 2, Window: time.Hour}
	host := callerHost(t, sdk.KernelDeps{}, []string{"generate"})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := svc.Generate(ctx, host, GenerateRequest{Model: "m", Prompt: "hi"}); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	_, err := svc.Generate(ctx, host, GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected the 3rd call within the window to be rate-limited")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected error to wrap ErrRateLimited, got %v", err)
	}
	if adapter.generateCalls != 2 {
		t.Errorf("expected exactly 2 adapter calls before the limit tripped, got %d", adapter.generateCalls)
	}
}

func TestRateLimitResetsAfterWindowElapses(t *testing.T) {
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	svc.Limits.Generate = RateLimit{MaxCalls: 1, Window: time.Minute}
	host := callerHost(t, sdk.KernelDeps{}, []string{"generate"})
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	defer func() { timeNow = time.Now }()
	timeNow = func() time.Time { return base }

	if _, err := svc.Generate(ctx, host, GenerateRequest{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := svc.Generate(ctx, host, GenerateRequest{Model: "m", Prompt: "hi"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second call within window: expected ErrRateLimited, got %v", err)
	}

	timeNow = func() time.Time { return base.Add(2 * time.Minute) }
	if _, err := svc.Generate(ctx, host, GenerateRequest{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("call after window elapsed: unexpected error: %v", err)
	}
}

func TestRateLimitIsPerCallingPluginNotGlobal(t *testing.T) {
	// Two distinct calling plugins (different manifest Name -> different
	// ScopedKV namespace, per pkg/sdk.hostAPI.Store) each get their own
	// budget — one plugin exhausting its limit must never affect the other.
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	svc.Limits.Generate = RateLimit{MaxCalls: 1, Window: time.Hour}
	deps := sdk.KernelDeps{}
	ctx := context.Background()

	hostA := callerHost(t, deps, []string{"generate"})
	if _, err := svc.Generate(ctx, hostA, GenerateRequest{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("plugin A first call: %v", err)
	}
	if _, err := svc.Generate(ctx, hostA, GenerateRequest{Model: "m", Prompt: "hi"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("plugin A second call: expected ErrRateLimited, got %v", err)
	}

	manifestB := sdk.Manifest{
		Name: "other-caller-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires:    sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API:         []sdk.APIScope{{Capability: "ai", Scopes: []string{"generate"}}},
		Permissions: []sdk.Permission{{Name: "network", Args: []string{"provider.example.com"}}},
	}
	hostB, err := sdk.NewHostAPI(manifestB, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (plugin B): %v", err)
	}
	if _, err := svc.Generate(ctx, hostB, GenerateRequest{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("plugin B first call: expected to succeed on its own budget, got %v", err)
	}
}

func TestGenerateEmitsObservabilityEventOnlyWhenEventsScopeDeclared(t *testing.T) {
	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	deps := sdk.KernelDeps{Bus: sdk.NewEventBus()}

	// Subscriber on the shared bus, mirroring capabilities/commerce's own
	// cross-plugin subscriber test pattern.
	var received *GeneratedEvent
	subscriberManifest := sdk.Manifest{
		Name: "subscriber-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API:      []sdk.APIScope{{Capability: "events", Scopes: []string{"subscribe"}}},
	}
	subscriber, err := sdk.NewHostAPI(subscriberManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (subscriber): %v", err)
	}
	if err := subscriber.On("ai.generated", func(ctx context.Context, payload any) error {
		e := payload.(GeneratedEvent)
		received = &e
		return nil
	}); err != nil {
		t.Fatalf("On: %v", err)
	}

	// A caller WITHOUT events:emit still gets a successful Generate call —
	// emission is best-effort/additive, never a hard requirement.
	hostWithoutEvents := callerHost(t, deps, []string{"generate"})
	if _, err := svc.Generate(context.Background(), hostWithoutEvents, GenerateRequest{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("Generate (no events scope): %v", err)
	}
	if received != nil {
		t.Fatal("expected no event observed for a caller that declared no events:emit scope")
	}

	hostWithEvents := callerHost(t, deps, []string{"generate"}, sdk.APIScope{Capability: "events", Scopes: []string{"emit"}})
	if _, err := svc.Generate(context.Background(), hostWithEvents, GenerateRequest{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("Generate (with events scope): %v", err)
	}
	if received == nil {
		t.Fatal("expected ai.generated to be observed once the caller declared events:emit")
	}
}

// testContentKernel wires real content/composition stores over a fresh
// SQLite DB, mirroring capabilities/commerce's own testKernel convention —
// needed for TestSummarizeContentItem below, the one test exercising this
// capability's real "content" dependency (PRD §7.2: "ai -> content,
// events").
func testContentKernel(t *testing.T) sdk.KernelDeps {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), content.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}
	return sdk.KernelDeps{
		Compositions: comps,
		Content:      content.NewAPI(comps, content.NewStore(d)),
	}
}

// defineNoteContentType registers a minimal "note" content type through a
// setup HostAPI declaring content:write — the same host-mediated path every
// other capability in this codebase uses to define a content type (see
// capabilities/commerce.Plugin.Register), never a direct
// composition.Store call.
func defineNoteContentType(t *testing.T, deps sdk.KernelDeps) {
	t.Helper()
	setup := callerHost(t, deps, nil, sdk.APIScope{Capability: "content", Scopes: []string{"write"}})
	if err := setup.RegisterContentType(context.Background(), "note", contract.ContentType{
		Fields: map[string]contract.Field{"body": {Type: contract.FieldString, Required: true}},
	}); err != nil {
		t.Fatalf("RegisterContentType: %v", err)
	}
}

func TestSummarizeContentItemUsesContentDependency(t *testing.T) {
	deps := testContentKernel(t)
	defineNoteContentType(t, deps)

	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	host := callerHost(t, deps, []string{"generate"}, sdk.APIScope{Capability: "content", Scopes: []string{"read", "write"}})

	item, err := host.Content().Create(context.Background(), "note", map[string]any{"body": "hello world"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	resp, err := svc.SummarizeContentItem(context.Background(), host, "note", item.ID, GenerateRequest{Model: "m", Prompt: "Summarize:"})
	if err != nil {
		t.Fatalf("SummarizeContentItem: %v", err)
	}
	if !containsSubstring(resp.Text, "hello world") {
		t.Errorf("expected the generated prompt to have included the content item's data, got response text %q", resp.Text)
	}
}

func TestSummarizeContentItemDeniedWithoutContentReadScope(t *testing.T) {
	deps := testContentKernel(t)
	defineNoteContentType(t, deps)

	adapter := &stubAdapter{host: "provider.example.com"}
	svc := NewService(adapter)
	// A privileged host creates the item; the host under test below declares
	// no content:read scope at all.
	setupHost := callerHost(t, deps, []string{"generate"}, sdk.APIScope{Capability: "content", Scopes: []string{"read", "write"}})
	item, err := setupHost.Content().Create(context.Background(), "note", map[string]any{"body": "hello world"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	host := callerHost(t, deps, []string{"generate"})
	_, err = svc.SummarizeContentItem(context.Background(), host, "note", item.ID, GenerateRequest{Model: "m", Prompt: "Summarize:"})
	if err == nil {
		t.Fatal("expected SummarizeContentItem to be denied without content:read scope")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
