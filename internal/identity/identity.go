// Package identity is the kernel identity engine. Phase 0 scope: create the
// admin account during first-run setup and verify credentials. Sessions,
// OAuth, and MFA arrive in slice 1.7.
package identity

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
)

// Migrations is the Phase-0 schema baseline for identity.
var Migrations = []db.Migration{
	{
		Version: 2,
		Name:    "identity baseline",
		SQL: `
			CREATE TABLE users (
				id            INTEGER PRIMARY KEY AUTOINCREMENT,
				email         TEXT NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				password_salt TEXT NOT NULL,
				role          TEXT NOT NULL DEFAULT 'admin',
				created_at    TEXT NOT NULL
			);
		`,
		PostgresSQL: `
			CREATE TABLE users (
				id            INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
				email         TEXT NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				password_salt TEXT NOT NULL,
				role          TEXT NOT NULL DEFAULT 'admin',
				created_at    TEXT NOT NULL
			);
		`,
	},
}

const (
	pbkdf2Iterations = 600_000 // OWASP 2024 guidance for PBKDF2-SHA256
	saltBytes        = 16
	keyBytes         = 32
)

// Service is the kernel identity engine.
type Service struct {
	db    *db.DB
	audit *audit.Logger // nil unless WithAudit wired (Ticket T7)
}

// Option configures optional Service behavior beyond the required database.
type Option func(*Service)

// WithAudit wires an audit logger so every identity write (create user /
// role change / deactivate / reactivate) records one row via the user
// recorder (Ticket T7 / gap 4). Nil — the zero value — is a byte-identical
// no-op: no rows, no behavior change, no panic. Reads are deliberately
// un-audited.
func WithAudit(logger *audit.Logger) Option {
	return func(s *Service) { s.audit = logger }
}

// NewService wires identity to the database abstraction.
func NewService(database *db.DB, opts ...Option) *Service {
	s := &Service{db: database}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// auditUser best-effort logs one identity write. A nil logger is a no-op; a
// logging failure never masks the write itself. CreateUser has no principal
// (setup/self-service path), so its actor is fully empty — Actor.ID is
// always "" at this layer (owner-confirmed: HTTP-layer actor enrichment is
// out of scope this round).
func (s *Service) auditUser(ctx context.Context, action string, principal *permission.Principal, userID int64) {
	if s.audit == nil {
		return
	}
	_ = s.audit.RecordUser(ctx, action, audit.Actor{Role: permission.RoleOf(principal)}, userID)
}

// ErrInvalidCredentials is returned for unknown users or wrong passwords —
// deliberately indistinguishable.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrAccountDeactivated is returned by Authenticate for a deactivated
// account with otherwise-correct credentials. Unlike ErrInvalidCredentials
// this is deliberately distinguishable: the caller legitimately owns these
// credentials, so telling them the account was deactivated (rather than
// "wrong password") is not a credential-enumeration risk.
var ErrAccountDeactivated = errors.New("account deactivated")

// CreateAdmin creates the initial admin account. Called once by the wizard.
func (s *Service) CreateAdmin(ctx context.Context, email, password string) error {
	_, err := s.createAccountWith(ctx, s.db, email, password, permission.RoleAdmin)
	return err
}

// CreateAdminWith creates the initial admin account using q instead of the
// service's own database handle — q is typically a transaction from
// db.WithTx, so bootstrap can create the admin and save the initial
// composition atomically: both commit together, or neither does.
func (s *Service) CreateAdminWith(ctx context.Context, q db.Queryer, email, password string) error {
	_, err := s.createAccountWith(ctx, q, email, password, permission.RoleAdmin)
	return err
}

// CreateUser creates an account with the given role, one of v1's fixed
// roles (admin/editor/viewer per the internal/permission matrix). Intended
// for an admin to provision editor/viewer accounts.
func (s *Service) CreateUser(ctx context.Context, email, password, role string) (*User, error) {
	if !permission.ValidRole(role) {
		return nil, fmt.Errorf("unknown role %q", role)
	}
	u, err := s.createAccountWith(ctx, s.db, email, password, role)
	if err != nil {
		return nil, err
	}
	s.auditUser(ctx, audit.ActionUserCreated, nil, u.ID)
	return u, nil
}

func (s *Service) createAccountWith(ctx context.Context, q db.Queryer, email, password, role string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("invalid email address")
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	// Query the id back explicitly rather than via Result.LastInsertId, which
	// Postgres's driver does not implement (§11.6: the db abstraction must
	// work identically on both engines).
	_, err = q.Exec(ctx,
		`INSERT INTO users (email, password_hash, password_salt, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		email, hex.EncodeToString(hash), hex.EncodeToString(salt), role, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	var id int64
	if err := q.QueryRow(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		return nil, fmt.Errorf("account id: %w", err)
	}
	return &User{ID: id, Email: email, Role: role, Active: true}, nil
}

// ListUsers returns every account, oldest first.
func (s *Service) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.Query(ctx, `SELECT id, email, role, mfa_enabled, active FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		var u User
		var mfaEnabled, active int
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &mfaEnabled, &active); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.MFAEnabled = mfaEnabled != 0
		u.Active = active != 0
		out = append(out, &u)
	}
	return out, rows.Err()
}

// Verify checks credentials, returning ErrInvalidCredentials on any mismatch.
// It is a thin wrapper over Authenticate for callers that need only a yes/no.
func (s *Service) Verify(ctx context.Context, email, password string) error {
	_, err := s.Authenticate(ctx, email, password)
	return err
}

// User is an authenticated principal — the identity the permission engine and
// domain APIs reason about. It never carries credentials.
type User struct {
	ID         int64  `json:"id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	MFAEnabled bool   `json:"mfaEnabled"`
	Active     bool   `json:"active"`
}

// Authenticate verifies credentials and returns the matching user, or
// ErrInvalidCredentials. Unlike Verify it yields the principal, so a caller can
// open a session.
func (s *Service) Authenticate(ctx context.Context, email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var (
		u                  User
		hashHex, saltHex   string
		mfaEnabled, active int
	)
	err := s.db.QueryRow(ctx,
		`SELECT id, email, role, password_hash, password_salt, mfa_enabled, active FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.Role, &hashHex, &saltHex, &mfaEnabled, &active)
	if errors.Is(err, db.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}
	want, err := hex.DecodeString(hashHex)
	if err != nil {
		return nil, fmt.Errorf("decode hash: %w", err)
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return nil, ErrInvalidCredentials
	}
	u.MFAEnabled = mfaEnabled != 0
	u.Active = active != 0
	if !u.Active {
		return nil, ErrAccountDeactivated
	}
	return &u, nil
}

// UserCount reports how many accounts exist (0 means setup has not run).
func (s *Service) UserCount(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}
