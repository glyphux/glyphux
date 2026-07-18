package identity

// TOTP MFA (RFC 6238, see totp.go for the algorithm itself). Opt-in per
// account: a user enrolls (BeginMFAEnrollment/ConfirmMFAEnrollment), and once
// enabled, a password login no longer issues a session directly — the
// caller must also resolve an MFA challenge (BeginMFAChallenge/
// ResolveMFAChallenge) with a TOTP code or one of the recovery codes issued
// at enrollment.
//
// Enrollment/confirmation/challenge-resolution are inherently self-service:
// the only "capability" that matters is being the authenticated account in
// question, which the transport layer already establishes by resolving the
// caller's own session before calling these methods (it never lets a caller
// name an arbitrary target user id here). That is why, unlike UpdateRole and
// Deactivate below, these methods take no *permission.Principal — there is
// no admin-over-another-account capability check to make.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

func init() { Migrations = append(Migrations, mfaMigrations...) }

var mfaMigrations = []db.Migration{
	{
		Version: 8,
		Name:    "mfa columns",
		SQL: `
			ALTER TABLE users ADD COLUMN mfa_enabled INTEGER NOT NULL DEFAULT 0;
			ALTER TABLE users ADD COLUMN mfa_secret TEXT NOT NULL DEFAULT '';
			ALTER TABLE users ADD COLUMN mfa_recovery_codes TEXT NOT NULL DEFAULT '';
		`,
	},
	{
		Version: 9,
		Name:    "mfa challenges",
		SQL: `
			CREATE TABLE mfa_challenges (
				token_hash TEXT NOT NULL PRIMARY KEY,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				expires_at TEXT NOT NULL
			);
		`,
	},
}

const (
	// mfaIssuer names the account in the otpauth:// URI / authenticator app.
	mfaIssuer = "glyphux"
	// recoveryCodeCount is how many one-time recovery codes are minted at
	// enrollment, covering a lost-device support path without meaningfully
	// bloating this slice's scope.
	recoveryCodeCount = 8
	recoveryCodeBytes = 5
	mfaChallengeTTL   = 5 * time.Minute
	mfaChallengeBytes = 32
)

// ErrInvalidMFACode is returned when a TOTP code and every recovery code
// both fail to verify.
var ErrInvalidMFACode = errors.New("invalid or missing MFA code")

// ErrInvalidMFAChallenge is returned for an unknown, expired, or already
// resolved MFA challenge token.
var ErrInvalidMFAChallenge = errors.New("invalid or expired MFA challenge")

// ErrMFANotEnabled is returned when a caller tries to verify or challenge
// MFA for an account that never completed enrollment.
var ErrMFANotEnabled = errors.New("MFA is not enabled for this account")

// now is the wall clock identity uses for MFA challenge expiry — indirected
// through a var (rather than an instance field, since totp code paths this
// simple don't need one) purely so tests can advance/inspect it via svc.now().
func (s *Service) now() time.Time { return time.Now().UTC() }

// BeginMFAEnrollment starts TOTP enrollment for userID: generates a fresh
// secret and stores it unconfirmed (mfa_enabled stays false until
// ConfirmMFAEnrollment verifies the user actually captured it in an
// authenticator app). Returns the secret and its otpauth:// URI (for a QR
// code) so the caller can display both.
func (s *Service) BeginMFAEnrollment(ctx context.Context, userID int64) (secret, otpauthURI string, err error) {
	email, err := s.emailByID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	secret, err = generateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET mfa_secret = ? WHERE id = ?`, secret, userID); err != nil {
		return "", "", fmt.Errorf("store pending MFA secret: %w", err)
	}
	return secret, otpauthURL(mfaIssuer, email, secret), nil
}

// ConfirmMFAEnrollment verifies code against the secret BeginMFAEnrollment
// stored, and if it matches, enables MFA and mints recovery codes — the raw
// codes are returned exactly once; only their hashes are persisted.
func (s *Service) ConfirmMFAEnrollment(ctx context.Context, userID int64, code string) (recoveryCodes []string, err error) {
	secret, err := s.pendingSecret(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !validateTOTP(secret, code, s.now()) {
		return nil, ErrInvalidMFACode
	}
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx,
		`UPDATE users SET mfa_enabled = 1, mfa_recovery_codes = ? WHERE id = ?`,
		strings.Join(hashes, ","), userID); err != nil {
		return nil, fmt.Errorf("enable MFA: %w", err)
	}
	return codes, nil
}

// DisableMFA turns MFA off for userID and forgets its secret and any unused
// recovery codes.
func (s *Service) DisableMFA(ctx context.Context, userID int64) error {
	if _, err := s.db.Exec(ctx,
		`UPDATE users SET mfa_enabled = 0, mfa_secret = '', mfa_recovery_codes = '' WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("disable MFA: %w", err)
	}
	return nil
}

// VerifyMFA checks code (a TOTP code or an unused recovery code) for an
// already-MFA-enabled userID. A matching recovery code is consumed (removed
// from the stored set) so it cannot be reused.
func (s *Service) VerifyMFA(ctx context.Context, userID int64, code string) error {
	if code == "" {
		return ErrInvalidMFACode
	}
	enabled, secret, hashes, err := s.mfaState(ctx, userID)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrMFANotEnabled
	}
	if validateTOTP(secret, code, s.now()) {
		return nil
	}
	codeHash := hashRecoveryCode(code)
	for i, h := range hashes {
		if constantTimeEq(h, codeHash) {
			remaining := append(hashes[:i:i], hashes[i+1:]...)
			if _, err := s.db.Exec(ctx,
				`UPDATE users SET mfa_recovery_codes = ? WHERE id = ?`, strings.Join(remaining, ","), userID); err != nil {
				return fmt.Errorf("consume recovery code: %w", err)
			}
			return nil
		}
	}
	return ErrInvalidMFACode
}

