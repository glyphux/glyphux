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
	"time"

	"github.com/glyphux/glyphux/blocks/firstparty"
	"github.com/glyphux/glyphux/capabilities/ai"
	"github.com/glyphux/glyphux/capabilities/commerce"
	"github.com/glyphux/glyphux/capabilities/forms"
	"github.com/glyphux/glyphux/capabilities/membership"
	"github.com/glyphux/glyphux/capabilities/notifications"
	"github.com/glyphux/glyphux/capabilities/seo"
	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/bootstrap"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/graphql"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/plugin"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/internal/server"
	"github.com/glyphux/glyphux/internal/setup"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// version is overridden at release-build time via
// -ldflags "-X main.version=$VERSION" (see scripts/release/build.sh); a
// source checkout / `go build` with no ldflags reports "dev", distinguishing
// a packaged release binary from an ad hoc developer build.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "glyphuxd:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to JSON config file (optional)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

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
	migrations = append(migrations, preset.Migrations...)
	migrations = append(migrations, bundle.Migrations...)
	// Plugin KV persistence (gap 6 / Ticket T1): the durable backend every
	// loaded plugin's Store() writes to — a fresh boot creates plugin_kv.
	migrations = append(migrations, pluginstore.Migrations...)
	// Install-time consent (gap 2 / Ticket T4): consent_decisions (14) and
	// audit_records (15). Migration-list ownership for these two versions
	// belongs to T4 — no other ticket adds a migration here (single-owner
	// rule).
	migrations = append(migrations, audit.Migrations...)
	migrations = append(migrations, consent.Migrations...)

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
		// Audit trail (gap 4 / Ticket T7): one logger for the whole daemon —
		// it backs the consent engine's decision records (T4), the item-level
		// content/media recorders below, and KernelDeps.Audit so every loaded
		// plugin's boundary gates log. Migration 15 (audit_records) is in the
		// list appended at main.go's migration block.
		auditLogger := audit.NewLogger(database)
		contentAPI := content.NewAPI(compositions, content.NewStore(database), content.WithAudit(auditLogger))
		mediaAPI := media.NewAPI(media.NewStore(database), filepath.Join(cfg.DataDir, "media"), media.WithAudit(auditLogger))

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
		presetStore := preset.NewStore(database)
		bundleStore := bundle.NewStore(database)

		// Install-time consent (gap 2 / Ticket T4): the consent engine over
		// the real database, with the shared audit logger wired in so every
		// decision is recorded (audit is strictly additive — an engine built
		// without WithAudit would still persist decisions). The T6 loader
		// builds the wasm/rpc consent adapter from this same engine; the
		// adapter ships in internal/plugin with its own suite (no
		// AlwaysConsent anywhere in the daemon path).
		consentEngine := consent.NewEngine(database, consent.WithAudit(auditLogger))

		// Plugin loading (Ticket T6 / gap 1): the 3-tier loader generalizes
		// the T5 registrar. Tier A first-party capabilities register through
		// the loader's RegisterPlugin (unchanged); tier B wasm and tier C
		// rpc plugins come from config (plugins[]{name,tier,source} +
		// GLYPHUX_PLUGINS_DIR), each consent-gated at load: an unconsented
		// plugin is refused before any host is built, logged, and the daemon
		// continues; invariant violations (duplicate names, tier
		// misconfiguration) fail the boot fast. Every loaded plugin's
		// Store() is backed by the durable pluginstore KV (T1) — the shared
		// KernelDeps also carries the daemon's domain stores and block
		// registry, so all three tiers see the same world.
		capLoader := plugin.NewLoader(sdk.KernelDeps{
			Compositions: compositions,
			Content:      contentAPI,
			Media:        mediaAPI,
			Identities:   identities,
			KV:           pluginstore.NewStore(database),
			Blocks:       blockRegistry,
			Audit:        auditLogger,
		}, consentEngine)
		for _, p := range firstPartyPlugins() {
			if err := capLoader.RegisterPlugin(p); err != nil {
				return nil, err
			}
		}
		if err := capLoader.Load(context.Background(), plugin.LoadConfig{
			Dir:     cfg.Plugins.Dir,
			Plugins: configuredPlugins(cfg.Plugins.Plugins),
		}); err != nil {
			return nil, err
		}

		// Activation is deferred on a virgin install: the wizard writes the
		// initial composition on first-run submission, and the first-party
		// plugins' Register calls (forms/commerce/membership define content
		// types) genuinely require it — RegisterContentType ->
		// DefineContentType fails with composition.ErrNotFound otherwise.
		// The wizard committer rebuilds this handler immediately after
		// committing, so plugins come up with the app on setup completion
		// (and an activation failure then fails the submission, never a
		// half-booted app); on every provisioned boot the composition
		// already exists and activation happens here, fail-fast.
		compsExist, err := compositions.Exists(context.Background())
		if err != nil {
			return nil, fmt.Errorf("check composition: %w", err)
		}
		if compsExist {
			if err := capLoader.Activate(context.Background()); err != nil {
				return nil, err
			}
		} else {
			log.Info("first-party capability plugins deferred until setup completes")
		}

		apiOpts := []api.Option{
			api.TrustProxyHeaders(cfg.TrustProxyHeaders),
			api.WithLayouts(layoutStore, blockRegistry),
			api.WithPresets(presetStore, bundleStore),
			api.WithConsent(consentEngine, capLoader.Registered()),
			api.WithAuditLogger(auditLogger),
		}
		// AI authoring (Ticket T5 / gap 3): opt-in via ai.provider. Unknown
		// providers fail fast here, at boot, naming the valid adapter set;
		// an unset provider leaves POST /api/v0/ai/compose 404ing (the
		// endpoint's default when WithAI is absent) and requires no key.
		if cfg.AI.Provider != "" {
			adapter, err := buildAIAdapter(cfg.AI)
			if err != nil {
				return nil, err
			}
			svc := ai.NewService(adapter)
			if cfg.AI.RateLimit > 0 {
				// rate_limit is a single calls/minute knob applied uniformly
				// to every operation the Service rate-limits (spec: "rate_limit
				// maps to Service.Limits").
				lim := ai.RateLimit{MaxCalls: cfg.AI.RateLimit, Window: time.Minute}
				svc.Limits = ai.Limits{Generate: lim, Embed: lim, Classify: lim}
			}
			apiOpts = append(apiOpts, api.WithAI(svc))
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

// firstPartyPlugins returns the five first-party capability plugins the
// daemon registers at boot, in registration order. commerce and membership
// get nil gateways (no payment processor configured — their manifests then
// declare no network permission); notifications gets the documented memory
// mailer placeholder, the daemon's default mailer until a real one is
// configured.
func firstPartyPlugins() []sdk.Plugin {
	return []sdk.Plugin{
		forms.New(),
		seo.New(),
		commerce.New(nil),
		membership.New(nil),
		notifications.New(notifications.NewMemoryMailerAdapter()),
	}
}

// configuredPlugins translates the operator-facing config entries into the
// loader's plugin configs. The manifest is carried through as configured
// (the interim carrier until T8's package containers ship it); a plugin
// without one is refused at load (deny-by-default) and the daemon continues.
func configuredPlugins(cfgPlugins []config.PluginConfig) []plugin.PluginConfig {
	out := make([]plugin.PluginConfig, 0, len(cfgPlugins))
	for _, p := range cfgPlugins {
		out = append(out, plugin.PluginConfig{
			Name:     p.Name,
			Tier:     p.Tier,
			Source:   p.Source,
			Manifest: p.Manifest,
		})
	}
	return out
}

// aiAdapterSet is the real adapter set in capabilities/ai — the constructors
// at capabilities/ai/claude.go:36, openai.go:43, gemini.go:31 and
// openai.go:56. An unknown provider is a startup error naming exactly this
// set (spec's accepted "claude|openai|gemini|openai-compatible").
const aiAdapterSet = "claude|openai|gemini|openai-compatible"

// buildAIAdapter constructs the provider adapter for cfg.AI.Provider. Base
// URL is required by claude/gemini/openai-compatible (their constructors
// reject an empty one — that error surfaces here as a fail-fast boot
// error); openai always talks to https://api.openai.com and ignores it.
func buildAIAdapter(cfg config.AIConfig) (ai.Adapter, error) {
	switch cfg.Provider {
	case "claude":
		return ai.NewClaudeAdapter(cfg.APIKey, cfg.BaseURL)
	case "openai":
		return ai.NewOpenAIAdapter(cfg.APIKey)
	case "gemini":
		return ai.NewGeminiAdapter(cfg.APIKey, cfg.BaseURL)
	case "openai-compatible":
		return ai.NewOpenAICompatibleAdapter(cfg.BaseURL, cfg.APIKey)
	default:
		return nil, fmt.Errorf("unknown ai.provider %q (want %s)", cfg.Provider, aiAdapterSet)
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
