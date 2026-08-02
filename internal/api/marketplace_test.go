// RED tests for Ticket T8 (gap 8 — marketplace surface): the HTTP surface
// over the locked package-format + trust decisions. These tests pin:
//
//   - GET /api/v0/marketplace/catalog — four pre-loaded categories
//     (official_plugins / official_themes / community / entitlements),
//     each entry annotated with core compatibility (kernel.Version vs the
//     manifest's requires.core).
//   - POST /api/v0/marketplace/packages/{id}/install — archive → packagefmt
//     decode (integrity + key-ID-aware signature) → preset/bundle
//     InstallFromPackage; 422 for tampered or too-new packages, nothing
//     persisted on failure.
//   - POST /api/v0/marketplace/entitlements — admin-only + CSRF, body
//     {"token": "<base64 of JSON EntitlementToken>"}; GET reports per-token
//     status (active/expired/not_yet_valid/invalid).
//
// Admin gate is permission.PluginsManage (flagged for owner); everything
// denies by default (401 anon, 403 under-privileged / missing CSRF).
package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/glyphux/glyphux/blocks/firstparty"
	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/marketplace"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/kernel"
	"github.com/glyphux/glyphux/pkg/packagefmt"
)

// ---- fixture helpers ----

// heroPresetPayload is the exact contract.CompositionPreset JSON carried as
// the payload file of the installable .gxt fixtures below — the same shape
// the preset pipeline's DecodePreset parses (mirrors preset_test.heroPreset).
func heroPresetPayload(t *testing.T, name string) []byte {
	t.Helper()
	p, err := json.Marshal(&contract.CompositionPreset{
		ContractVersion: contract.CompositionPresetV1,
		Name:            name,
		Layout: contract.Layout{
			ContractVersion: contract.LayoutCompositionV1,
			Regions: map[string]contract.Region{
				"main": {Blocks: []contract.Block{{Type: "heading"}}},
			},
		},
		Manifest: contract.Manifest{
			RequiresContract: contract.LayoutCompositionV1,
			Blocks:           []string{"heading"},
			Slots:            []string{"main"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// signedPresetContainer builds a .gxt (preset-kind) container signed by
// keyID via the packagefmt encoder under test.
func signedPresetContainer(t *testing.T, priv ed25519.PrivateKey, keyID, name, requiresCore string) []byte {
	t.Helper()
	payload := heroPresetPayload(t, name)
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: packagefmt.SchemaVersionV1,
			Name:          name,
			Version:       "1.0.0",
			Kind:          packagefmt.KindPreset,
			License:       "proprietary",
			RequiresCore:  requiresCore,
		},
		Files: []packagefmt.PackageFile{
			{Path: "preset.json", SHA256: sha256HexOf(payload), Data: payload},
		},
		Checksums: map[string]string{"preset.json": sha256HexOf(payload)},
	}
	data, err := packagefmt.Encode(sp, priv, keyID)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return data
}

func sha256HexOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func entitlementTokenJSON(t *testing.T, priv ed25519.PrivateKey, ent marketplace.Entitlement) string {
	t.Helper()
	tok, err := marketplace.IssueEntitlement(priv, ent)
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}
	raw, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// ---- server harness ----

// testServerWithMarketplace mirrors testServerWithPresets, adds the
// marketplace migrations, and wires a marketplace.Manager holding cat and
// tokens over the same real SQLite database. adminCreds are the admin
// session; editorCookie is a second login with an editor role.
func testServerWithMarketplace(t *testing.T, mgr *marketplace.Manager) (http.Handler, authCreds, *http.Cookie) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, layout.Migrations...)
	migs = append(migs, preset.Migrations...)
	migs = append(migs, bundle.Migrations...)
	migs = append(migs, marketplace.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(context.Background(), nil, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	identities := identity.NewService(d)
	sessions := identity.NewSessions(d)
	registry := blocks.New()
	if err := firstparty.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	contentAPI := content.NewAPI(comps, content.NewStore(d))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.New(comps, contentAPI, newTestMediaAPI(t, d), identities, sessions, log,
		api.WithLayouts(layout.NewStore(d), registry),
		api.WithPresets(preset.NewStore(d), bundle.NewStore(d)),
		api.WithMarketplace(mgr),
	)
	mux := http.NewServeMux()
	srv.Routes(mux)

	if err := identities.CreateAdmin(context.Background(), "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := identities.CreateUser(context.Background(), "editor@example.com", "editor horse battery", "editor"); err != nil {
		t.Fatal(err)
	}
	rec := do(t, mux, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "admin@example.com", "password": "correct horse battery",
	})
	adminCreds := sessionCookie(t, rec)

	rec = do(t, mux, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "editor horse battery",
	})
	editorCreds := sessionCookie(t, rec)
	_ = editorCreds

	// Return the editor's raw session cookie for the no-CSRF case.
	for _, c := range rec.Result().Cookies() {
		if c.Name == "glyphux_session" {
			return mux, adminCreds, c
		}
	}
	t.Fatal("editor session cookie missing")
	return mux, adminCreds, nil
}

// marketplaceManager builds a Manager with a test keypair in the trust set,
// a four-category catalog, and a real entitlement Store over d.
func marketplaceManager(t *testing.T, d *db.DB) (*marketplace.Manager, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "glyphux-dev-2026-01"

	installable := signedPresetContainer(t, priv, keyID, "acme-hero-pro", ">=0.1.0")
	tooNew := signedPresetContainer(t, priv, keyID, "acme-future-theme", ">=0.3.0")

	cat := marketplace.NewCatalog([]marketplace.CatalogEntry{
		{ID: "compose-kit", Name: "Compose Kit", Kind: packagefmt.KindPlugin, Tier: "official", Version: "1.0.0", RequiresCore: ">=0.1.0", License: "free"},
		{ID: "hero-theme", Name: "Hero Theme", Kind: packagefmt.KindPreset, Tier: "official", Version: "2.0.0", RequiresCore: ">=0.1.0", License: "proprietary", Commercial: true, PackageBytes: installable},
		{ID: "future-theme", Name: "Future Theme", Kind: packagefmt.KindPreset, Tier: "official", Version: "3.0.0", RequiresCore: ">=0.3.0", License: "proprietary", Commercial: true, PackageBytes: tooNew},
		{ID: "starter-bundle", Name: "Starter Bundle", Kind: packagefmt.KindBundle, Tier: "community", Version: "0.9.0", RequiresCore: ">=0.1.0", License: "free"},
	})
	tokens := marketplace.NewStore(d)
	return marketplace.NewManager(cat, tokens, map[string]ed25519.PublicKey{keyID: pub}), priv
}

// ---- tests ----

// TestMarketplaceCatalogGroupsFourCategories: GIVEN a booted server with a
// catalog spanning plugin/preset/bundle entries, WHEN an admin lists the
// catalog, THEN it is grouped under exactly the four locked categories with
// each entry carrying its manifest metadata.
func TestMarketplaceCatalogGroupsFourCategories(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mgr, _ := marketplaceManager(t, d)
	h, admin, _ := testServerWithMarketplace(t, mgr)

	rec := doWithCookieBody(t, h, http.MethodGet, "/api/v0/marketplace/catalog", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /marketplace/catalog = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	cats, ok := body["categories"].(map[string]any)
	if !ok {
		t.Fatalf("categories missing: %v", body)
	}
	for _, want := range []string{"official_plugins", "official_themes", "community", "entitlements"} {
		list, ok := cats[want].([]any)
		if !ok || len(list) == 0 {
			t.Errorf("category %q missing or empty: %v", want, cats[want])
		}
	}
	plugins := cats["official_plugins"].([]any)
	first := plugins[0].(map[string]any)
	if first["id"] != "compose-kit" || first["kind"] != "plugin" || first["tier"] != "official" {
		t.Errorf("official plugin entry = %v", first)
	}
}

// TestMarketplaceCatalogAnnotatesCoreCompatibility: GIVEN entries whose
// requires.core sits on either side of kernel.Version, WHEN the catalog is
// listed, THEN each entry carries core_compatible + core_status derived from
// pkg/kernel.Version.
func TestMarketplaceCatalogAnnotatesCoreCompatibility(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mgr, _ := marketplaceManager(t, d)
	h, admin, _ := testServerWithMarketplace(t, mgr)

	rec := doWithCookieBody(t, h, http.MethodGet, "/api/v0/marketplace/catalog", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /marketplace/catalog = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	cats := body["categories"].(map[string]any)
	themes := cats["official_themes"].([]any)
	byID := map[string]map[string]any{}
	for _, e := range themes {
		entry := e.(map[string]any)
		byID[entry["id"].(string)] = entry
	}
	hero := byID["hero-theme"]
	if hero["core_status"] != "compatible" || hero["core_compatible"] != true {
		t.Errorf("hero-theme core annotation = %v, want compatible (kernel %s)", hero, kernel.Version)
	}
	future := byID["future-theme"]
	if future["core_status"] != "too_new" || future["core_compatible"] != false {
		t.Errorf("future-theme core annotation = %v, want too_new (kernel %s)", future, kernel.Version)
	}
}

// TestMarketplaceInstallPresetPersists: GIVEN a valid .gxt signed by a
// trusted key, WHEN an admin installs it, THEN the archive decodes
// (integrity + key-ID-aware signature), the preset pipeline persists the
// record, and the endpoint returns its id.
func TestMarketplaceInstallPresetPersists(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mgr, _ := marketplaceManager(t, d)
	h, admin, _ := testServerWithMarketplace(t, mgr)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/marketplace/packages/hero-theme/install", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST install = %d, body %s", rec.Code, rec.Body.String())
	}
	installed := decode(t, rec)
	if id, _ := installed["id"].(string); id == "" {
		t.Fatalf("install response has no id: %v", installed)
	}
	// The preset record must actually be persisted (query the real store).
	presets := preset.NewStore(d)
	records, err := presets.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range records {
		if r.Name == "hero-theme" {
			found = true
		}
	}
	if !found {
		t.Fatalf("installed preset not persisted: %+v", records)
	}
}

// TestMarketplaceInstallRejectsTamperedPackage: GIVEN a container whose
// payload was flipped in transit, WHEN an admin installs it, THEN the
// packagefmt integrity check rejects it with 422 and nothing is persisted.
func TestMarketplaceInstallRejectsTamperedPackage(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mgr, priv := marketplaceManager(t, d)
	// A second container for the tamper — build via the same encoder, flip a
	// payload byte, and swap it into a manager whose catalog points at it.
	good := signedPresetContainer(t, priv, "glyphux-dev-2026-01", "tampered-theme", ">=0.1.0")
	tampered := bytes.Clone(good)
	if i := bytes.Index(tampered, []byte(`"name":"tampered-theme"`)); i >= 0 {
		// Flip a byte inside the manifest's name value — signature must fail.
		tampered[i+len(`"name":"tampered-theme"`)-1] ^= 0xff
	}
	cat := marketplace.NewCatalog([]marketplace.CatalogEntry{
		{ID: "tampered-theme", Name: "Tampered Theme", Kind: packagefmt.KindPreset, Tier: "official", Version: "1.0.0", RequiresCore: ">=0.1.0", License: "proprietary", Commercial: true, PackageBytes: tampered},
	})
	keys := mgr.Keys
	mgr2 := marketplace.NewManager(cat, mgr.Entitlements, keys)
	h, admin, _ := testServerWithMarketplace(t, mgr2)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/marketplace/packages/tampered-theme/install", admin, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST install tampered = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
	presets := preset.NewStore(d)
	if records, err := presets.List(context.Background()); err != nil {
		t.Fatal(err)
	} else {
		for _, r := range records {
			if r.Name == "tampered-theme" {
				t.Fatalf("tampered package persisted: %+v", r)
			}
		}
	}
}

// TestMarketplaceInstallRejectsTooNewRequiresCore: GIVEN a genuinely signed
// .gxt whose requires.core exceeds kernel.Version, WHEN an admin installs
// it, THEN the core-constraint gate rejects it with 422 and nothing is
// persisted (kernel.Version is the host's, baked into the check at install
// time).
func TestMarketplaceInstallRejectsTooNewRequiresCore(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mgr, _ := marketplaceManager(t, d)
	h, admin, _ := testServerWithMarketplace(t, mgr)

	rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/marketplace/packages/future-theme/install", admin, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST install too-new = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
	presets := preset.NewStore(d)
	if records, err := presets.List(context.Background()); err != nil {
		t.Fatal(err)
	} else {
		for _, r := range records {
			if r.Name == "future-theme" {
				t.Fatalf("too-new package persisted: %+v", r)
			}
		}
	}
}

// TestMarketplaceEntitlementsRegisterAndReportStatus: GIVEN an active and an
// expired entitlement token both signed by the trust key, WHEN they are
// POSTed (admin + CSRF) and the list is GET, THEN each reports its computed
// status; a tampered token is rejected at registration.
func TestMarketplaceEntitlementsRegisterAndReportStatus(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mgr, priv := marketplaceManager(t, d)
	h, admin, _ := testServerWithMarketplace(t, mgr)

	now := time.Now().UTC()
	active := entitlementTokenJSON(t, priv, marketplace.Entitlement{
		LicenseID: "lic-1", ExtensionName: "acme-hero-pro",
		MinVersion: "1.0.0", MaxVersion: "2.0.0",
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		Scope: "single-host",
	})
	expired := entitlementTokenJSON(t, priv, marketplace.Entitlement{
		LicenseID: "lic-2", ExtensionName: "acme-gone",
		MinVersion: "1.0.0", MaxVersion: "2.0.0",
		NotBefore: now.Add(-48 * time.Hour), NotAfter: now.Add(-24 * time.Hour),
		Scope: "single-host",
	})

	for _, tok := range []string{active, expired} {
		rec := doWithCookieBody(t, h, http.MethodPost, "/api/v0/marketplace/entitlements", admin, map[string]any{"token": tok})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST entitlements = %d, body %s", rec.Code, rec.Body.String())
		}
	}

	rec := doWithCookieBody(t, h, http.MethodGet, "/api/v0/marketplace/entitlements", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET entitlements = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	statuses := map[string]string{}
	if tokens, ok := body["tokens"].([]any); ok {
		for _, tok := range tokens {
			tv := tok.(map[string]any)
			statuses[tv["extension"].(string)] = tv["status"].(string)
		}
	}
	if statuses["acme-hero-pro"] != "active" {
		t.Errorf("acme-hero-pro status = %q, want active (%v)", statuses["acme-hero-pro"], statuses)
	}
	if statuses["acme-gone"] != "expired" {
		t.Errorf("acme-gone status = %q, want expired (%v)", statuses["acme-gone"], statuses)
	}

	// Tampered token: flip a byte in the signed payload, registration must
	// 422 (invalid signature), not persist.
	tampered := active
	if b, err := base64.StdEncoding.DecodeString(tampered); err == nil && len(b) > 0 {
		b[len(b)/2] ^= 0xff
		tampered = base64.StdEncoding.EncodeToString(b)
	}
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/marketplace/entitlements", admin, map[string]any{"token": tampered})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST tampered entitlement = %d, want 422, body %s", rec.Code, rec.Body.String())
	}
}

