package api

// TOTP MFA endpoints (slice 0009). Enroll/confirm/disable are self-service
// (requireUser already put the caller's own identity.User in context — a
// caller can never enroll/confirm/disable MFA for anyone but themselves);
// verify is the login flow's second step and is necessarily public, since
// the caller isn't holding a session yet.

import (
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/identity"
)

func (s *Server) handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	secret, otpauthURI, err := s.identities.BeginMFAEnrollment(r.Context(), user.ID)
	if err != nil {
		s.log.Error("begin MFA enrollment", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauthUrl": otpauthURI})
}

func (s *Server) handleMFAConfirm(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	codes, err := s.identities.ConfirmMFAEnrollment(r.Context(), user.ID, body.Code)
	if err != nil {
		s.writeMFAError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (s *Server) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	if err := s.identities.DisableMFA(r.Context(), user.ID); err != nil {
		s.log.Error("disable MFA", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMFAVerify is the login flow's step 2: the client submits the
// mfaToken handleLogin returned plus a TOTP or recovery code, and gets back
// exactly what a normal login would have — a session cookie plus the bearer
// token in the body.
func (s *Server) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	key := remoteKey(r)
	if !s.loginLimiter.allow(key) {
		s.writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}
	var body struct {
		MFAToken string `json:"mfaToken"`
		Code     string `json:"code"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	user, err := s.identities.ResolveMFAChallenge(r.Context(), body.MFAToken, body.Code)
	if err != nil {
		s.loginLimiter.recordFailure(key)
		s.writeMFAError(w, err)
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
	s.writeJSON(w, http.StatusOK, loginResponse{User: user, Token: sess.Token})
}

func (s *Server) writeMFAError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidMFACode):
		s.writeError(w, http.StatusUnauthorized, "invalid or missing MFA code")
	case errors.Is(err, identity.ErrInvalidMFAChallenge):
		s.writeError(w, http.StatusUnauthorized, "invalid or expired MFA challenge")
	case errors.Is(err, identity.ErrMFANotEnabled):
		s.writeError(w, http.StatusUnprocessableEntity, "MFA is not enabled for this account")
	default:
		s.log.Error("MFA request", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
	}
}
