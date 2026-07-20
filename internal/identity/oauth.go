// OAuth2/social login: the standard authorization-code flow (RFC 6749 §4.1)
// against an extensible set of providers. Provider choice for this slice:
// GitHub — a self-hosted instance can register a GitHub OAuth App with zero
// review/verification friction (no OAuth consent-screen configuration, no
// domain-ownership proof, works immediately with an http://localhost
// redirect URI during development), unlike Google, which gates non-trivial
// scopes behind a consent-screen verification process aimed at production
// public apps. GitHub is the only provider wired into cmd/glyphuxd today,
// but OAuthProvider is a plain config struct (client id/secret,
// authorize/token/userinfo endpoints) precisely so a second provider is a
// registration, not a rewrite.
//
// Account linking rule (the one thing every OAuth integration must decide
// explicitly): match the provider's reported email, case-insensitively,
// against an existing account and link by provider+subject if found;
// otherwise create a new account. The provider must report the email as
// verified — this code refuses to link or create off an unverified email,
// since doing so would let anyone claiming an arbitrary unverified address
// take over (or shadow-create) an account for it. A newly created
// OAuth-only account gets the viewer role (least privilege); an admin
// promotes it via UpdateRole same as any other account.
package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
)

// OAuthProvider is one provider's configuration: endpoints plus this
// instance's registered app credentials. EmailsURL is optional — set it for
// a GitHub-shaped provider whose userinfo endpoint doesn't itself carry a
// reliable verified email (GitHub's /user.email is null unless the user
// made it public; the verified, possibly-private primary email lives at
// /user/emails instead). Leave it empty for an OIDC-userinfo-shaped
// provider that returns email/email_verified directly from UserInfoURL.
type OAuthProvider struct {
	Name         string
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	EmailsURL    string
	Scopes       []string
}

// OAuthIdentity is what a completed authorization-code exchange yields:
// enough to run the account-linking rule (FindOrCreateOAuthUser).
type OAuthIdentity struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
}

// ErrInvalidOAuthState is returned for a callback whose state parameter is
// unknown, expired, or already consumed — the CSRF check the authorization
// code flow relies on (RFC 6749 §10.12).
var ErrInvalidOAuthState = errors.New("invalid or expired OAuth state")

// ErrUnknownOAuthProvider is returned for a provider name OAuthManager was
// not configured with.
var ErrUnknownOAuthProvider = errors.New("unknown OAuth provider")

// ErrOAuthEmailUnverified is returned when the provider does not report the
// account's email as verified — see the account-linking rule documented at
// the top of this file for why that is a hard refusal, not a soft warning.
var ErrOAuthEmailUnverified = errors.New("OAuth provider did not report a verified email")

const oauthStateTTL = 10 * time.Minute

// OAuthManager drives the authorization-code flow against a fixed set of
// configured providers. It holds in-memory CSRF state only (no database
// dependency) — unlike Sessions/mfa_challenges, a lost state on daemon
// restart just means an in-flight login has to restart too, which is an
// acceptable trade for not needing a migration for what is a
// seconds-to-minutes-lived value.
type OAuthManager struct {
	providers  map[string]OAuthProvider
	httpClient *http.Client

	mu     sync.Mutex
	states map[string]oauthStateEntry
}

type oauthStateEntry struct {
	provider string
	expires  time.Time
}

// NewOAuthManager wires an OAuthManager over the given providers. client may
// be nil, in which case http.DefaultClient is used; tests pass a fake
// provider's own httptest.Server client so no real network egress occurs.
func NewOAuthManager(client *http.Client, providers ...OAuthProvider) *OAuthManager {
	if client == nil {
		client = http.DefaultClient
	}
	m := &OAuthManager{
		providers:  make(map[string]OAuthProvider, len(providers)),
		httpClient: client,
		states:     make(map[string]oauthStateEntry),
	}
	for _, p := range providers {
		m.providers[p.Name] = p
	}
	return m
}

// Provider returns the named provider's config, or false if unconfigured.
func (m *OAuthManager) Provider(name string) (OAuthProvider, bool) {
	p, ok := m.providers[name]
	return p, ok
}

