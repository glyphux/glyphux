package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/permission"
)

// sessionCookieName is the HttpOnly cookie carrying the session token for
// browser clients. API clients may instead send `Authorization: Bearer <token>`.
const sessionCookieName = "glyphux_session"

type ctxKey int

const userKey ctxKey = iota

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	key := remoteKey(r)
	if !s.loginLimiter.allow(key) {
		s.writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	user, err := s.identities.Authenticate(r.Context(), body.Email, body.Password)
	if err != nil {
		// Unknown user and wrong password are indistinguishable.
		s.loginLimiter.recordFailure(key)
		s.writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	sess, err := s.sessions.Create(r.Context(), user.ID)
	if err != nil {
		s.log.Error("create session", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, s.newSessionCookie(sess.Token, r))
	// Browser clients authenticate via the cookie just set; programmatic
	// clients (SDKs, scripts) have no cookie jar, so the same token is also
	// returned in the body for Authorization: Bearer use.
	s.writeJSON(w, http.StatusOK, loginResponse{User: user, Token: sess.Token})
}

type loginResponse struct {
	*identity.User
	Token string `json:"token"`
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token, ok := bearerToken(r); ok {
		_ = s.sessions.Revoke(r.Context(), token)
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.sessions.Revoke(r.Context(), c.Value)
	}
	// Clear the cookie regardless.
	clear := s.newSessionCookie("", r)
	clear.MaxAge = -1
	http.SetCookie(w, clear)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	s.writeJSON(w, http.StatusOK, user)
}

// currentUser resolves the authenticated principal from a bearer token or the
// session cookie, or reports false if the request is unauthenticated.
func (s *Server) currentUser(r *http.Request) (*identity.User, bool) {
	if token, ok := bearerToken(r); ok {
		if u, err := s.sessions.Lookup(r.Context(), token); err == nil {
			return u, true
		}
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if u, err := s.sessions.Lookup(r.Context(), c.Value); err == nil {
			return u, true
		}
	}
	return nil, false
}

// requireUser wraps a handler so it runs only for authenticated requests,
// stashing the principal in the request context. A 401 is returned otherwise.
func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.currentUser(r)
		if !ok {
			s.writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
}

// requireCapability wraps a handler so it runs only for authenticated
// requests whose role holds capability. Unauthenticated requests get 401;
// authenticated but under-privileged requests get 403.
func (s *Server) requireCapability(capability permission.Capability, next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFrom(r.Context())
		if !permission.Allows(user.Role, capability) {
			s.writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next(w, r)
	})
}

// userFrom returns the principal stashed by requireUser, if any.
func userFrom(ctx context.Context) (*identity.User, bool) {
	u, ok := ctx.Value(userKey).(*identity.User)
	return u, ok
}

func (s *Server) newSessionCookie(token string, r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	}
}

// isHTTPS mirrors setup.Wizard's identical trust decision: an unvouched
// X-Forwarded-Proto header is only honored when trustProxyHeaders is set.
func (s *Server) isHTTPS(r *http.Request) bool {
	return r.TLS != nil || (s.trustProxyHeaders && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):]), true
	}
	return "", false
}
