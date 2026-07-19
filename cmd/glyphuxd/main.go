// Command glyphuxd is the Glyphux server daemon — the primary entrypoint
// (§5.3). It boots the kernel, runs migrations, serves the first-run web
// wizard on first boot, then the running platform. Single static binary,
// embedded SQLite, no required cloud (Principle 9).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/glyphux/glyphux/blocks/firstparty"
	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/bootstrap"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/graphql"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/server"
	"github.com/glyphux/glyphux/internal/setup"
	"github.com/glyphux/glyphux/pkg/blocks"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "glyphuxd:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to JSON config file (optional)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Schema baseline: idempotent, ordered, recorded (slice 0.3).
	migrations := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migrations = append(migrations, content.Migrations...)
	migrations = append(migrations, media.Migrations...)
	migrations = append(migrations, layout.Migrations...)

	boot, err := bootstrap.Boot(ctx, bootstrap.Options{
		DataDir:           cfg.DataDir,
		SQLitePath:        cfg.SQLitePath(),
		DatabaseExplicit:  databaseExplicit(cfg),
		Database:          cfg.Database,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
		Migrations:        migrations,
		Log:               log,
		BuildFullHandler:  buildFullHandler(cfg, log),
	})
	if err != nil {
		return err
	}
	defer boot.Database.Close()

	srv := server.New(cfg.Addr, boot.Handler, log)
	if cfg.OpenBrowser {
		go func() {
			maybeOpenBrowser(true, "http://"+srv.Addr()+"/", openBrowser, log)
		}()
	}
	return srv.Run(ctx, cfg.ShutdownTimeout)
}

// databaseExplicit reports whether the operator explicitly chose a database
// driver via config file or GLYPHUX_DB_DRIVER, as opposed to relying on
// bootstrap's own decision (a persisted choice from a prior setup, or the
// wizard). It must key off the driver alone, not whether a DSN happens to
// be set: after a Postgres wizard setup, the operator is told to export
// only GLYPHUX_DB_DSN for the *persisted* driver choice to keep working —
// that DSN-only case must fall through to bootstrap's persisted-config
// path, which is what actually knows to combine the recorded driver with
// the env var. Treating DSN presence alone as "explicit" here would
// silently reopen the default SQLite store instead (Principle 10: explicit
// configuration wins, but an incomplete one must not masquerade as one).
func databaseExplicit(cfg config.Config) bool {
	return cfg.Database.Driver != config.Default().Database.Driver
}

// buildFullHandler assembles the daemon's complete route table (the API
// transport plus the wizard) once bootstrap has decided which database to
// serve from — the domain APIs bootstrap itself has no need to know about
// (content, media, sessions) are wired up here.
func buildFullHandler(cfg config.Config, log *slog.Logger) bootstrap.BuildFullHandlerFunc {
	return func(database *db.DB, compositions *composition.Store, identities *identity.Service, wizard *setup.Wizard) (http.Handler, error) {
		sessions := identity.NewSessions(database)
		contentAPI := content.NewAPI(compositions, content.NewStore(database))
		mediaAPI := media.NewAPI(media.NewStore(database), filepath.Join(cfg.DataDir, "media"))

		// Layer-2 block/layout transport (slice 4.4a). The registry is
		// populated with the first-party blocks at construction time — this
		// is the first slice to actually wire blocks/firstparty into the
		// running daemon (slices 4.1/4.2 deliberately left it unwired; see
		// docs/implementation/completed/0026's "No daemon bootstrap wiring"
		// risk note). RegisterAll can only fail on a duplicate/empty name,
		// which would be a first-party programming bug, not an operator
		// misconfiguration — fatal at startup like any other invariant
		// violation this daemon can't recover from.
		blockRegistry := blocks.New()
		if err := firstparty.RegisterAll(blockRegistry); err != nil {
			return nil, fmt.Errorf("register first-party blocks: %w", err)
		}
		layoutStore := layout.NewStore(database)

		apiOpts := []api.Option{
			api.TrustProxyHeaders(cfg.TrustProxyHeaders),
			api.WithLayouts(layoutStore, blockRegistry),
		}
		if oauthMgr := githubOAuthManager(cfg); oauthMgr != nil {
			apiOpts = append(apiOpts, api.WithOAuth(oauthMgr, publicURL(cfg)))
		}
		apiServer := api.New(compositions, contentAPI, mediaAPI, identities, sessions, log, apiOpts...)
		// GraphQL (slice 1.12) is a second transport over the same domain
		// APIs the REST apiServer above was just built from — not a new
		// privileged path.
		graphqlResolver := graphql.NewResolver(compositions, contentAPI, mediaAPI, identities, sessions, log)
		graphqlHandler := graphql.NewHandler(graphqlResolver)
		return server.Handler(apiServer, wizard, server.WithGraphQL(graphqlHandler), server.WithCORS(cfg.AllowedOrigins)), nil
	}
}

// githubOAuthURLs are GitHub's real, fixed OAuth2 endpoints — see
// internal/identity/oauth.go's doc comment for why GitHub was chosen as
// this slice's provider.
const (
	githubAuthURL     = "https://github.com/login/oauth/authorize"
	githubTokenURL    = "https://github.com/login/oauth/access_token"
	githubUserInfoURL = "https://api.github.com/user"
	githubEmailsURL   = "https://api.github.com/user/emails"
)

// githubOAuthManager builds an identity.OAuthManager wired to real GitHub
// endpoints if the operator registered a GitHub OAuth App and set its
// credentials (GLYPHUX_OAUTH_GITHUB_CLIENT_ID/_SECRET); returns nil
// (leaving OAuth login routes 404ing) otherwise — OAuth is opt-in server
// configuration, same as MFA is opt-in per account.
func githubOAuthManager(cfg config.Config) *identity.OAuthManager {
	if cfg.OAuth.GitHubClientID == "" || cfg.OAuth.GitHubClientSecret == "" {
		return nil
	}
	return identity.NewOAuthManager(nil, identity.OAuthProvider{
		Name:         "github",
		ClientID:     cfg.OAuth.GitHubClientID,
		ClientSecret: cfg.OAuth.GitHubClientSecret,
		AuthURL:      githubAuthURL,
		TokenURL:     githubTokenURL,
		UserInfoURL:  githubUserInfoURL,
		EmailsURL:    githubEmailsURL,
		Scopes:       []string{"read:user", "user:email"},
	})
}

// publicURL is this instance's externally-reachable base URL for building
// an OAuth redirect_uri — must match what's registered with the provider.
// Falls back to http://<listen addr>, which only works for local/loopback
// testing (GitHub itself accepts http://localhost redirect URIs, so this is
// enough to exercise the flow against a real registered app without TLS).
func publicURL(cfg config.Config) string {
	if cfg.PublicURL != "" {
		return cfg.PublicURL
	}
	return "http://" + cfg.Addr
}
