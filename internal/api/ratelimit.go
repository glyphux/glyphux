package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// loginRateLimit bounds failed-login attempts per remote address — a
// brute-force defense (slice 1.9). Fixed window: at most loginMaxAttempts
// failures within loginWindow before further attempts are rejected.
const (
	loginMaxAttempts = 10
	loginWindow      = time.Minute
)

// loginLimiter tracks failed-login timestamps per remote address.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	now      func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string][]time.Time), now: time.Now}
}

// allow reports whether a login attempt from key may proceed, given past
// failures recorded via recordFailure.
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recentLocked(key)) < loginMaxAttempts
}

// recordFailure records a failed attempt from key.
func (l *loginLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.recentLocked(key), l.now())
}

// recentLocked prunes and returns attempts for key within the window. Caller
// must hold l.mu.
func (l *loginLimiter) recentLocked(key string) []time.Time {
	cutoff := l.now().Add(-loginWindow)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.attempts[key] = kept
	return kept
}

// remoteKey extracts the client address for rate-limiting purposes, stripping
// the port so a NAT'd client's varying source ports share one bucket.
func remoteKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
