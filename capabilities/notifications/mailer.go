package notifications

import (
	"context"
	"sync"
)

// MailerAdapter is the provider-agnostic outbound-message boundary this
// capability dispatches through (PRD §14 slice 3.1: "over events + a mailer
// adapter"). A real deployment would implement this against a region-
// relevant provider (e.g. Resend/Vonage/FCM per the PRD's own examples);
// this slice ships only MemoryMailerAdapter (below) since no live provider
// credentials exist in this environment — see this slice's tracking doc for
// the judgment call that the adapter BOUNDARY, not a live integration, is
// what this ticket proves.
type MailerAdapter interface {
	Send(ctx context.Context, to, subject, body string) error
}

// SentMessage is one message MemoryMailerAdapter recorded.
type SentMessage struct {
	To      string
	Subject string
	Body    string
}

// MemoryMailerAdapter is a MailerAdapter that records every message it was
// asked to send instead of contacting any real provider — the local/stub
// adapter this slice's spec calls for, usable both in tests and as a
// placeholder wiring for a running glyphuxd until a real provider adapter
// is written.
type MemoryMailerAdapter struct {
	mu   sync.Mutex
	sent []SentMessage
}

// NewMemoryMailerAdapter returns an adapter with no recorded messages.
func NewMemoryMailerAdapter() *MemoryMailerAdapter {
	return &MemoryMailerAdapter{}
}

// Send records the message and always succeeds.
func (m *MemoryMailerAdapter) Send(ctx context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, SentMessage{To: to, Subject: subject, Body: body})
	return nil
}

// Sent returns every message recorded so far, in send order. Returns a copy
// so callers can't mutate this adapter's internal state.
func (m *MemoryMailerAdapter) Sent() []SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SentMessage(nil), m.sent...)
}
