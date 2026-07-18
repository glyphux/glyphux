package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

// csrfCookieName carries a random anti-CSRF token, issued alongside the
// session cookie at login. Unlike the session cookie it is deliberately NOT
// HttpOnly: the admin SPA (and any other cookie-authenticated browser
// client) must be able to read it via JS and mirror it back in the
// csrfHeaderName header on every state-changing request — the classic
// double-submit-cookie pattern (PRD §1.9).
//
// A cross-site attacker can make a victim's browser attach the session
// cookie to a forged request automatically, but same-origin policy stops
// that attacker's page from reading this cookie's value to mirror it in a
// header, so the forged request fails the match even while riding a valid,
// unwitting session.
const csrfCookieName = "glyphux_csrf"

// csrfHeaderName is the header a cookie-authenticated client must set to
// the current csrfCookieName cookie's value on every state-changing
// request.
const csrfHeaderName = "X-CSRF-Token"

const csrfTokenBytes = 32

// newCSRFToken generates a fresh random CSRF token.
func newCSRFToken() (string, error) {
	b := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// newCSRFCookie mirrors newSessionCookie's HTTPS-trust decision but is not
// HttpOnly, since client JS must be able to read it.
func (s *Server) newCSRFCookie(token string, r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   s.isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	}
}

// requireCSRF wraps a state-changing (POST/PUT/PATCH/DELETE) handler with a
// double-submit-cookie CSRF check. It applies only to requests
// authenticated via the session cookie: bearer-token-authenticated requests
// are exempt entirely, since a stolen bearer token already requires the
// attacker to be able to read the response — unlike a cookie, which a
// forged cross-site request carries automatically — so bearer auth isn't
// the CSRF threat model (PRD §1.9 decision). Requests with no session
// cookie at all pass through unchecked too; downstream auth
// (requireUser/requireCapability) is what rejects those as unauthenticated.
func (s *Server) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearerToken(r); ok {
			next(w, r)
			return
		}
		sessCookie, err := r.Cookie(sessionCookieName)
		if err != nil || sessCookie.Value == "" {
			next(w, r)
			return
		}
		csrfCookie, err := r.Cookie(csrfCookieName)
		if err != nil || csrfCookie.Value == "" {
			s.writeError(w, http.StatusForbidden, "missing CSRF token")
			return
		}
		header := r.Header.Get(csrfHeaderName)
		if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(csrfCookie.Value)) != 1 {
			s.writeError(w, http.StatusForbidden, "invalid CSRF token")
			return
		}
		next(w, r)
	}
}
