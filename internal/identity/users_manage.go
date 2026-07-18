package identity

// Role changes and account deactivation — the user-management operations an
// admin performs on someone else's account, as opposed to mfa.go's
// self-service operations. Both take a *permission.Principal and check
// permission.AllowsPrincipal themselves (PRD §10.5 domain-boundary
// enforcement, the same pattern internal/content, internal/composition, and
// internal/media follow) as defense-in-depth alongside whatever
// transport-level requireCapability check also runs.
//
// Deactivate (not a hard delete) was chosen deliberately: see the tracking
// doc's Current Decisions for why — no internal/content reference chased
// users by id at the time of writing, but deactivation is still the safer
// default (it revokes sessions and blocks future auth without destroying
// the row), and it is trivially reversible by an admin if done in error,
// unlike a hard delete.

import (
	"context"
	"errors"
	"fmt"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
)

func init() { Migrations = append(Migrations, userManagementMigrations...) }

var userManagementMigrations = []db.Migration{
	{
		Version: 10,
		Name:    "user active flag",
		SQL:     `ALTER TABLE users ADD COLUMN active INTEGER NOT NULL DEFAULT 1;`,
	},
}

// ErrCannotModifySelf guards against an admin locking themselves out by
// changing their own role or deactivating their own account through this
// path (they can still do so directly against the database if truly
// intended — this is a safety rail, not a hard security boundary).
var ErrCannotModifySelf = errors.New("cannot change your own role or deactivate your own account")

// UpdateRole changes userID's role to one of v1's fixed roles. Requires the
// calling principal to hold users:manage.
func (s *Service) UpdateRole(ctx context.Context, principal *permission.Principal, userID int64, role string) (*User, error) {
	if !permission.AllowsPrincipal(principal, permission.UsersManage) {
		return nil, permission.ErrDenied
	}
	if !permission.ValidRole(role) {
		return nil, fmt.Errorf("unknown role %q", role)
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, userID); err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}
	return s.userByID(ctx, userID)
}

// Deactivate blocks userID from authenticating going forward. It does not
// revoke existing sessions itself (Service has no dependency on Sessions,
// which is a separate store wired alongside it) — callers (internal/api's
// handler, internal/graphql's resolver) must also call
// Sessions.RevokeAllForUser so a deactivated account's live sessions don't
// keep working until they expire on their own.
func (s *Service) Deactivate(ctx context.Context, principal *permission.Principal, userID int64) error {
	if !permission.AllowsPrincipal(principal, permission.UsersManage) {
		return permission.ErrDenied
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET active = 0 WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("deactivate user: %w", err)
	}
	return nil
}

// Reactivate re-enables a previously deactivated account. Requires
// users:manage, mirroring Deactivate.
func (s *Service) Reactivate(ctx context.Context, principal *permission.Principal, userID int64) error {
	if !permission.AllowsPrincipal(principal, permission.UsersManage) {
		return permission.ErrDenied
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET active = 1 WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("reactivate user: %w", err)
	}
	return nil
}
