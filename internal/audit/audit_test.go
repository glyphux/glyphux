package audit_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/db"
)

func newTestLogger(t *testing.T) *audit.Logger {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), audit.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return audit.NewLogger(d)
}

func TestLog_PersistsRecordRetrievableByPlugin(t *testing.T) {
	ctx := context.Background()
	l := newTestLogger(t)

	if err := l.Log(ctx, audit.Record{
		PluginName: "commerce",
		Action:     "consent.decide",
		Allowed:    true,
		Detail:     "status=approved",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	records, err := l.ListByPlugin(ctx, "commerce")
	if err != nil {
		t.Fatalf("ListByPlugin: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if r.PluginName != "commerce" || r.Action != "consent.decide" || !r.Allowed || r.Detail != "status=approved" {
		t.Fatalf("unexpected record: %+v", r)
	}
	if r.OccurredAt.IsZero() {
		t.Fatalf("expected OccurredAt to be set")
	}
	if r.ID == 0 {
		t.Fatalf("expected a non-zero assigned ID")
	}
}

func TestLog_RecordsBothAllowedAndDeniedEntries(t *testing.T) {
	ctx := context.Background()
	l := newTestLogger(t)

	if err := l.Log(ctx, audit.Record{PluginName: "forms", Action: "hostapi.register_admin_page", Allowed: false, Detail: "admin_ui: sdk: scope not declared in manifest"}); err != nil {
		t.Fatalf("Log denied: %v", err)
	}
	if err := l.Log(ctx, audit.Record{PluginName: "forms", Action: "hostapi.register_admin_page", Allowed: true, Detail: "slug=forms-settings"}); err != nil {
		t.Fatalf("Log allowed: %v", err)
	}

	records, err := l.ListByPlugin(ctx, "forms")
	if err != nil {
		t.Fatalf("ListByPlugin: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Allowed {
		t.Fatalf("expected first record (oldest) to be the denied attempt")
	}
	if !records[1].Allowed {
		t.Fatalf("expected second record to be the allowed attempt")
	}
}

func TestListByPlugin_UnknownPlugin_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	l := newTestLogger(t)

	records, err := l.ListByPlugin(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ListByPlugin: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no records, got %d", len(records))
	}
}

func TestListByPlugin_DoesNotLeakOtherPluginsRecords(t *testing.T) {
	ctx := context.Background()
	l := newTestLogger(t)

	if err := l.Log(ctx, audit.Record{PluginName: "commerce", Action: "consent.decide", Allowed: true}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := l.Log(ctx, audit.Record{PluginName: "forms", Action: "consent.decide", Allowed: true}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	records, err := l.ListByPlugin(ctx, "commerce")
	if err != nil {
		t.Fatalf("ListByPlugin: %v", err)
	}
	if len(records) != 1 || records[0].PluginName != "commerce" {
		t.Fatalf("expected only commerce's own record, got %+v", records)
	}
}

func TestLog_PersistsAcrossLoggerInstances(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	d1, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := d1.Migrate(ctx, audit.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	l1 := audit.NewLogger(d1)
	if err := l1.Log(ctx, audit.Record{PluginName: "commerce", Action: "consent.decide", Allowed: true}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	d1.Close()

	d2, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	t.Cleanup(func() { d2.Close() })
	l2 := audit.NewLogger(d2)

	records, err := l2.ListByPlugin(ctx, "commerce")
	if err != nil {
		t.Fatalf("ListByPlugin: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected persisted record to survive across Logger instances, got %d", len(records))
	}
}
