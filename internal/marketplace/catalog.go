// Package marketplace is the kernel-side marketplace surface (Ticket T8 /
// gap 8): the pre-loaded catalog (embedded sample + optional operator
// extension file), the entitlement-token registry, and the trust set the
// install path verifies packages against.
//
// It is deliberately a thin kernel package: the container format lives in
// pkg/packagefmt (the neutral ADR seam), the token primitives live in
// capabilities/marketplace, and the HTTP surface lives in internal/api.
// Nothing here talks to a remote registry — remote fetch/update/publish are
// explicitly out of scope for T8 (the catalog is local, the install path
// consumes bytes already in hand).
package marketplace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/glyphux/glyphux/pkg/packagefmt"
)

// CatalogEntry is one catalog listing. Commercial entries are the ones the
// "entitlements" category surfaces — a paid extension whose updates are
// gated on a registered token. PackageBytes carries the signed container
// for installable entries (the embedded sample is listing-only — the dev
// private key that would sign real fixtures is never embedded, per the
// trust-model decision); a nil PackageBytes entry is a listing the install
// endpoint cannot act on.
type CatalogEntry struct {
	ID           string
	Name         string
	Kind         packagefmt.Kind
	Tier         string // "official" | "community"
	Version      string
	RequiresCore string
	License      string
	Commercial   bool
	PackageBytes []byte
}

// Catalog is the resolved, in-memory catalog: the embedded sample plus any
// operator catalog-file extensions. It is immutable after construction.
type Catalog struct {
	entries []CatalogEntry
	byID    map[string]CatalogEntry
}

// NewCatalog builds a Catalog from entries. Later entries with a duplicate
// ID replace earlier ones (operator files extend after the sample, so an
// operator listing wins on collision).
func NewCatalog(entries []CatalogEntry) *Catalog {
	c := &Catalog{byID: map[string]CatalogEntry{}}
	for _, e := range entries {
		c.byID[e.ID] = e
	}
	// Preserve order (deduped): rebuild entries from the seen ids in
	// first-seen order so List() is stable and collision-free.
	seen := map[string]bool{}
	c.entries = c.entries[:0]
	for _, e := range entries {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		c.entries = append(c.entries, e)
	}
	return c
}

// List returns the catalog entries in first-seen order.
func (c *Catalog) List() []CatalogEntry {
	return c.entries
}

// ByID returns the entry with the given id, if present.
func (c *Catalog) ByID(id string) (CatalogEntry, bool) {
	e, ok := c.byID[id]
	return e, ok
}

// EmbeddedSample is the catalog pre-loaded at boot on every host: free
// official plugins, free official themes (preset-kind), free community
// packages, and a commercial entry so the entitlements category is
// populated from the first boot. It is an illustrative SAMPLE — every entry
// is listing-only (nil PackageBytes), because installable containers must
// be signed by a key in the host's trust set and the dev root's private key
// is never embedded. Real installable packages arrive through the future
// registry surface (out of scope for T8).
func EmbeddedSample() []CatalogEntry {
	return []CatalogEntry{
		// Free official plugins (capability-kind).
		{ID: "glyphux-forms", Name: "Forms", Kind: packagefmt.KindPlugin, Tier: "official", Version: "1.0.0", RequiresCore: ">=0.1.0", License: "MIT"},
		{ID: "glyphux-seo", Name: "SEO", Kind: packagefmt.KindPlugin, Tier: "official", Version: "1.0.0", RequiresCore: ">=0.1.0", License: "MIT"},
		// Free official themes (preset-kind — the .gxt container).
		{ID: "glyphux-starter-theme", Name: "Starter Theme", Kind: packagefmt.KindPreset, Tier: "official", Version: "1.0.0", RequiresCore: ">=0.1.0", License: "MIT"},
		// Free community packages.
		{ID: "community-blog-bundle", Name: "Community Blog Bundle", Kind: packagefmt.KindBundle, Tier: "community", Version: "0.9.0", RequiresCore: ">=0.1.0", License: "MIT"},
		// Commercial (entitlements category populated at boot).
		{ID: "glyphux-commercial-sample", Name: "Commercial Sample Theme", Kind: packagefmt.KindPreset, Tier: "official", Version: "2.0.0", RequiresCore: ">=0.1.0", License: "proprietary", Commercial: true},
	}
}

// BuildCatalog resolves the boot catalog: the embedded sample always, plus
// the operator catalog file when operatorFile is non-empty — the file
// EXTENDS the sample, never replaces it (the locked decision). A missing
// operator file is a boot error (fail-fast: the operator pointed at a file
// that is not there).
func BuildCatalog(operatorFile string) (*Catalog, error) {
	entries := append([]CatalogEntry{}, EmbeddedSample()...)
	if operatorFile != "" {
		extra, err := LoadCatalogFile(operatorFile)
		if err != nil {
			return nil, err
		}
		entries = append(entries, extra...)
	}
	return NewCatalog(entries), nil
}

// catalogFileEntry is the on-disk JSON shape of one operator catalog
// listing: every field CatalogEntry has except PackageBytes (listing
// extension only — operators extend the listing; installable containers
// still arrive signed, from the registry surface).
type catalogFileEntry struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         packagefmt.Kind `json:"kind"`
	Tier         string          `json:"tier"`
	Version      string          `json:"version"`
	RequiresCore string          `json:"requires_core"`
	License      string          `json:"license"`
	Commercial   bool            `json:"commercial"`
}

// LoadCatalogFile parses an operator catalog extension file (a JSON array
// of listings). It extends — callers merge the result onto the embedded
// sample.
func LoadCatalogFile(path string) ([]CatalogEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("marketplace: read catalog file %s: %w", path, err)
	}
	var docs []catalogFileEntry
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("marketplace: parse catalog file %s: %w", path, err)
	}
	out := make([]CatalogEntry, 0, len(docs))
	for _, d := range docs {
		if d.ID == "" || d.Name == "" {
			return nil, errors.New("marketplace: catalog file entry requires id and name")
		}
		out = append(out, CatalogEntry{
			ID:           d.ID,
			Name:         d.Name,
			Kind:         d.Kind,
			Tier:         d.Tier,
			Version:      d.Version,
			RequiresCore: d.RequiresCore,
			License:      d.License,
			Commercial:   d.Commercial,
		})
	}
	return out, nil
}