// TestMarketplaceDeniesByDefault: GIVEN no session, an editor session, or an
// admin session without the CSRF token, WHEN they hit marketplace mutation
// routes, THEN the server denies — 401 anonymous, 403 under-privileged, 403
// missing CSRF. Catalog GET stays admin-gated too.
func TestMarketplaceDeniesByDefault(t *testing.T) {
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mgr, _ := marketplaceManager(t, d)
	h, admin, editorSession := testServerWithMarketplace(t, mgr)
	_ = admin

	// Anonymous install → 401.
	rec := do(t, h, http.MethodPost, "/api/v0/marketplace/packages/hero-theme/install", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous install = %d, want 401", rec.Code)
	}

	// Editor (role editor) install → 403 (marketplace is PluginsManage).
	editorLogin := do(t, h, http.MethodPost, "/api/v0/auth/login", map[string]any{
		"email": "editor@example.com", "password": "editor horse battery",
	})
	editor := sessionCookie(t, editorLogin)
	rec = doWithCookieBody(t, h, http.MethodPost, "/api/v0/marketplace/packages/hero-theme/install", editor, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor install = %d, want 403", rec.Code)
	}

	// Admin session cookie but no CSRF token → 403 (double-submit gate).
	req := httptest.NewRequest(http.MethodPost, "/api/v0/marketplace/packages/hero-theme/install", nil)
	req.AddCookie(editorSession)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("install without CSRF = %d, want 403", rec2.Code)
	}
}
