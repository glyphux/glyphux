// Package seo is the PRD §14 slice 3.2 first-party capability: content-derived
// meta tags, a sitemap, and structured data (JSON-LD), generated ENTIRELY
// from existing content reached through the public sdk.HostAPI's
// sdk.ContentAPI — never a kernel-internal shortcut. It follows the same
// convention capabilities/forms (slice 2.9) established: import only
// pkg/sdk, pkg/contract, and internal/content solely for the *content.Item
// type sdk.ContentAPI's own method signatures already return to a Tier-A
// (in-process) plugin.
//
// Tier decision (see this slice's tracking doc,
// docs/implementation/active/0022-phase3-slice3.2-seo.md, for full
// rationale): the PRD tags this slice "Tier B/WASM", but the spec doc
// explicitly allows building it the same way forms was built if that is
// judged the more practical first vertical slice. seo is built as a Tier-A
// sdk.Plugin here: its entire job is deterministic string/XML/JSON
// generation from data already crossing the HostAPI boundary — there is no
// privileged operation a WASM sandbox would meaningfully constrain that
// content:read doesn't already constrain. A real WASM guest remains
// possible without changing this package's public functions' signatures.
package seo

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"strings"
	"time"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Plugin is the seo capability's sdk.Plugin implementation.
type Plugin struct{}

// New returns a seo Plugin.
func New() *Plugin { return &Plugin{} }

// Manifest declares this capability's identity and its one consent-relevant
// axis: content[read] (PRD §7.2: "seo -> content"). seo never writes
// content — it only reads existing items to derive meta tags, sitemaps, and
// structured data — so no "write"/"publish" scope and no permissions axis
// (no admin_ui/scheduled_jobs/network grant needed for this slice's scope).
func (p *Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    "seo",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read"}},
		},
	}
}

// Register validates that host was built from a manifest declaring
// content:read — seo has no content type of its own to define (unlike
// forms), so there is nothing else to set up. Failing fast here, rather
// than only at the first GenerateSitemap/GenerateMetaTags/
// GenerateStructuredData call, mirrors forms's own "no bypass of pkg/sdk's
// own gate" guarantee: a host built without content:read cannot be used by
// this capability at all.
func (p *Plugin) Register(host sdk.HostAPI) error {
	if host.Content() == nil {
		return errors.New("seo: content:read scope required to register")
	}
	return nil
}

// MetaTags is the <title>/<meta name="description">/Open Graph tag set a
// real theme/renderer would emit for one content item.
type MetaTags struct {
	Title         string
	Description   string
	Canonical     string
	OGTitle       string
	OGDescription string
	OGURL         string
	OGType        string
}

// fieldString reads a string field out of item.Data, defaulting to
// fallback if the field is absent, not a string, or empty. Content types
// are dynamic (declared per-site via contract.ContentType), so seo cannot
// require any specific field at compile time — it degrades gracefully
// instead, per this slice's tracking doc.
func fieldString(item *content.Item, field, fallback string) string {
	v, ok := item.Data[field]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return fallback
	}
	return s
}

// canonicalURL builds the public URL for item under baseURL, using its
// "slug" field if present, else falling back to the item's own ID — a
// judgment call documented in this slice's tracking doc: seo does not
// invent slugs, it only reads whatever a content type/editor already
// assigned.
func canonicalURL(item *content.Item, baseURL string) string {
	slug := fieldString(item, "slug", item.ID)
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(slug, "/")
}

// GenerateMetaTags derives the meta-tag set for one content item — a pure
// function of the item and the site's base URL, taking exactly the
// *content.Item shape sdk.ContentAPI.Get/List already return to a Tier-A
// plugin, so it can be called and tested with no HostAPI at all.
func GenerateMetaTags(item *content.Item, baseURL string) MetaTags {
	title := fieldString(item, "title", "")
	description := fieldString(item, "description", "")
	url := canonicalURL(item, baseURL)
	return MetaTags{
		Title:         title,
		Description:   description,
		Canonical:     url,
		OGTitle:       title,
		OGDescription: description,
		OGURL:         url,
		OGType:        "article",
	}
}

