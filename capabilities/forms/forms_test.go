package forms_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/capabilities/forms"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// testKernel wires real content/composition stores over a fresh SQLite DB —
// this capability's real backing, exactly like a running glyphuxd, proving
// the dogfooding claim against genuine domain APIs rather than mocks.
func testKernel(t *testing.T) sdk.KernelDeps {
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

func TestManifestIsWellFormed(t *testing.T) {
	p := forms.New()
	if err := p.Manifest().Validate(); err != nil {
		t.Fatalf("expected forms's own manifest to be valid, got %v", err)
	}
}

func TestRegisterDefinesFormSubmissionContentType(t *testing.T) {
	ctx := context.Background()
	deps := testKernel(t)
	p := forms.New()

	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	comp, err := deps.Compositions.Load(ctx)
	if err != nil {
		t.Fatalf("load composition: %v", err)
	}
	if _, ok := comp.ContentTypes["form_submission"]; !ok {
		t.Fatal("expected form_submission content type to be defined after Register")
	}
}

func TestSubmitThenListRoundTripsAFormEntry(t *testing.T) {
	ctx := context.Background()
	deps := testKernel(t)
	p := forms.New()

	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	id, err := forms.Submit(ctx, host, "contact-us", map[string]any{"email": "a@example.com", "message": "hi"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if id == "" {
		t.Fatal("expected a non-empty submission id")
	}

	ids, err := forms.List(ctx, host, "contact-us")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("expected List to return exactly the submitted id, got %v", ids)
	}
}

func TestListFiltersByFormName(t *testing.T) {
	ctx := context.Background()
	deps := testKernel(t)
	p := forms.New()

	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := forms.Submit(ctx, host, "contact-us", map[string]any{"email": "a@example.com"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := forms.Submit(ctx, host, "newsletter", map[string]any{"email": "b@example.com"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ids, err := forms.List(ctx, host, "newsletter")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected only the newsletter submission, got %d entries", len(ids))
	}
}

func TestRegisterDeniedWithoutContentWriteScope(t *testing.T) {
	// A HostAPI built from a manifest that doesn't declare content:write
	// (unlike forms's own manifest) must refuse RegisterContentType — this
	// is pkg/sdk's own gate, exercised here to prove forms.Register carries
	// no bypass of its own.
	deps := testKernel(t)
	restricted := sdk.Manifest{
		Name: "forms", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
	}
	host, err := sdk.NewHostAPI(restricted, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	p := forms.New()
	if err := p.Register(host); err == nil {
		t.Fatal("expected Register to be denied against a host built without content:write")
	}
}
