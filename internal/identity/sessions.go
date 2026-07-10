package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

// ErrInvalidSession reports a session token that is unknown, revoked, or
// expired — deliberately indistinguishable so a caller cannot probe validity.
var ErrInvalidSession = errors.New("invalid session")

// DefaultSessionTTL is how long a session stays valid after creation.
const DefaultSessionTTL = 30 * 24 * time.Hour

const sessionTokenBytes = 32

// init folds the session schema into the package Migrations set so every
// caller that runs identity.Migrations gets the sessions table too.
func init() { Migrations = append(Migrations, sessionMigrations...) }

// sessionMigrations extends the identity schema with the sessions table. It is
// appended after the identity baseline (version 2).
var sessionMigrations = []db.Migration{
	{
		Version: 4,
		Name:    "sessions",
		SQL: `
			CREATE TABLE sessions (
				token_hash TEXT NOT NULL PRIMARY KEY,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				created_at TEXT NOT NULL,
				expires_at TEXT NOT NULL
			);
			CREATE INDEX idx_sessions_user ON sessions (user_id);
		`,
	},
}

// Session is an issued session. Token is the raw bearer secret and is returned
// exactly once, at creation; only its hash is ever stored.
type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

// Sessions issues and validates opaque session tokens. Tokens are stored only
// as SHA-256 hashes, so a database leak does not yield usable sessions.
type Sessions struct {
	db  *db.DB
	ttl time.Duration
	now func() time.Time
}

// NewSessions wires the session store with the default TTL and wall clock.
func NewSessions(database *db.DB) *Sessions {
	return &Sessions{db: database, ttl: DefaultSessionTTL, now: time.Now}
}

// Create opens a new session for the user and returns it with its raw token.
func (s *Sessions) Create(ctx context.Context, userID int64) (*Session, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(raw)
	now := s.now().UTC()
	expires := now.Add(s.ttl)
	_, err := s.db.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), userID, now.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Session{Token: token, UserID: userID, ExpiresAt: expires}, nil
}

// Lookup resolves a session token to its user, or ErrInvalidSession if the
// token is unknown, revoked, or expired.
func (s *Sessions) Lookup(ctx context.Context, token string) (*User, error) {
	var (
		u          User
		expiresStr string
	)
	err := s.db.QueryRow(ctx, `
		SELECT u.id, u.email, u.role, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, hashToken(token)).
		Scan(&u.ID, &u.Email, &u.Role, &expiresStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresStr)
	if err != nil {
		return nil, fmt.Errorf("parse session expiry: %w", err)
	}
	if !s.now().Before(expires) {
		return nil, ErrInvalidSession
	}
	return &u, nil
}

// Revoke deletes a session so its token can no longer be used. Revoking an
// unknown token is a no-op (idempotent logout).
func (s *Sessions) Revoke(ctx context.Context, token string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token)); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
