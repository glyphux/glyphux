// Package setup is the first-run web wizard (§6). It is served by the daemon
// itself — the same wizard for local, server, and (later) managed scenarios —
// and it is just another client of the composition contract (Principle 2):
// its whole job is to write the initial composition and the admin account,
// then lock itself permanently (§6.4).
//
// Phase-0 scope per §6.6: create admin + choose database + set site name.
package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/pkg/contract"
)

// Input is the validated form data a submission collects: everything a
// Committer needs to decide where and how to write the initial state.
type Input struct {
	SiteName      string
	AdminEmail    string
	AdminPassword string
	Driver        string // "sqlite" or "postgres"
	DSN           string // set only when Driver == "postgres"
}

// Committer persists a validated submission as the wizard's initial state.
// The default (no Committer set) writes directly to the wizard's own
// database, atomically. A caller that needs the database choice to actually
// take effect — e.g. opening a fresh Postgres connection when the operator
// selects it in the form — supplies its own Committer via SetCommitter.
type Committer interface {
	Commit(ctx context.Context, in Input) error
}

// Wizard serves the first-run flow and locks it after completion.
type Wizard struct {
	compositions      *composition.Store
	identities        *identity.Service
	database          *db.DB
	log               *slog.Logger
	trustProxyHeaders bool // only trust X-Forwarded-Proto when explicitly configured

	mu        sync.Mutex
	complete  bool
	token     string // required for non-localhost requests (§6.4)
	committer Committer
}

// New builds the wizard, deciding up front whether setup is already complete.
// If setup is pending, a one-time setup token is generated and logged so a
// cloud operator can claim the unconfigured instance before an attacker does.
//
// trustProxyHeaders controls whether X-Forwarded-Proto is honored when
// deciding if a remote request arrived over HTTPS (§6.4). Leave it false
// unless a trusted reverse proxy in front of glyphuxd is known to set that
// header — otherwise a client can simply claim to be HTTPS.
func New(ctx context.Context, comps *composition.Store, ids *identity.Service, database *db.DB, log *slog.Logger, trustProxyHeaders bool) (*Wizard, error) {
	w := &Wizard{compositions: comps, identities: ids, database: database, log: log, trustProxyHeaders: trustProxyHeaders}
	exists, err := comps.Exists(ctx)
	if err != nil {
		return nil, err
	}
	w.complete = exists
	if !exists {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("generate setup token: %w", err)
		}
		w.token = hex.EncodeToString(raw)
		log.Info("first-run setup pending",
			"url", "/setup",
			"setup_token", w.token,
			"note", "token required when accessing the wizard from a non-localhost address")
	}
	return w, nil
}

// Complete reports whether first-run setup has finished.
func (w *Wizard) Complete() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.complete
}

// SetCommitter overrides how a validated submission is persisted. See
// Committer. Not safe to call concurrently with a submission in flight —
// callers set it once, before serving any requests.
func (w *Wizard) SetCommitter(c Committer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.committer = c
}

// Routes registers the wizard endpoints on mux.
func (w *Wizard) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /setup", w.handleForm)
	mux.HandleFunc("POST /setup", w.handleSubmit)
}

func (w *Wizard) handleForm(rw http.ResponseWriter, r *http.Request) {
	if w.Complete() {
		http.Error(rw, "setup already completed; this route is permanently disabled", http.StatusGone)
		return
	}
	if !isLocalhost(r) && !w.isSecure(r) {
		http.Error(rw, "HTTPS is required to access first-run setup remotely (§6.4)", http.StatusForbidden)
		return
	}
	w.render(rw, formData{NeedToken: !isLocalhost(r)})
}

