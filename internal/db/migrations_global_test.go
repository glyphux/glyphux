package db_test

import (
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/internal/preset"
)

// TestMigrationVersionsAreGloballyUnique is the guard against each package
// numbering its own migrations independently — a scheme that has bitten this
// repo twice already (media's 8 collided with identity/mfa's original 8;
// then mfa's replacement 12 collided with consent's reservation, per the
// documented history in internal/media/store.go and internal/consent/store.go).
//
// cmd/glyphuxd/main.go concatenates every package's Migrations slice into ONE
// ordered set, and internal/db/migrate.go's Migrate rejects duplicate
// versions across the whole applied set — so a duplicate is a boot-time
// database error the moment the second package is wired in. This test turns
// that into an immediate, package-naming test failure instead. Every package
// exposing a Migrations slice must be listed here; adding a new one without
// adding it to this list is the same bug this test exists to catch.
//
// The set of packages is deliberately exhaustive: composition, identity
// (whose slice is grown by init()s in identity/mfa.go, oauth.go, sessions.go
// and users_manage.go — importing identity.Migrations pulls those in),
// content, media, consent, audit, layout, preset, bundle, pluginstore.
func TestMigrationVersionsAreGloballyUnique(t *testing.T) {
	sets := []struct {
		pkg  string
		migs []db.Migration
	}{
		{"composition", composition.Migrations},
		{"identity", identity.Migrations},
		{"content", content.Migrations},
		{"media", media.Migrations},
		{"consent", consent.Migrations},
		{"audit", audit.Migrations},
		{"layout", layout.Migrations},
		{"preset", preset.Migrations},
		{"bundle", bundle.Migrations},
		{"pluginstore", pluginstore.Migrations},
	}
	seen := map[int]string{}
	for _, set := range sets {
		for _, m := range set.migs {
			if prev, ok := seen[m.Version]; ok {
				t.Errorf("migration version %d collides: %s and %s both declare it (versions are global across all packages)", m.Version, prev, set.pkg)
			} else {
				seen[m.Version] = set.pkg
			}
		}
	}
}
