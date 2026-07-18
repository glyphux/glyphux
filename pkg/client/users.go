package client

import (
	"context"
	"fmt"
)

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

// UpdateRole changes id's role to one of v1's fixed roles.
func (s *UsersService) UpdateRole(ctx context.Context, id int64, role string) (*User, error) {
	var u User
	if err := s.c.doJSON(ctx, "PATCH", fmt.Sprintf("/api/v0/users/%d/role", id), map[string]string{"role": role}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Deactivate blocks id from authenticating and revokes every session it
// currently holds.
func (s *UsersService) Deactivate(ctx context.Context, id int64) error {
	return s.c.doJSON(ctx, "POST", fmt.Sprintf("/api/v0/users/%d/deactivate", id), nil, nil)
}

// Reactivate re-enables a previously deactivated account.
func (s *UsersService) Reactivate(ctx context.Context, id int64) error {
	return s.c.doJSON(ctx, "POST", fmt.Sprintf("/api/v0/users/%d/reactivate", id), nil, nil)
}