func (w *Wizard) handleSubmit(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.complete {
		http.Error(rw, "setup already completed; this route is permanently disabled", http.StatusGone)
		return
	}
	if !isLocalhost(r) && !w.isSecure(r) {
		http.Error(rw, "HTTPS is required to submit first-run setup remotely (§6.4)", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(rw, "malformed form", http.StatusBadRequest)
		return
	}

	needToken := !isLocalhost(r)
	if needToken {
		given := r.PostFormValue("setup_token")
		if subtle.ConstantTimeCompare([]byte(given), []byte(w.token)) != 1 {
			w.log.Warn("setup attempt with invalid token", "remote", r.RemoteAddr)
			w.renderError(rw, needToken, "Invalid setup token. The token was printed to the server log at boot.")
			return
		}
	}

	siteName := strings.TrimSpace(r.PostFormValue("site_name"))
	email := r.PostFormValue("admin_email")
	password := r.PostFormValue("admin_password")
	driver := r.PostFormValue("database")
	dsn := strings.TrimSpace(r.PostFormValue("database_dsn"))

	if siteName == "" {
		w.renderError(rw, needToken, "Site name is required.")
		return
	}
	if driver != "" && driver != "sqlite" && driver != "postgres" {
		w.renderError(rw, needToken, fmt.Sprintf("Unknown database %q (want sqlite or postgres).", driver))
		return
	}
	if driver == "postgres" && dsn == "" {
		w.renderError(rw, needToken, "A Postgres connection string is required when Postgres is selected.")
		return
	}
	in := Input{SiteName: siteName, AdminEmail: email, AdminPassword: password, Driver: driver, DSN: dsn}

	ctx := r.Context()
	// Writing the initial composition is the act that completes setup, so it
	// and the admin account are created atomically: either both exist or
	// neither does, and a failed attempt is always safe to retry (§17).
	// commit persists via Store.SaveWith (not Save), which does not require a
	// principal — the real security boundary for this pre-auth bootstrap
	// write is the wizard's own token/localhost gate above, not a capability
	// check (Store.Save's capability check exists for post-setup callers).
	if err := w.commit(ctx, in); err != nil {
		w.renderError(rw, needToken, err.Error())
		return
	}

	w.complete = true
	w.token = ""
	w.log.Info("first-run setup completed", "site", siteName)

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(rw, doneHTML, template.HTMLEscapeString(siteName))
}

// commit persists in as the wizard's initial state: the injected Committer
// if one is set, otherwise the default — create the admin account and save
// the composition atomically against the wizard's own database.
func (w *Wizard) commit(ctx context.Context, in Input) error {
	if w.committer != nil {
		return w.committer.Commit(ctx, in)
	}
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: in.SiteName},
	}
	return w.database.WithTx(ctx, func(q db.Queryer) error {
		if err := w.identities.CreateAdminWith(ctx, q, in.AdminEmail, in.AdminPassword); err != nil {
			return fmt.Errorf("admin account: %w", err)
		}
		if err := w.compositions.SaveWith(ctx, q, comp); err != nil {
			return fmt.Errorf("composition: %w", err)
		}
		return nil
	})
}

type formData struct {
	NeedToken bool
	Error     string
}

func (w *Wizard) render(rw http.ResponseWriter, data formData) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := formTemplate.Execute(rw, data); err != nil {
		w.log.Error("render setup form", "error", err)
	}
}

func (w *Wizard) renderError(rw http.ResponseWriter, needToken bool, msg string) {
	rw.WriteHeader(http.StatusUnprocessableEntity)
	w.render(rw, formData{NeedToken: needToken, Error: msg})
}

// isSecure reports whether r arrived over TLS. A reverse-proxy-terminated
// TLS connection is only recognized via X-Forwarded-Proto when
// trustProxyHeaders is set — an unvouched header is just a client claim.
func (w *Wizard) isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return w.trustProxyHeaders && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var formTemplate = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Set up Glyphux</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: system-ui, sans-serif; max-width: 26rem; margin: 4rem auto; padding: 0 1rem; }
  h1 { font-size: 1.4rem; }
  label { display: block; margin-top: 1rem; font-weight: 600; }
  input, select { width: 100%; padding: .5rem; margin-top: .25rem; box-sizing: border-box; }
  button { margin-top: 1.5rem; padding: .6rem 1.2rem; font-weight: 600; cursor: pointer; }
  .error { color: #b00020; margin-top: 1rem; }
  .hint { font-size: .85rem; opacity: .7; }
</style>
</head>
<body>
<h1>Welcome to Glyphux</h1>
<p>Complete first-run setup. This writes your site&rsquo;s initial composition and creates the admin account.</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="/setup">
  <label>Site name
    <input name="site_name" required autofocus>
  </label>
  <label>Admin email
    <input name="admin_email" type="email" required>
  </label>
  <label>Admin password
    <input name="admin_password" type="password" minlength="8" required>
  </label>
  <label>Database
    <select name="database">
      <option value="sqlite" selected>SQLite (embedded, recommended)</option>
      <option value="postgres">Postgres</option>
    </select>
    <span class="hint">SQLite needs nothing else. Choosing Postgres takes effect immediately — no restart.</span>
  </label>
  <label>Postgres connection string
    <input name="database_dsn" placeholder="postgres://user:pass@host:5432/dbname">
    <span class="hint">Required only if you selected Postgres above. Never written to disk.</span>
  </label>
  {{if .NeedToken}}
  <label>Setup token
    <input name="setup_token" required>
    <span class="hint">Printed to the server log at boot.</span>
  </label>
  {{end}}
  <button type="submit">Complete setup</button>
</form>
</body>
</html>
`))

const doneHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Glyphux is ready</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 26rem; margin: 4rem auto;">
<h1>%s is ready</h1>
<p>Setup is complete and this wizard is now permanently disabled.</p>
<p>Try the API: <code>GET /api/v0/content/ping</code></p>
</body></html>`
