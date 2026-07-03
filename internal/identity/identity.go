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
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/glyphux/glyphux/internal/db"
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
				created_at    TEXT NOT NULL DEFAULT (datetime('now'))
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
	db *db.DB
}

// NewService wires identity to the database abstraction.
func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

// ErrInvalidCredentials is returned for unknown users or wrong passwords —
// deliberately indistinguishable.
var ErrInvalidCredentials = errors.New("invalid credentials")

// CreateAdmin creates the initial admin account. Called once by the wizard.
func (s *Service) CreateAdmin(ctx context.Context, email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("invalid email address")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyBytes)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO users (email, password_hash, password_salt, role) VALUES (?, ?, ?, 'admin')`,
		email, hex.EncodeToString(hash), hex.EncodeToString(salt))
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	return nil
}

// Verify checks credentials, returning ErrInvalidCredentials on any mismatch.
func (s *Service) Verify(ctx context.Context, email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	var hashHex, saltHex string
	err := s.db.QueryRow(ctx,
		`SELECT password_hash, password_salt FROM users WHERE email = ?`, email).
		Scan(&hashHex, &saltHex)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("verify credentials: %w", err)
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}
	want, err := hex.DecodeString(hashHex)
	if err != nil {
		return fmt.Errorf("decode hash: %w", err)
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyBytes)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// UserCount reports how many accounts exist (0 means setup has not run).
func (s *Service) UserCount(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}
