// Package audit is the kernel's audit subsystem (PRD §10.5: "every
// sensitive grant and every cross-boundary call is recorded by the audit
// subsystem"). It is an append-only, real-DB-backed log — kernel-owned,
// never a plugin-facing surface.
//
// This slice wires two sources into it: the install-time consent engine
// (internal/consent — every Decide call, whatever its outcome) and
// pkg/sdk's HostAPI boundary gates (the registration/event methods that
// already have a clean allow/deny signal: RegisterContentType,
// RegisterBlock, RegisterAdminPage, RegisterJob, On, Emit). Item-level
// Content/Users/Media CRUD auditing is deferred — see this slice's tracking
// doc for why the coarser boundary was chosen first.
package audit

import (
	"context"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

// Record is one persisted audit entry: an attempted action, whether it was
// allowed, and free-form detail describing what happened. Denied attempts
// are recorded exactly like allowed ones — a denial is itself a security-
// relevant event (PRD §10.1's adversarial-by-default model assumes plugins
// will probe boundaries they aren't granted).
type Record struct {
	ID         int64
	PluginName string
	Action     string
	Allowed    bool
	Detail     string
	OccurredAt time.Time
}

// Logger persists audit records against the database abstraction — never a
// raw table a caller queries directly.
type Logger struct {
	db *db.DB
}

// NewLogger wires the audit logger to the database abstraction. Callers
// must have already run Migrations (via db.Migrate) against database.
func NewLogger(database *db.DB) *Logger {
	return &Logger{db: database}
}

// Log persists r. If r.OccurredAt is zero, it is set to time.Now().UTC()
// before persisting — callers normally leave it unset and let Log stamp it.
func (l *Logger) Log(ctx context.Context, r Record) error {
	if r.OccurredAt.IsZero() {
		r.OccurredAt = time.Now().UTC()
	}
	_, err := insert(ctx, l.db, r)
	return err
}

// ListByPlugin returns every record logged for pluginName, oldest first. An
// unknown plugin name returns an empty slice, not an error.
func (l *Logger) ListByPlugin(ctx context.Context, pluginName string) ([]Record, error) {
	return listByPlugin(ctx, l.db, pluginName)
}
