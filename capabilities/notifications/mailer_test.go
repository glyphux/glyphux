package notifications_test

import (
	"context"
	"testing"

	"github.com/glyphux/glyphux/capabilities/notifications"
)

func TestMemoryMailerAdapterRecordsSentMessages(t *testing.T) {
	adapter := notifications.NewMemoryMailerAdapter()

	if err := adapter.Send(context.Background(), "a@example.com", "hi", "body"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent := adapter.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 recorded message, got %d", len(sent))
	}
	got := sent[0]
	if got.To != "a@example.com" || got.Subject != "hi" || got.Body != "body" {
		t.Fatalf("unexpected recorded message: %+v", got)
	}
}