// Render renders m as the literal HTML tag set a theme/renderer would
// inject into a page's <head>.
func (m MetaTags) Render() string {
	var b strings.Builder
	b.WriteString("<title>" + m.Title + "</title>\n")
	b.WriteString(`<meta name="description" content="` + m.Description + `">` + "\n")
	b.WriteString(`<link rel="canonical" href="` + m.Canonical + `">` + "\n")
	b.WriteString(`<meta property="og:type" content="` + m.OGType + `">` + "\n")
	b.WriteString(`<meta property="og:title" content="` + m.OGTitle + `">` + "\n")
	b.WriteString(`<meta property="og:description" content="` + m.OGDescription + `">` + "\n")
	b.WriteString(`<meta property="og:url" content="` + m.OGURL + `">`)
	return b.String()
}

// article is the schema.org Article JSON-LD shape GenerateStructuredData
// produces. Only the fields derivable from a generic content.Item are
// populated — "author" (schema.org recommends but does not strictly
// require it for Article) is deferred; see this slice's tracking doc.
type article struct {
	Context       string `json:"@context"`
	Type          string `json:"@type"`
	Headline      string `json:"headline"`
	Description   string `json:"description"`
	DatePublished string `json:"datePublished"`
	DateModified  string `json:"dateModified"`
	URL           string `json:"url"`
}

// GenerateStructuredData derives a schema.org Article JSON-LD block for one
// content item — a pure function like GenerateMetaTags, validated
// structurally (not just "no error") by this package's own tests via
// encoding/json round-trip.
func GenerateStructuredData(item *content.Item, baseURL string) (string, error) {
	a := article{
		Context:       "https://schema.org",
		Type:          "Article",
		Headline:      fieldString(item, "title", ""),
		Description:   fieldString(item, "description", ""),
		DatePublished: item.CreatedAt.UTC().Format(time.RFC3339),
		DateModified:  item.UpdatedAt.UTC().Format(time.RFC3339),
		URL:           canonicalURL(item, baseURL),
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// sitemapURLSet/sitemapURLElem are the encoding/xml shape of a standard
// sitemap.xml document (https://www.sitemaps.org/protocol.html).
type sitemapURLSet struct {
	XMLName xml.Name         `xml:"urlset"`
	Xmlns   string           `xml:"xmlns,attr"`
	URLs    []sitemapURLElem `xml:"url"`
}

type sitemapURLElem struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

const sitemapXMLNS = "http://www.sitemaps.org/schemas/sitemap/0.9"

// GenerateSitemap lists every item of typeName through host.Content()
// (the ONLY way this function reaches content — no kernel-internal
// shortcut) and renders a well-formed sitemap.xml containing exactly the
// published items, oldest first. Draft items are excluded: a sitemap is a
// public-facing document, and content.Item.Status distinguishes drafts from
// published items on the very object ContentAPI.List already returns, so
// filtering client-side (like forms's List does for form_name) needs no
// pkg/sdk change.
func GenerateSitemap(ctx context.Context, host sdk.HostAPI, typeName, baseURL string) (string, error) {
	contentAPI := host.Content()
	if contentAPI == nil {
		return "", errors.New("seo: content:read scope required to generate a sitemap")
	}
	items, err := contentAPI.List(ctx, typeName)
	if err != nil {
		return "", err
	}

	set := sitemapURLSet{Xmlns: sitemapXMLNS}
	for _, item := range items {
		if item.Status != content.StatusPublished {
			continue
		}
		set.URLs = append(set.URLs, sitemapURLElem{
			Loc:     canonicalURL(item, baseURL),
			LastMod: item.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}

	body, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(body), nil
}