// AuthorizeURL builds the URL to redirect the browser to for provider,
// along with a fresh CSRF state token the caller must round-trip back
// (typically via a short-lived cookie) and pass to Exchange.
func (m *OAuthManager) AuthorizeURL(provider, redirectURI string) (authURL, state string, err error) {
	p, ok := m.providers[provider]
	if !ok {
		return "", "", ErrUnknownOAuthProvider
	}
	state, err = randomState()
	if err != nil {
		return "", "", err
	}
	m.mu.Lock()
	m.states[state] = oauthStateEntry{provider: provider, expires: time.Now().Add(oauthStateTTL)}
	m.mu.Unlock()

	v := url.Values{}
	v.Set("client_id", p.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("state", state)
	v.Set("response_type", "code")
	if len(p.Scopes) > 0 {
		v.Set("scope", strings.Join(p.Scopes, " "))
	}
	return p.AuthURL + "?" + v.Encode(), state, nil
}

// Exchange completes the authorization-code flow: validates and consumes
// state, trades code for an access token, and fetches the provider's
// userinfo (and, for a GitHub-shaped provider, its emails endpoint) to
// produce an OAuthIdentity.
func (m *OAuthManager) Exchange(ctx context.Context, provider, code, redirectURI, state string) (*OAuthIdentity, error) {
	p, ok := m.providers[provider]
	if !ok {
		return nil, ErrUnknownOAuthProvider
	}
	if !m.consumeState(provider, state) {
		return nil, ErrInvalidOAuthState
	}

	token, err := m.exchangeCodeForToken(ctx, p, code, redirectURI)
	if err != nil {
		return nil, err
	}
	subject, email, verified, err := m.fetchIdentity(ctx, p, token)
	if err != nil {
		return nil, err
	}
	return &OAuthIdentity{Provider: provider, Subject: subject, Email: email, EmailVerified: verified}, nil
}

func (m *OAuthManager) consumeState(provider, state string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.states[state]
	if !ok {
		return false
	}
	delete(m.states, state) // single-use regardless of outcome below
	return entry.provider == provider && time.Now().Before(entry.expires)
}

func (m *OAuthManager) exchangeCodeForToken(ctx context.Context, p OAuthProvider, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// GitHub's token endpoint returns form-encoded unless asked for JSON;
	// requesting JSON explicitly keeps this provider-agnostic (an
	// OIDC-shaped provider already returns JSON regardless).
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if body.Error != "" {
		return "", fmt.Errorf("token exchange rejected: %s", body.Error)
	}
	if body.AccessToken == "" {
		return "", errors.New("token response carried no access_token")
	}
	return body.AccessToken, nil
}

func (m *OAuthManager) fetchIdentity(ctx context.Context, p OAuthProvider, token string) (subject, email string, verified bool, err error) {
	var userinfo struct {
		ID            any    `json:"id"`
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := m.getJSON(ctx, p.UserInfoURL, token, &userinfo); err != nil {
		return "", "", false, fmt.Errorf("fetch userinfo: %w", err)
	}
	subject = userinfo.Sub
	if subject == "" {
		subject = fmt.Sprint(userinfo.ID)
	}
	email, verified = userinfo.Email, userinfo.EmailVerified

	if p.EmailsURL != "" {
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := m.getJSON(ctx, p.EmailsURL, token, &emails); err != nil {
			return "", "", false, fmt.Errorf("fetch emails: %w", err)
		}
		for _, e := range emails {
			if e.Primary {
				email, verified = e.Email, e.Verified
				break
			}
		}
	}
	return subject, email, verified, nil
}

func (m *OAuthManager) getJSON(ctx context.Context, endpoint, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func randomState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// --- Account linking (identity.Service side) ---

func init() { Migrations = append(Migrations, oauthMigrations...) }

var oauthMigrations = []db.Migration{
	{
		Version: 11,
		Name:    "oauth linking",
		SQL: `
			ALTER TABLE users ADD COLUMN oauth_provider TEXT NOT NULL DEFAULT '';
			ALTER TABLE users ADD COLUMN oauth_subject TEXT NOT NULL DEFAULT '';
			CREATE UNIQUE INDEX idx_users_oauth ON users (oauth_provider, oauth_subject) WHERE oauth_provider != '';
		`,
	},
}

// FindOrCreateOAuthUser applies this slice's account-linking rule: an
// already-linked provider+subject resolves directly; otherwise a verified
// email matches an existing account (linking it going forward); otherwise a
// new viewer-role account is created. emailVerified must be true — see the
// package doc comment for why an unverified email is refused outright
// rather than trusted.
func (s *Service) FindOrCreateOAuthUser(ctx context.Context, provider, subject, email string, emailVerified bool) (*User, error) {
	if !emailVerified {
		return nil, ErrOAuthEmailUnverified
	}
	email = strings.ToLower(strings.TrimSpace(email))

	if id, ok, err := s.userIDByOAuth(ctx, provider, subject); err != nil {
		return nil, err
	} else if ok {
		return s.userByID(ctx, id)
	}

	if id, ok, err := s.userIDByEmail(ctx, email); err != nil {
		return nil, err
	} else if ok {
		if _, err := s.db.Exec(ctx, `UPDATE users SET oauth_provider = ?, oauth_subject = ? WHERE id = ?`,
			provider, subject, id); err != nil {
			return nil, fmt.Errorf("link OAuth identity: %w", err)
		}
		return s.userByID(ctx, id)
	}

	return s.createOAuthAccount(ctx, provider, subject, email)
}

func (s *Service) userIDByOAuth(ctx context.Context, provider, subject string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(ctx, `SELECT id FROM users WHERE oauth_provider = ? AND oauth_subject = ?`, provider, subject).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("lookup OAuth identity: %w", err)
	}
	return id, true, nil
}

func (s *Service) userIDByEmail(ctx context.Context, email string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	if errors.Is(err, db.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("lookup account by email: %w", err)
	}
	return id, true, nil
}

// createOAuthAccount provisions a brand-new account for a first-time OAuth
// login. It has no usable password: a random, never-surfaced value fills
// the NOT NULL password_hash/password_salt columns, so password login
// simply never matches (the account is OAuth-only until/unless an admin —
// or a future self-service "set a password" flow — gives it one).
func (s *Service) createOAuthAccount(ctx context.Context, provider, subject, email string) (*User, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("generate placeholder password: %w", err)
	}
	u, err := s.createAccountWith(ctx, s.db, email, hex.EncodeToString(random), permission.RoleViewer)
	if err != nil {
		return nil, fmt.Errorf("create OAuth account: %w", err)
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET oauth_provider = ?, oauth_subject = ? WHERE id = ?`,
		provider, subject, u.ID); err != nil {
		return nil, fmt.Errorf("link new OAuth account: %w", err)
	}
	return u, nil
}
