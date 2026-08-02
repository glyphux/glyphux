// Ticket T7 (gap 4): item-level CRUD auditing. These are the
// domain-facing recorders the content/media/identity write paths call —
// RecordContent/RecordMedia/RecordUser each persist one append-only row via
// Logger.Log with the stable Detail payload {actor_id, role, item_id, type}
// (the audit contract; every key always present so the payload is
// trivially parseable by any client).
//
// Convention (owner-confirmed resolutions for the T7 spec ambiguities):
//   - PluginName is stamped "content" / "media" / "identity" so item-level
//     rows are queryable through the endpoint's only accessor, ListByPlugin
//     (GET /api/v0/audit?plugin=<stamp>).
//   - Actor.ID is "" whenever the recording layer can't see the
//     authenticated user — the domain boundary sees role-only
//     permission.Principals by design, and CreateUser has no principal at
//     all (setup/self-service path). HTTP-layer actor enrichment is out of
//     scope this round (constructor-wiring only, per the spec).
//   - The 13 action constants are defined here once, shared by every
//     caller; media rows carry type "media" and user rows "user".
package audit

import (
	"context"
	"encoding/json"
	"strconv"
)

// The 13 item-level write actions, defined once per the spec ("define 13
// action constants once in recorders.go"). The action string is part of the
// audit contract — the hostapi.* boundary-gate actions (pkg/sdk) are a
// separate vocabulary.
const (
	ActionContentCreated     = "content.created"
	ActionContentUpdated     = "content.updated"
	ActionContentDeleted     = "content.deleted"
	ActionContentPublished   = "content.published"
	ActionContentUnpublished = "content.unpublished"
	ActionContentRolledBack  = "content.rolled_back"

	ActionMediaUploaded = "media.uploaded"
	ActionMediaUpdated  = "media.updated"
	ActionMediaDeleted  = "media.deleted"

	ActionUserCreated     = "user.created"
	ActionUserRoleChanged = "user.role_changed"
	ActionUserDeactivated = "user.deactivated"
	ActionUserReactivated = "user.reactivated"
)

// Actor is who performed the audited action: the acting user's id ("" when
// unknown — anonymous callers, setup-time writes, or domain boundaries that
// only see role-only principals) and their role.
type Actor struct {
	ID   string
	Role string
}

// detailPayload is the stable JSON Detail shape — the audit contract.
type detailPayload struct {
	ActorID string `json:"actor_id"`
	Role    string `json:"role"`
	ItemID  string `json:"item_id"`
	Type    string `json:"type"`
}

func detailJSON(actor Actor, itemID, typ string) string {
	b, err := json.Marshal(detailPayload{ActorID: actor.ID, Role: actor.Role, ItemID: itemID, Type: typ})
	if err != nil {
		// Unreachable — the payload is plain strings; keep the contract
		// stable even if json.Marshal ever grows a failure mode.
		return `{"actor_id":"` + actor.ID + `","role":"` + actor.Role + `","item_id":"` + itemID + `","type":"` + typ + `"}`
	}
	return string(b)
}

// RecordContent logs one content write (created|updated|deleted|published|
// unpublished|rolled_back) against item id of typeName, stamped plugin
// "content".
func (l *Logger) RecordContent(ctx context.Context, action string, actor Actor, typeName, id string) error {
	return l.Log(ctx, Record{
		PluginName: "content",
		Action:     action,
		Allowed:    true,
		Detail:     detailJSON(actor, id, typeName),
	})
}

// RecordMedia logs one media write (uploaded|updated|deleted) against media
// item id, stamped plugin "media" with type "media".
func (l *Logger) RecordMedia(ctx context.Context, action string, actor Actor, id string) error {
	return l.Log(ctx, Record{
		PluginName: "media",
		Action:     action,
		Allowed:    true,
		Detail:     detailJSON(actor, id, "media"),
	})
}

// RecordUser logs one identity write (created|role_changed|deactivated|
// reactivated) against the subject userID, stamped plugin "identity" with
// type "user" and item_id the user id.
func (l *Logger) RecordUser(ctx context.Context, action string, actor Actor, userID int64) error {
	return l.Log(ctx, Record{
		PluginName: "identity",
		Action:     action,
		Allowed:    true,
		Detail:     detailJSON(actor, strconv.FormatInt(userID, 10), "user"),
	})
}
