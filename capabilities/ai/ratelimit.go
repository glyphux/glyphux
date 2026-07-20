package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// RateLimit is a fixed-window call budget: at most MaxCalls calls per
// Window. MaxCalls <= 0 means unlimited (rate limiting disabled for that
// operation) — the zero value of RateLimit is therefore "no limit," so a
// Limits value left unconfigured never surprises a caller with a limit it
// never asked for.
type RateLimit struct {
	MaxCalls int
	Window   time.Duration
}

// Limits are the per-operation rate limits a Service enforces at its own
// domain-API boundary (Generate/Embed/Classify), per this slice's spec:
// "Rate-limiting/scoping is this capability's job, not each adapter's" —
// exactly the boundary capabilities/commerce and capabilities/membership
// scope their own operations at, not duplicated per-adapter. Enforcement is
// PER CALLING PLUGIN: each is tracked in the calling plugin's own
// host.Store() (namespaced by that plugin's manifest name — see
// pkg/sdk.HostAPI.Store), so one plugin's heavy AI usage cannot exhaust
// another plugin's budget.
type Limits struct {
	Generate RateLimit
	Embed    RateLimit
	Classify RateLimit
}

// DefaultLimits returns the rate limits a Service uses unless overridden:
// 60 calls/minute for Generate (the most expensive/slowest operation), 120
// calls/minute for Embed and Classify (typically cheaper, smaller
// requests). These are deliberately conservative starting defaults, not a
// PRD-specified number — a real deployment is expected to tune Limits to
// its own provider's rate limits and cost tolerance; see this slice's
// tracking doc.
func DefaultLimits() Limits {
	return Limits{
		Generate: RateLimit{MaxCalls: 60, Window: time.Minute},
		Embed:    RateLimit{MaxCalls: 120, Window: time.Minute},
		Classify: RateLimit{MaxCalls: 120, Window: time.Minute},
	}
}

// ErrRateLimited is returned when a calling plugin has exceeded its budget
// for the operation it attempted.
var ErrRateLimited = errors.New("ai: rate limit exceeded")

// rateLimitState is what checkRateLimit persists in the calling plugin's
// ScopedKV — a fixed-window counter, not a token bucket or sliding window
// (a documented simplification: precise smoothing is not this slice's
// concern, only proving a real per-caller budget is enforced at the
// domain-API boundary).
type rateLimitState struct {
	WindowStart time.Time `json:"window_start"`
	Count       int       `json:"count"`
}

// rateLimitKey namespaces the ScopedKV key per operation, so Generate/Embed/
// Classify each track an independent budget for the same calling plugin.
func rateLimitKey(operation string) string {
	return "ratelimit:" + operation
}

// checkRateLimit enforces limit for operation against the calling plugin's
// own host.Store() (already namespaced per plugin by HostAPI itself — see
// pkg/sdk.hostAPI.Store's doc comment), incrementing the window's counter on
// success. A limit with MaxCalls <= 0 is a no-op (unlimited).
func checkRateLimit(ctx context.Context, host sdk.HostAPI, operation string, limit RateLimit) error {
	if limit.MaxCalls <= 0 {
		return nil
	}
	key := rateLimitKey(operation)
	store := host.Store()
	now := timeNow().UTC()

	raw, ok, err := store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("ai: read rate-limit state: %w", err)
	}
	var state rateLimitState
	if ok {
		if err := json.Unmarshal(raw, &state); err != nil {
			// A corrupted record is treated as "no prior window" rather than
			// a hard failure — the caller should not be permanently locked
			// out by a bad stored value.
			state = rateLimitState{}
			ok = false
		}
	}
	if !ok || now.Sub(state.WindowStart) >= limit.Window {
		state = rateLimitState{WindowStart: now, Count: 0}
	}
	if state.Count >= limit.MaxCalls {
		return fmt.Errorf("%w: %s: %d calls per %s", ErrRateLimited, operation, limit.MaxCalls, limit.Window)
	}
	state.Count++
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("ai: encode rate-limit state: %w", err)
	}
	return store.Set(ctx, key, data)
}

// timeNow is time.Now, indirected so tests in this package can exercise
// fixed-window rollover deterministically without a real sleep.
var timeNow = time.Now
