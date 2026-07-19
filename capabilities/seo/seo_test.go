package seo_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/capabilities/seo"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// adminPrincipal is used only to seed real content fixtures directly through
// content.API (the way an editor/admin plugin would, not the seo capability
// itself) — seo only ever reads through its own scoped host, exactly like
// capabilities/forms's tests seed via the raw store/API and exercise the
// capability under test only through sdk.HostAPI.
var adminPrincipal = &permission.Principal{Role: permission.RoleAdmin}

const articleType = "article"

// testKernel wires real content/composition stores over a fresh SQLite DB,
// registers an "article" content type with title/description/slug fields,
// and returns the KernelDeps a real host is built from — mirrors
// capabilities/forms's testKernel helper.
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
		ContentTypes: map[string]contract.ContentType{
			articleType: {
				Fields: map[string]contract.Field{
					"title":       {Type: contract.FieldString, Required: true},
					"description": {Type: contract.FieldString},
					"slug":        {Type: contract.FieldString},
				},
			},
		},
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}
	return sdk.KernelDeps{
		Compositions: comps,
		Content:      content.NewAPI(comps, content.NewStore(d)),
	}
}

// seoHost builds a real HostAPI scoped exactly the way seo.Plugin's own
// Manifest declares (content:read only) — the capability under test never
// gets more access than it asks for.
func seoHost(t *testing.T, deps sdk.KernelDeps) sdk.HostAPI {
	t.Helper()
	p := seo.New()
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return host
}

