package identity

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glyphux/glyphux/internal/permission"
)

// fakeGitHub is a protocol-accurate test double for GitHub's OAuth2
// authorization-code flow: /login/oauth/authorize (not actually hit by the
// server side — the browser would go there), POST /login/oauth/access_token
// (Accept: application/json => JSON, mirroring GitHub's real quirk of
// defaulting to form-encoded otherwise), GET /user, and GET /user/emails.
// Real end-to-end verification against github.com itself is not practical
// in this sandbox (no live client credentials, no network egress); this
// double exercises the exact same request/response shapes the real
// endpoints use, which is what oauth.go's Exchange code path talks to.
type fakeGitHub struct {
	srv          *httptest.Server
	code         string
	accessToken  string
	subject      string
	primaryEmail string
	verified     bool
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{
		code:         "test-auth-code",
		accessToken:  "test-access-token",
		subject:      "12345",
		primaryEmail: "octocat@example.com",
		verified:     true,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("code") != f.code {
			http.Error(w, "bad code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": f.accessToken,
			"token_type":   "bearer",
			"scope":        "user:email",
		})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.accessToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "login": "octocat"})
	})
	mux.HandleFunc("GET /user/emails", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.accessToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "secondary@example.com", "primary": false, "verified": true},
			{"email": f.primaryEmail, "primary": true, "verified": f.verified},
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) provider() OAuthProvider {
	return OAuthProvider{
		Name:         "github",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		AuthURL:      f.srv.URL + "/login/oauth/authorize",
		TokenURL:     f.srv.URL + "/login/oauth/access_token",
		UserInfoURL:  f.srv.URL + "/user",
		EmailsURL:    f.srv.URL + "/user/emails",
		Scopes:       []string{"user:email"},
	}
}

func TestAuthorizeURLIncludesClientIDStateAndRedirect(t *testing.T) {
	gh := newFakeGitHub(t)
	m := NewOAuthManager(nil, gh.provider())
	url, state, err := m.AuthorizeURL("github", "https://glyphux.example/callback")
	if err != nil {
		t.Fatal(err)
	}
	if state == "" {
		t.Fatal("empty state")
	}
	if !contains(url, "client_id=test-client-id") || !contains(url, "state="+state) ||
		!contains(url, "redirect_uri=https%3A%2F%2Fglyphux.example%2Fcallback") {
		t.Errorf("authorize URL missing expected params: %s", url)
	}
}

func TestAuthorizeURLUnknownProvider(t *testing.T) {
	m := NewOAuthManager(nil)
	if _, _, err := m.AuthorizeURL("nope", "https://x/callback"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestExchangeCompletesFullFlowAgainstFakeProvider(t *testing.T) {
	gh := newFakeGitHub(t)
	m := NewOAuthManager(gh.srv.Client(), gh.provider())
	_, state, err := m.AuthorizeURL("github", "https://glyphux.example/callback")
	if err != nil {
		t.Fatal(err)
	}

	ident, err := m.Exchange(t.Context(), "github", gh.code, "https://glyphux.example/callback", state)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if ident.Provider != "github" || ident.Subject != "12345" {
		t.Errorf("unexpected identity %+v", ident)
	}
	if ident.Email != "octocat@example.com" || !ident.EmailVerified {
		t.Errorf("expected verified primary email, got %+v", ident)
	}
}

func TestExchangeRejectsUnknownOrReusedState(t *testing.T) {
	gh := newFakeGitHub(t)
	m := NewOAuthManager(gh.srv.Client(), gh.provider())
	_, state, _ := m.AuthorizeURL("github", "https://glyphux.example/callback")

	if _, err := m.Exchange(t.Context(), "github", gh.code, "https://glyphux.example/callback", "bogus-state"); !errors.Is(err, ErrInvalidOAuthState) {
		t.Errorf("bogus state: got %v, want ErrInvalidOAuthState", err)
	}

	if _, err := m.Exchange(t.Context(), "github", gh.code, "https://glyphux.example/callback", state); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	// State is single-use.
	if _, err := m.Exchange(t.Context(), "github", gh.code, "https://glyphux.example/callback", state); !errors.Is(err, ErrInvalidOAuthState) {
		t.Errorf("reused state: got %v, want ErrInvalidOAuthState", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// --- Account linking rule tests (identity.Service side) ---

func TestFindOrCreateOAuthUserCreatesNewAccountOnFirstLogin(t *testing.T) {
	svc := testService(t)
	ctx := t.Context()
	u, err := svc.FindOrCreateOAuthUser(ctx, "github", "999", "new@example.com", true)
	if err != nil {
		t.Fatalf("FindOrCreateOAuthUser: %v", err)
	}
	if u.Email != "new@example.com" {
		t.Errorf("email = %q, want new@example.com", u.Email)
	}
	if u.Role != permission.RoleViewer {
		t.Errorf("new OAuth account role = %q, want viewer (least privilege)", u.Role)
	}
}

func TestFindOrCreateOAuthUserLinksExistingAccountByVerifiedEmail(t *testing.T) {
	svc := testService(t)
	ctx := t.Context()
	existing, err := svc.CreateUser(ctx, "existing@example.com", "correct horse battery", "editor")
	if err != nil {
		t.Fatal(err)
	}

	linked, err := svc.FindOrCreateOAuthUser(ctx, "github", "555", "existing@example.com", true)
	if err != nil {
		t.Fatalf("FindOrCreateOAuthUser: %v", err)
	}
	if linked.ID != existing.ID {
		t.Errorf("expected linking to existing account %d, got new account %d", existing.ID, linked.ID)
	}
	if linked.Role != "editor" {
		t.Errorf("linking should not change the existing account's role; got %q", linked.Role)
	}

	// A second login with the same provider+subject resolves to the same
	// linked account without re-matching by email.
	again, err := svc.FindOrCreateOAuthUser(ctx, "github", "555", "existing@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != existing.ID {
		t.Errorf("subsequent login should resolve to the same linked account")
	}
}

func TestFindOrCreateOAuthUserRejectsUnverifiedEmail(t *testing.T) {
	svc := testService(t)
	if _, err := svc.FindOrCreateOAuthUser(t.Context(), "github", "1", "unverified@example.com", false); !errors.Is(err, ErrOAuthEmailUnverified) {
		t.Errorf("got %v, want ErrOAuthEmailUnverified", err)
	}
}
