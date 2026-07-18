package client

import "context"

// User is the authenticated principal, mirroring internal/identity.User's
// wire shape.
type User struct {
	ID         int64  `json:"id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	MFAEnabled bool   `json:"mfaEnabled"`
	Active     bool   `json:"active"`
}

// AuthService wraps /api/v0/auth.
type AuthService struct{ c *Client }

// MFARequiredError is returned by Login instead of a *User when the
// account has TOTP MFA enabled: the password verified, but a session was
// deliberately withheld pending a second factor. Call VerifyMFA with Token
// and a TOTP (or recovery) code to complete the login.
type MFARequiredError struct{ Token string }

func (e *MFARequiredError) Error() string { return "glyphux: MFA verification required" }

// Login authenticates and stores the returned bearer token on the Client for
// subsequent requests. If the account has MFA enabled, no session is issued
// yet — Login returns a *MFARequiredError instead (check with errors.As);
// complete the login with VerifyMFA.
func (s *AuthService) Login(ctx context.Context, email, password string) (*User, error) {
	var resp struct {
		User
		Token       string `json:"token"`
		MFARequired bool   `json:"mfaRequired"`
		MFAToken    string `json:"mfaToken"`
	}
	if err := s.c.doJSON(ctx, "POST", "/api/v0/auth/login", map[string]string{
		"email": email, "password": password,
	}, &resp); err != nil {
		return nil, err
	}
	if resp.MFARequired {
		return nil, &MFARequiredError{Token: resp.MFAToken}
	}
	s.c.SetToken(resp.Token)
	return &resp.User, nil
}

// VerifyMFA completes a login that Login reported as MFA-required: token is
// the MFARequiredError's Token, code is a TOTP code from the enrolled
// authenticator app (or an unused recovery code). On success the Client's
// bearer token is set, same as a plain Login.
func (s *AuthService) VerifyMFA(ctx context.Context, token, code string) (*User, error) {
	var resp struct {
		User
		Token string `json:"token"`
	}
	if err := s.c.doJSON(ctx, "POST", "/api/v0/auth/mfa/verify", map[string]string{
		"mfaToken": token, "code": code,
	}, &resp); err != nil {
		return nil, err
	}
	s.c.SetToken(resp.Token)
	return &resp.User, nil
}

// BeginMFAEnrollment starts TOTP enrollment for the Client's currently
// authenticated account: returns the raw secret and its otpauth:// URI (for
// a QR code). Enrollment isn't active yet — call ConfirmMFAEnrollment with
// a code generated from the secret to turn MFA on.
func (s *AuthService) BeginMFAEnrollment(ctx context.Context) (secret, otpauthURL string, err error) {
	var resp struct {
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauthUrl"`
	}
	if err := s.c.doJSON(ctx, "POST", "/api/v0/auth/mfa/enroll", nil, &resp); err != nil {
		return "", "", err
	}
	return resp.Secret, resp.OTPAuthURL, nil
}

// ConfirmMFAEnrollment verifies code against the secret BeginMFAEnrollment
// returned; on success MFA is enabled and the returned recovery codes are
// shown exactly once — store them somewhere safe.
func (s *AuthService) ConfirmMFAEnrollment(ctx context.Context, code string) ([]string, error) {
	var resp struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	if err := s.c.doJSON(ctx, "POST", "/api/v0/auth/mfa/confirm", map[string]string{"code": code}, &resp); err != nil {
		return nil, err
	}
	return resp.RecoveryCodes, nil
}

// DisableMFA turns MFA off for the Client's currently authenticated account.
func (s *AuthService) DisableMFA(ctx context.Context) error {
	return s.c.doJSON(ctx, "POST", "/api/v0/auth/mfa/disable", nil, nil)
}

// Logout revokes the current session and clears the stored token.
func (s *AuthService) Logout(ctx context.Context) error {
	if err := s.c.doJSON(ctx, "POST", "/api/v0/auth/logout", nil, nil); err != nil {
		return err
	}
	s.c.SetToken("")
	return nil
}

// Me returns the authenticated principal for the Client's current token.
func (s *AuthService) Me(ctx context.Context) (*User, error) {
	var u User
	if err := s.c.doJSON(ctx, "GET", "/api/v0/auth/me", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}
