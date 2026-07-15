package client

import "context"

// User is the authenticated principal, mirroring internal/identity.User's
// wire shape.
type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// AuthService wraps /api/v0/auth.
type AuthService struct{ c *Client }

// Login authenticates and stores the returned bearer token on the Client for
// subsequent requests.
func (s *AuthService) Login(ctx context.Context, email, password string) (*User, error) {
	var resp struct {
		User
		Token string `json:"token"`
	}
	if err := s.c.doJSON(ctx, "POST", "/api/v0/auth/login", map[string]string{
		"email": email, "password": password,
	}, &resp); err != nil {
		return nil, err
	}
	s.c.SetToken(resp.Token)
	return &resp.User, nil
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
