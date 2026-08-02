// RED tests for Ticket T7 (gap 4 — audit activation + item-level CRUD
// auditing). These pin the recorder surface internal/audit/recorders.go
// will implement: the 13 item-level action constants, the Actor type, and
// RecordContent / RecordMedia / RecordUser persisting through Logger.Log
// with a stable JSON Detail payload — the audit contract every domain
// recorder and the endpoint rely on.
//
// Interpretation (documented, awaiting owner confirmation where flagged):
//   - plugin_name is stamped "content" / "media" / "identity" so item-level
//     rows are queryable through the endpoint's only accessor, ListByPlugin.
//   - Detail is one stable shape for all three recorders:
//     {"actor_id","role","item_id","type"}; actor_id is "" when the
//     recording layer can't see the authenticated user (role-only
//     principals at the domain boundary, setup-time writes) — every key is
//     always present so the payload is trivially parseable.
package audit

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/db"
)

func testAuditLogger(t *testing.T) *Logger {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), Migrations); err != nil {
		t.Fatal(err)
	}
	return NewLogger(d)
}

// detail is the parsed JSON Detail payload — the stable audit contract.
type detail struct {
	ActorID string `json:"actor_id"`
	Role    string `json:"role"`
	ItemID  string `json:"item_id"`
	Type    string `json:"type"`
}

func parseDetail(t *testing.T, r Record) detail {
	t.Helper()
	var out detail
	if err := json.Unmarshal([]byte(r.Detail), &out); err != nil {
		t.Fatalf("parse detail %q: %v", r.Detail, err)
	}
	return out
}

// TestItemActionConstantsArePinned pins the 13 action constants defined once
// in recorders.go — one per item-level write site — so every caller shares
// one action vocabulary (the spec: "define 13 action constants once").
func TestItemActionConstantsArePinned(t *testing.T) {
	pinned := map[string]string{
		ActionContentCreated:     "content.created",
		ActionContentUpdated:     "content.updated",
		ActionContentDeleted:     "content.deleted",
		ActionContentPublished:   "content.published",
		ActionContentUnpublished: "content.unpublished",
		ActionContentRolledBack:  "content.rolled_back",
		ActionMediaUploaded:      "media.uploaded",
		ActionMediaUpdated:       "media.updated",
		ActionMediaDeleted:       "media.deleted",
		ActionUserCreated:        "user.created",
		ActionUserRoleChanged:    "user.role_changed",
		ActionUserDeactivated:    "user.deactivated",
		ActionUserReactivated:    "user.reactivated",
	}
	if len(pinned) != 13 {
		t.Fatalf("pinned set = %d constants, want 13", len(pinned))
	}
	for constant, want := range pinned {
		if constant != want {
			t.Errorf("constant = %q, want %q", constant, want)
		}
	}
}

// TestRecordContentPersistsStableDetail: GIVEN a real-SQLite audit logger,
// WHEN RecordContent logs a content.create for an admin actor, THEN the row
// is queryable by plugin "content" with the pinned action, allowed=true,
// and a parseable Detail carrying actor_id, role, item_id and type.
func TestRecordContentPersistsStableDetail(t *testing.T) {
	l := testAuditLogger(t)
	ctx := context.Background()

	if err := l.RecordContent(ctx, ActionContentCreated, Actor{ID: "42", Role: "admin"}, "article", "item-1"); err != nil {
		t.Fatalf("RecordContent: %v", err)
	}

	rows, err := l.ListByPlugin(ctx, "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Action != ActionContentCreated || !r.Allowed || r.PluginName != "content" {
		t.Errorf("row = %+v, want action=%s allowed plugin_name=content", r, ActionContentCreated)
	}
	if r.OccurredAt.IsZero() {
		t.Error("occurred_at must be stamped")
	}
	d := parseDetail(t, r)
	if d.ActorID != "42" || d.Role != "admin" || d.ItemID != "item-1" || d.Type != "article" {
		t.Errorf("detail = %+v, want actor_id=42 role=admin item_id=item-1 type=article", d)
	}
}

// TestRecordMediaAndUserPersistStableDetail pins the media/user recorders:
// GIVEN an editor actor (actor_id unknown at the domain boundary), rows are
// stamped plugin "media"/"identity" with type "media"/"user" and the item_id
// carried (media id / user id as string).
func TestRecordMediaAndUserPersistStableDetail(t *testing.T) {
	l := testAuditLogger(t)
	ctx := context.Background()
	actor := Actor{ID: "", Role: "editor"}

	if err := l.RecordMedia(ctx, ActionMediaUploaded, actor, "media-9"); err != nil {
		t.Fatalf("RecordMedia: %v", err)
	}
	if err := l.RecordUser(ctx, ActionUserRoleChanged, actor, 7); err != nil {
		t.Fatalf("RecordUser: %v", err)
	}

	mediaRows, err := l.ListByPlugin(ctx, "media")
	if err != nil {
		t.Fatal(err)
	}
	if len(mediaRows) != 1 || mediaRows[0].Action != ActionMediaUploaded {
		t.Fatalf("media rows = %+v, want one %s", mediaRows, ActionMediaUploaded)
	}
	md := parseDetail(t, mediaRows[0])
	if md.ActorID != "" || md.Role != "editor" || md.ItemID != "media-9" || md.Type != "media" {
		t.Errorf("media detail = %+v, want actor_id=\"\" role=editor item_id=media-9 type=media", md)
	}

	userRows, err := l.ListByPlugin(ctx, "identity")
	if err != nil {
		t.Fatal(err)
	}
	if len(userRows) != 1 || userRows[0].Action != ActionUserRoleChanged {
		t.Fatalf("user rows = %+v, want one %s", userRows, ActionUserRoleChanged)
	}
	ud := parseDetail(t, userRows[0])
	if ud.ActorID != "" || ud.Role != "editor" || ud.ItemID != "7" || ud.Type != "user" {
		t.Errorf("user detail = %+v, want actor_id=\"\" role=editor item_id=7 type=user", ud)
	}
}

// TestRecordersAppendInOrder pins append-only ordering: rows come back from
// ListByPlugin oldest-first (the endpoint's presentation order).
func TestRecordersAppendInOrder(t *testing.T) {
	l := testAuditLogger(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := l.RecordContent(ctx, ActionContentCreated, Actor{Role: "admin"}, "article", "i"); err != nil {
			t.Fatalf("RecordContent #%d: %v", i, err)
		}
	}
	rows, err := l.ListByPlugin(ctx, "content")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].ID <= rows[i-1].ID {
			t.Errorf("rows not oldest-first: id %d after %d", rows[i].ID, rows[i-1].ID)
		}
	}
}
