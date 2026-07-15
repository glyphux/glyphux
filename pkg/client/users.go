package client

import "context"

// UsersService wraps /api/v0/users (admin-only; requires users:manage).
type UsersService struct{ c *Client }

// Create provisions a new account with the given role.
func (s *UsersService) Create(ctx context.Context, email, password, role string) (*User, error) {
	var u User
	if err := s.c.doJSON(ctx, "POST", "/api/v0/users", map[string]string{
		"email": email, "password": password, "role": role,
	}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// List returns every account.
func (s *UsersService) List(ctx context.Context) ([]User, error) {
	var resp struct {
		Users []User `json:"users"`
	}
	if err := s.c.doJSON(ctx, "GET", "/api/v0/users", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Users, nil
}
