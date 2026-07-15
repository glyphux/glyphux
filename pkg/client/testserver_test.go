package client_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/pkg/contract"
)

// newTestServer boots a real api.Server (real sqlite-backed domain stores,
// real HTTP routing) with an "article" content type already composed, and
// returns it as a live httptest.Server plus the identity/session services
// used to seed accounts directly where a test needs to bypass the wizard.
func newTestServer(t *testing.T) (*httptest.Server, *identity.Service, *identity.Sessions) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, media.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}

	comps := composition.NewStore(d)
	if err := comps.Save(context.Background(), &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
				"body":  {Type: contract.FieldRichText},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	identities := identity.NewService(d)
	sessions := identity.NewSessions(d)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mediaAPI := media.NewAPI(media.NewStore(d), filepath.Join(t.TempDir(), "media"))
	srv := api.New(comps, content.NewAPI(comps, content.NewStore(d)), mediaAPI, identities, sessions, log)
	mux := http.NewServeMux()
	srv.Routes(mux)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, identities, sessions
}
