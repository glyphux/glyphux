package consent_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/sdk"
)

func newTestEngine(t *testing.T) *consent.Engine {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), consent.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return consent.NewEngine(d)
}

func commerceManifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    "commerce",
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=1.0.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read", "write"}},
			{Capability: "payments", Scopes: []string{"charge", "refund"}},
		},
		Permissions: []sdk.Permission{
			{Name: "admin_ui"},
			{Name: "network", Args: []string{"api.stripe.com"}},
		},
	}
}

func TestRequest_ReflectsManifestAxesAndFingerprint(t *testing.T) {
	e := newTestEngine(t)
	m := commerceManifest()

	req, err := e.Request(m)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.PluginName != "commerce" || req.PluginVersion != "1.0.0" {
		t.Fatalf("unexpected identity: %+v", req)
	}
	if len(req.API) != 2 || len(req.Permissions) != 2 {
		t.Fatalf("expected request to carry manifest's full declared axes, got %+v", req)
	}
	if req.Fingerprint == "" {
		t.Fatalf("expected a non-empty fingerprint")
	}

	// Same shape, different Requires/Runtime -> same fingerprint (fingerprint
	// only covers the two consent-relevant axes, API+Permissions).
	m2 := m
	m2.Requires.Core = ">=2.0.0"
	req2, err := e.Request(m2)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req2.Fingerprint != req.Fingerprint {
		t.Fatalf("expected fingerprint to be stable across non-axis manifest fields")
	}
}

func TestRequest_RejectsInvalidManifest(t *testing.T) {
	e := newTestEngine(t)
	bad := commerceManifest()
	bad.Name = "" // invalid per sdk.Manifest.Validate

	if _, err := e.Request(bad); err == nil {
		t.Fatalf("expected error for invalid manifest")
	}
}

func TestApprove_ThenIsConsented_ReportsFullGrant(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t)
	m := commerceManifest()

	req, err := e.Request(m)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := e.Approve(ctx, req, 42); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	decision, ok, err := e.IsConsented(ctx, m)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if !ok {
		t.Fatalf("expected consented after Approve")
	}
	if decision.Status != consent.StatusApproved {
		t.Fatalf("expected StatusApproved, got %v", decision.Status)
	}
	if decision.DecidedBy != 42 {
		t.Fatalf("expected DecidedBy=42, got %d", decision.DecidedBy)
	}
	if len(decision.GrantedAPI) != len(m.API) || len(decision.GrantedPermissions) != len(m.Permissions) {
		t.Fatalf("expected full grant to mirror manifest axes, got %+v", decision)
	}
}

func TestDeny_ThenIsConsented_ReportsNotConsented(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t)
	m := commerceManifest()

	req, err := e.Request(m)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := e.Deny(ctx, req, 42); err != nil {
		t.Fatalf("Deny: %v", err)
	}

	_, ok, err := e.IsConsented(ctx, m)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if ok {
		t.Fatalf("expected not consented after Deny")
	}
}

func TestIsConsented_UnknownPlugin_ReportsNotConsented(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t)
	m := commerceManifest()

	_, ok, err := e.IsConsented(ctx, m)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if ok {
		t.Fatalf("expected not consented before any decision exists")
	}
}

func TestDecide_PartialGrant_RecordsSubsetAndPartialStatus(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t)
	m := commerceManifest()

	req, err := e.Request(m)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Admin allows content[read,write] but denies payments entirely, and
	// allows admin_ui but denies network.
	grantedAPI := []sdk.APIScope{
		{Capability: "content", Scopes: []string{"read", "write"}},
	}
	grantedPerms := []sdk.Permission{
		{Name: "admin_ui"},
	}
	decision, err := e.Decide(ctx, req, grantedAPI, grantedPerms, 7)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Status != consent.StatusPartial {
		t.Fatalf("expected StatusPartial, got %v", decision.Status)
	}

	got, ok, err := e.IsConsented(ctx, m)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if !ok {
		t.Fatalf("expected a partial grant to still count as consented (with a reduced grant)")
	}
	if len(got.GrantedAPI) != 1 || got.GrantedAPI[0].Capability != "content" {
		t.Fatalf("expected only content granted, got %+v", got.GrantedAPI)
	}
	if len(got.GrantedPermissions) != 1 || got.GrantedPermissions[0].Name != "admin_ui" {
		t.Fatalf("expected only admin_ui granted, got %+v", got.GrantedPermissions)
	}
}

func TestDecide_RejectsGrantExceedingRequest(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t)
	m := commerceManifest()

	req, err := e.Request(m)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Attempt to grant a scope never requested.
	overreach := []sdk.APIScope{
		{Capability: "content", Scopes: []string{"read", "write", "publish"}},
	}
	if _, err := e.Decide(ctx, req, overreach, nil, 1); err == nil {
		t.Fatalf("expected error granting scopes beyond what was requested")
	}

	// Attempt to grant a whole capability never requested.
	overreachCap := []sdk.APIScope{
		{Capability: "media", Scopes: []string{"read"}},
	}
	if _, err := e.Decide(ctx, req, overreachCap, nil, 1); err == nil {
		t.Fatalf("expected error granting a capability beyond the request")
	}
}

func TestReConsent_RequiredWhenManifestGainsNewCapabilityAtSameVersion(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(t)

	original := sdk.Manifest{
		Name:    "commerce",
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=1.0.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read"}},
		},
	}
	req, err := e.Request(original)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := e.Approve(ctx, req, 1); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	_, ok, err := e.IsConsented(ctx, original)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if !ok {
		t.Fatalf("expected original manifest shape to be consented")
	}

	// SAME version, but the manifest now also declares payments — a plugin
	// that re-declared a different shape under an unchanged version string.
	changed := original
	changed.API = []sdk.APIScope{
		{Capability: "content", Scopes: []string{"read"}},
		{Capability: "payments", Scopes: []string{"charge"}},
	}

	_, ok, err = e.IsConsented(ctx, changed)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if ok {
		t.Fatalf("expected a stale consent to NOT apply to a manifest that gained a new capability under the same version")
	}

	// After the admin re-approves the new shape, it becomes consented.
	newReq, err := e.Request(changed)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := e.Approve(ctx, newReq, 1); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	_, ok, err = e.IsConsented(ctx, changed)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if !ok {
		t.Fatalf("expected the new shape to be consented after re-approval")
	}

	// And the OLD shape is no longer consented either, since it's a
	// different fingerprint from what's now on file... but re-requesting the
	// exact original shape again should still show its own still-valid
	// approval (each fingerprint's decision is independent).
	_, ok, err = e.IsConsented(ctx, original)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if !ok {
		t.Fatalf("expected the original shape's own prior approval to remain valid under its own fingerprint")
	}
}

func TestDecision_PersistsAcrossEngineInstances(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	d1, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := d1.Migrate(ctx, consent.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	e1 := consent.NewEngine(d1)
	m := commerceManifest()
	req, err := e1.Request(m)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := e1.Approve(ctx, req, 99); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	d1.Close()

	d2, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	t.Cleanup(func() { d2.Close() })
	e2 := consent.NewEngine(d2)

	decision, ok, err := e2.IsConsented(ctx, m)
	if err != nil {
		t.Fatalf("IsConsented: %v", err)
	}
	if !ok {
		t.Fatalf("expected a fresh Engine instance backed by the same database to see the prior decision")
	}
	if decision.DecidedBy != 99 {
		t.Fatalf("expected persisted DecidedBy=99, got %d", decision.DecidedBy)
	}
}
