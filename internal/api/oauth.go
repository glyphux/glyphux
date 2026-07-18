package api

// OAuth2/social login endpoints (slice 0009). Both routes 404 unless
// WithOAuth configured a manager — an operator who hasn't registered
// provider credentials simply doesn't get this surface, rather than it
// error out at every request.

import (
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/identity"
)

// oauthStateCookieName carries the CSRF state issued at /start through the
// browser redirect round-trip back to /callback. Short-lived and HttpOnly;
// its value is compared against, not trusted as, the caller's identity.
const oauthStateCookieName = "glyphux_oauth_state"

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		s.writeError(w, http.StatusNotFound, "OAuth is not configured on this instance")
		return
	}
	provider := r.PathValue("provider")
	redirectURI := s.oauthRedirectBase + "/api/v0/auth/oauth/" + provider + "/callback"
	authURL, state, err := s.oauth.AuthorizeURL(provider, redirectURI)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown OAuth provider")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    state,
		Path:     "/api/v0/auth/oauth",
		HttpOnly: true,
		Secure:   s.isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		s.writeError(w, http.StatusNotFound, "OAuth is not configured on this instance")
		return
	}
	provider := r.PathValue("provider")
	code := r.URL.Query().Get("code")
	stateParam := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie(oauthStateCookieName)
	if code == "" || stateParam == "" || err != nil || stateCookie.Value != stateParam {
		s.writeError(w, http.StatusBadRequest, "invalid OAuth callback")
		return
	}

	redirectURI := s.oauthRedirectBase + "/api/v0/auth/oauth/" + provider + "/callback"
	ident, err := s.oauth.Exchange(r.Context(), provider, code, redirectURI, stateParam)
	if err != nil {
		s.log.Error("OAuth exchange", "provider", provider, "error", err)
		s.writeError(w, http.StatusBadRequest, "OAuth login failed")
		return
	}

	user, err := s.identities.FindOrCreateOAuthUser(r.Context(), ident.Provider, ident.Subject, ident.Email, ident.EmailVerified)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, identity.ErrOAuthEmailUnverified) {
			status = http.StatusForbidden
		} else {
			s.log.Error("find or create OAuth user", "error", err)
		}
		s.writeError(w, status, "OAuth login failed")
		return
	}
	if !user.Active {
		s.writeError(w, http.StatusForbidden, "account deactivated")
		return
	}

	sess, err := s.sessions.Create(r.Context(), user.ID)
	if err != nil {
		s.log.Error("create session", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, s.newSessionCookie(sess.Token, r))
	// Mirrors handleLogin: this cookie-authenticated session needs its own
	// CSRF cookie too, or every mutation route's requireCSRF would reject
	// this session with no way to obtain a valid token (slice 1.9).
	csrfToken, err := newCSRFToken()
	if err != nil {
		s.log.Error("generate CSRF token", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, s.newCSRFCookie(csrfToken, r))
	// Clear the now-consumed state cookie.
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookieName, Path: "/api/v0/auth/oauth", MaxAge: -1})
	s.writeJSON(w, http.StatusOK, loginResponse{User: user, Token: sess.Token})
}
