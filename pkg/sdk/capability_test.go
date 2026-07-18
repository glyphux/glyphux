package sdk_test

import (
	"errors"
	"testing"

	"github.com/glyphux/glyphux/pkg/sdk"
)

func TestResolveMailerHasNoImplicitDependencies(t *testing.T) {
	res, err := sdk.CapabilityGraph.Resolve([]string{"mailer"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res.Implicit) != 0 {
		t.Fatalf("expected no implicit capabilities, got %v", res.Implicit)
	}
	if got := res.All(); len(got) != 1 || got[0] != "mailer" {
		t.Fatalf("expected All() == [mailer], got %v", got)
	}
}

func TestResolveCommerceResolvesFullTransitiveClosure(t *testing.T) {
	res, err := sdk.CapabilityGraph.Resolve([]string{"commerce"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res.Explicit) != 1 || res.Explicit[0] != "commerce" {
		t.Fatalf("expected explicit == [commerce], got %v", res.Explicit)
	}
	wantImplicit := []string{"content", "events", "identity", "mailer", "notifications", "payments", "permissions"}
	assertSameSet(t, res.Implicit, wantImplicit)

	wantAll := append(append([]string{}, wantImplicit...), "commerce")
	assertSameSet(t, res.All(), wantAll)
}

func TestResolveTenancyIsAnInertGraphNodeWithItsDeclaredDeps(t *testing.T) {
	res, err := sdk.CapabilityGraph.Resolve([]string{"tenancy"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertSameSet(t, res.Implicit, []string{"identity", "permissions"})
}

func TestResolveMarketplaceResolvesFullTransitiveClosure(t *testing.T) {
	res, err := sdk.CapabilityGraph.Resolve([]string{"marketplace"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	wantImplicit := []string{
		"commerce", "content", "events", "identity", "mailer",
		"notifications", "packaging", "payments", "permissions", "signing",
	}
	assertSameSet(t, res.Implicit, wantImplicit)
}

func TestResolveExplicitTakesPrecedenceOverImplicit(t *testing.T) {
	// permissions depends on identity. Declaring both explicitly means
	// identity must be recorded once, as explicit — never duplicated into
	// Implicit just because permissions would also pull it in.
	res, err := sdk.CapabilityGraph.Resolve([]string{"permissions", "identity"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertSameSet(t, res.Explicit, []string{"permissions", "identity"})
	if len(res.Implicit) != 0 {
		t.Fatalf("expected identity to be recorded only as explicit, got implicit %v", res.Implicit)
	}
}

func TestResolveDedupesRepeatedExplicitInput(t *testing.T) {
	res, err := sdk.CapabilityGraph.Resolve([]string{"content", "content"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res.Explicit) != 1 {
		t.Fatalf("expected deduped explicit list, got %v", res.Explicit)
	}
}

func TestResolveNoDuplicatesAcrossDiamondDependencies(t *testing.T) {
	// membership depends on identity, permissions, content, notifications;
	// permissions itself depends on identity, and notifications depends on
	// events/mailer which don't overlap here — but identity is reachable via
	// two paths (directly, and via permissions). It must appear once.
	res, err := sdk.CapabilityGraph.Resolve([]string{"membership"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	seen := map[string]int{}
	for _, c := range res.All() {
		seen[c]++
	}
	for name, count := range seen {
		if count != 1 {
			t.Fatalf("capability %q appeared %d times in All(), want exactly once", name, count)
		}
	}
	if seen["identity"] != 1 {
		t.Fatalf("expected identity present exactly once, got %d", seen["identity"])
	}
}

func TestResolveUnknownExplicitCapabilityReturnsError(t *testing.T) {
	_, err := sdk.CapabilityGraph.Resolve([]string{"not-a-real-capability"})
	if !errors.Is(err, sdk.ErrUnknownCapability) {
		t.Fatalf("expected ErrUnknownCapability, got %v", err)
	}
}

func TestResolveEmptyExplicitSetResolvesToNothing(t *testing.T) {
	res, err := sdk.CapabilityGraph.Resolve(nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res.All()) != 0 {
		t.Fatalf("expected empty resolution, got %v", res.All())
	}
}

// --- cycle detection: exercised against a small synthetic graph, not the
// real PRD graph (which is acyclic by construction) ---

func TestResolveRejectsCircularDependency(t *testing.T) {
	g := sdk.NewGraph(map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	})
	_, err := g.Resolve([]string{"a"})
	if err == nil {
		t.Fatal("expected an error for a circular dependency, got nil")
	}
	var cycleErr *sdk.CycleError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected a *sdk.CycleError, got %T: %v", err, err)
	}
}

func TestResolveRejectsSelfReferentialCycle(t *testing.T) {
	g := sdk.NewGraph(map[string][]string{
		"a": {"a"},
	})
	_, err := g.Resolve([]string{"a"})
	var cycleErr *sdk.CycleError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected a *sdk.CycleError, got %T: %v", err, err)
	}
}

func TestResolveAcceptsNonCyclicSyntheticGraph(t *testing.T) {
	g := sdk.NewGraph(map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": nil,
	})
	res, err := g.Resolve([]string{"a"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertSameSet(t, res.All(), []string{"a", "b", "c"})
}

func TestCapabilityGraphContainsAllPRDNodes(t *testing.T) {
	want := []string{
		"content", "identity", "permissions", "media", "storage", "forms",
		"seo", "mailer", "events", "notifications", "membership", "payments",
		"commerce", "packaging", "signing", "marketplace", "tenancy", "ai",
		"stock-media",
	}
	for _, name := range want {
		if _, err := sdk.CapabilityGraph.Resolve([]string{name}); err != nil {
			t.Errorf("expected %q to be a known node in CapabilityGraph, got error: %v", name, err)
		}
	}
}

func assertSameSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %v, want %v", got, want)
	}
	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Fatalf("missing %q: got %v, want %v", w, got, want)
		}
	}
}