// createPublished seeds one published article directly through content.API
// (standing in for an editor plugin/admin action, not seo's own writes) and
// returns the resulting item.
func createPublished(t *testing.T, deps sdk.KernelDeps, data map[string]any) *content.Item {
	t.Helper()
	item, err := deps.Content.Create(context.Background(), adminPrincipal, articleType, data)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	item, err = deps.Content.Publish(context.Background(), adminPrincipal, articleType, item.ID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return item
}

func TestManifestIsWellFormed(t *testing.T) {
	p := seo.New()
	if err := p.Manifest().Validate(); err != nil {
		t.Fatalf("expected seo's own manifest to be valid, got %v", err)
	}
}

func TestRegisterDeniedWithoutContentReadScope(t *testing.T) {
	deps := testKernel(t)
	restricted := sdk.Manifest{
		Name: "seo", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
	}
	host, err := sdk.NewHostAPI(restricted, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	p := seo.New()
	if err := p.Register(host); err == nil {
		t.Fatal("expected Register to be denied against a host built without content:read")
	}
}

func TestGenerateMetaTagsProducesTitleDescriptionAndOpenGraphTags(t *testing.T) {
	item := &content.Item{
		ID:   "abc123",
		Type: articleType,
		Data: map[string]any{
			"title":       "How Glyphux Works",
			"description": "An overview of the Glyphux content engine.",
			"slug":        "how-glyphux-works",
		},
	}

	tags := seo.GenerateMetaTags(item, "https://example.com")

	if tags.Title != "How Glyphux Works" {
		t.Errorf("Title = %q, want %q", tags.Title, "How Glyphux Works")
	}
	if tags.Description != "An overview of the Glyphux content engine." {
		t.Errorf("Description = %q, want the item's description", tags.Description)
	}
	wantURL := "https://example.com/how-glyphux-works"
	if tags.Canonical != wantURL {
		t.Errorf("Canonical = %q, want %q", tags.Canonical, wantURL)
	}
	if tags.OGTitle != tags.Title {
		t.Errorf("OGTitle = %q, want to match Title %q", tags.OGTitle, tags.Title)
	}
	if tags.OGDescription != tags.Description {
		t.Errorf("OGDescription = %q, want to match Description", tags.OGDescription)
	}
	if tags.OGURL != wantURL {
		t.Errorf("OGURL = %q, want %q", tags.OGURL, wantURL)
	}
	if tags.OGType == "" {
		t.Error("expected a non-empty OGType")
	}

	rendered := tags.Render()
	for _, want := range []string{
		"<title>How Glyphux Works</title>",
		`<meta name="description" content="An overview of the Glyphux content engine.">`,
		`<meta property="og:title" content="How Glyphux Works">`,
		`<meta property="og:description" content="An overview of the Glyphux content engine.">`,
		`<meta property="og:url" content="` + wantURL + `">`,
		`<link rel="canonical" href="` + wantURL + `">`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered meta tags missing %q\ngot:\n%s", want, rendered)
		}
	}
}

func TestGenerateMetaTagsFallsBackToItemIDWhenSlugMissing(t *testing.T) {
	item := &content.Item{
		ID:   "no-slug-id",
		Type: articleType,
		Data: map[string]any{"title": "Untitled Slug Case"},
	}

	tags := seo.GenerateMetaTags(item, "https://example.com")

	want := "https://example.com/no-slug-id"
	if tags.Canonical != want {
		t.Errorf("Canonical = %q, want fallback to item id %q", tags.Canonical, want)
	}
}

func TestGenerateStructuredDataProducesValidArticleJSONLD(t *testing.T) {
	item := &content.Item{
		ID:   "abc123",
		Type: articleType,
		Data: map[string]any{
			"title":       "How Glyphux Works",
			"description": "An overview of the Glyphux content engine.",
			"slug":        "how-glyphux-works",
		},
	}

	raw, err := seo.GenerateStructuredData(item, "https://example.com")
	if err != nil {
		t.Fatalf("GenerateStructuredData: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("expected valid JSON-LD, got unmarshal error: %v\nraw: %s", err, raw)
	}

	if doc["@context"] != "https://schema.org" {
		t.Errorf(`@context = %v, want "https://schema.org"`, doc["@context"])
	}
	if doc["@type"] != "Article" {
		t.Errorf(`@type = %v, want "Article"`, doc["@type"])
	}
	for _, field := range []string{"headline", "description", "datePublished", "dateModified", "url"} {
		v, ok := doc[field]
		if !ok {
			t.Errorf("missing required Article field %q", field)
			continue
		}
		if s, ok := v.(string); !ok || s == "" {
			t.Errorf("field %q = %v, want a non-empty string", field, v)
		}
	}
	if doc["headline"] != "How Glyphux Works" {
		t.Errorf(`headline = %v, want "How Glyphux Works"`, doc["headline"])
	}
	if doc["url"] != "https://example.com/how-glyphux-works" {
		t.Errorf(`url = %v, want "https://example.com/how-glyphux-works"`, doc["url"])
	}
}

// sitemapURLSet/sitemapURLElem mirror the shape of a real sitemap.xml
// (https://www.sitemaps.org/protocol.html) so the test parses the generated
// document back with encoding/xml rather than eyeballing it or grepping the
// string.
type sitemapURLSet struct {
	XMLName xml.Name         `xml:"urlset"`
	URLs    []sitemapURLElem `xml:"url"`
}

type sitemapURLElem struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

func TestGenerateSitemapProducesWellFormedXMLForPublishedItems(t *testing.T) {
	deps := testKernel(t)
	host := seoHost(t, deps)

	published1 := createPublished(t, deps, map[string]any{"title": "Post One", "slug": "post-one"})
	published2 := createPublished(t, deps, map[string]any{"title": "Post Two", "slug": "post-two"})

	// A draft article must NOT appear in the sitemap.
	if _, err := deps.Content.Create(context.Background(), adminPrincipal, articleType, map[string]any{"title": "Draft Post", "slug": "draft-post"}); err != nil {
		t.Fatalf("Create draft: %v", err)
	}

	raw, err := seo.GenerateSitemap(context.Background(), host, articleType, "https://example.com")
	if err != nil {
		t.Fatalf("GenerateSitemap: %v", err)
	}

	var set sitemapURLSet
	if err := xml.Unmarshal([]byte(raw), &set); err != nil {
		t.Fatalf("expected well-formed sitemap XML, got unmarshal error: %v\nraw: %s", err, raw)
	}

	if len(set.URLs) != 2 {
		t.Fatalf("expected exactly 2 <url> entries (published only), got %d: %+v", len(set.URLs), set.URLs)
	}

	gotLocs := map[string]bool{}
	for _, u := range set.URLs {
		gotLocs[u.Loc] = true
		if u.LastMod == "" {
			t.Errorf("expected a non-empty <lastmod> for %q", u.Loc)
		}
	}
	wantLoc1 := "https://example.com/post-one"
	wantLoc2 := "https://example.com/post-two"
	if !gotLocs[wantLoc1] {
		t.Errorf("expected sitemap to contain %q, got %v", wantLoc1, gotLocs)
	}
	if !gotLocs[wantLoc2] {
		t.Errorf("expected sitemap to contain %q, got %v", wantLoc2, gotLocs)
	}
	if gotLocs["https://example.com/draft-post"] {
		t.Error("draft item must not appear in the sitemap")
	}

	if !strings.Contains(raw, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("expected an XML declaration header")
	}
	if !strings.Contains(raw, `xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"`) {
		t.Error("expected the standard sitemap.org xmlns")
	}

	_ = published1
	_ = published2
}

func TestGenerateSitemapDeniedWithoutContentReadScope(t *testing.T) {
	deps := testKernel(t)
	restricted := sdk.Manifest{
		Name: "seo", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
	}
	host, err := sdk.NewHostAPI(restricted, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}

	if _, err := seo.GenerateSitemap(context.Background(), host, articleType, "https://example.com"); err == nil {
		t.Fatal("expected GenerateSitemap to fail against a host without content:read (host.Content() is nil)")
	}
}
