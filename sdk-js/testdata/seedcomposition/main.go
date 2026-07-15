// Command seedcomposition is a test-fixture helper for the JS SDK's
// integration suite (sdk-js/). It is not part of the product: the daemon
// currently exposes no HTTP-level way to declare content types after
// first-run setup completes (the wizard's own form only collects site name,
// admin credentials, and the database choice — see internal/setup/setup.go).
// The Go-level test suite works around this by seeding the composition
// directly via internal/composition.Store.Save (see
// internal/api/auth_test.go testServerWithAuth); this program does the same
// thing for the JS SDK's black-box tests, using the same sanctioned domain
// API rather than touching the database's tables directly.
//
// It is run once, against a stopped daemon's SQLite file, between the
// first-run /setup HTTP call (which the JS test harness performs for real,
// per the confirmed seam) and starting the long-lived glyphuxd instance the
// SDK tests actually exercise. See sdk-js/test/global-setup.ts.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seedcomposition:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: seedcomposition <sqlite-path>")
	}
	sqlitePath := os.Args[1]

	database, err := db.OpenSQLite(sqlitePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", sqlitePath, err)
	}
	defer database.Close()

	compositions := composition.NewStore(database)
	ctx := context.Background()

	comp, err := compositions.Load(ctx)
	if err != nil {
		return fmt.Errorf("load composition: %w", err)
	}

	if comp.ContentTypes == nil {
		comp.ContentTypes = map[string]contract.ContentType{}
	}
	comp.ContentTypes["post"] = contract.ContentType{
		Fields: map[string]contract.Field{
			"title":      {Type: contract.FieldString, Required: true},
			"body":       {Type: contract.FieldString},
			"featured":   {Type: contract.FieldBoolean},
			"hero_image": {Type: contract.FieldMedia},
		},
	}

	if err := compositions.Save(ctx, comp); err != nil {
		return fmt.Errorf("save composition: %w", err)
	}
	fmt.Println("seeded content type \"post\"")
	return nil
}