// BeginMFAChallenge issues a short-lived challenge token for userID, once
// its password has already verified — the login flow's second step. The
// raw token is the one returned to the client; only its hash is stored.
func (s *Service) BeginMFAChallenge(ctx context.Context, userID int64) (string, error) {
	raw := make([]byte, mfaChallengeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate MFA challenge: %w", err)
	}
	token := hex.EncodeToString(raw)
	expires := s.now().Add(mfaChallengeTTL)
	if _, err := s.db.Exec(ctx,
		`INSERT INTO mfa_challenges (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, expires.Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("create MFA challenge: %w", err)
	}
	return token, nil
}

// ResolveMFAChallenge verifies code against the challenge token's user and,
// only on success, consumes the challenge (deletes it) and returns the user
// — ready for the caller to open a session. An invalid code leaves the
// challenge intact so the client can retry within its TTL.
func (s *Service) ResolveMFAChallenge(ctx context.Context, token, code string) (*User, error) {
	var (
		userID     int64
		expiresStr string
	)
	err := s.db.QueryRow(ctx, `SELECT user_id, expires_at FROM mfa_challenges WHERE token_hash = ?`, hashToken(token)).
		Scan(&userID, &expiresStr)
	if errors.Is(err, db.ErrNoRows) {
		return nil, ErrInvalidMFAChallenge
	}
	if err != nil {
		return nil, fmt.Errorf("lookup MFA challenge: %w", err)
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresStr)
	if err != nil {
		return nil, fmt.Errorf("parse MFA challenge expiry: %w", err)
	}
	if !s.now().Before(expires) {
		return nil, ErrInvalidMFAChallenge
	}
	if err := s.VerifyMFA(ctx, userID, code); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM mfa_challenges WHERE token_hash = ?`, hashToken(token)); err != nil {
		return nil, fmt.Errorf("consume MFA challenge: %w", err)
	}
	return s.userByID(ctx, userID)
}

// emailByID and userByID both need the same "does this id exist" lookup that
// ListUsers/Authenticate already do inline; kept as small unexported helpers
// here rather than growing Service's public surface.
func (s *Service) emailByID(ctx context.Context, userID int64) (string, error) {
	var email string
	err := s.db.QueryRow(ctx, `SELECT email FROM users WHERE id = ?`, userID).Scan(&email)
	if errors.Is(err, db.ErrNoRows) {
		return "", fmt.Errorf("unknown user %d", userID)
	}
	if err != nil {
		return "", fmt.Errorf("lookup user: %w", err)
	}
	return email, nil
}

func (s *Service) userByID(ctx context.Context, userID int64) (*User, error) {
	var u User
	var mfaEnabled int
	err := s.db.QueryRow(ctx, `SELECT id, email, role, mfa_enabled FROM users WHERE id = ?`, userID).
		Scan(&u.ID, &u.Email, &u.Role, &mfaEnabled)
	if errors.Is(err, db.ErrNoRows) {
		return nil, fmt.Errorf("unknown user %d", userID)
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	u.MFAEnabled = mfaEnabled != 0
	return &u, nil
}

func (s *Service) pendingSecret(ctx context.Context, userID int64) (string, error) {
	var secret string
	err := s.db.QueryRow(ctx, `SELECT mfa_secret FROM users WHERE id = ?`, userID).Scan(&secret)
	if errors.Is(err, db.ErrNoRows) {
		return "", fmt.Errorf("unknown user %d", userID)
	}
	if err != nil {
		return "", fmt.Errorf("lookup pending MFA secret: %w", err)
	}
	if secret == "" {
		return "", fmt.Errorf("no MFA enrollment in progress for user %d", userID)
	}
	return secret, nil
}

func (s *Service) mfaState(ctx context.Context, userID int64) (enabled bool, secret string, recoveryHashes []string, err error) {
	var enabledInt int
	var hashesCSV string
	dbErr := s.db.QueryRow(ctx, `SELECT mfa_enabled, mfa_secret, mfa_recovery_codes FROM users WHERE id = ?`, userID).
		Scan(&enabledInt, &secret, &hashesCSV)
	if errors.Is(dbErr, db.ErrNoRows) {
		return false, "", nil, fmt.Errorf("unknown user %d", userID)
	}
	if dbErr != nil {
		return false, "", nil, fmt.Errorf("lookup MFA state: %w", dbErr)
	}
	if hashesCSV == "" {
		return enabledInt != 0, secret, nil, nil
	}
	return enabledInt != 0, secret, strings.Split(hashesCSV, ","), nil
}

// generateRecoveryCodes mints recoveryCodeCount fresh one-time codes,
// returning both the raw codes (shown to the user exactly once) and their
// SHA-256 hashes (what gets persisted).
func generateRecoveryCodes() (codes, hashes []string, err error) {
	codes = make([]string, recoveryCodeCount)
	hashes = make([]string, recoveryCodeCount)
	for i := range codes {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		codes[i] = strings.ToUpper(hex.EncodeToString(raw))
		hashes[i] = hashRecoveryCode(codes[i])
	}
	return codes, hashes, nil
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}
