// Package forms is the PRD §14/§2.9 dogfood capability: a first-party
// plugin built ENTIRELY on the public extension API (pkg/sdk), the same
// path a third-party author would use. It imports pkg/sdk and pkg/contract
// (both public), and internal/content solely for the *content.Item type
// pkg/sdk.ContentAPI's own method signatures already expose to an
// in-process (Tier A) plugin — it never imports or calls internal/content's
// API directly; every actual data access flows through the HostAPI passed
// into Register (PRD §8.3: "the ONLY surface a plugin can reach").
package forms

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// contentTypeName is the single content type this capability defines: every
// form's every submission is one item of this type, disambiguated by its
// "form_name" field. A judgment call (see this slice's tracking doc):
// one shared content type rather than a dynamically-registered type per
// form name, since content types are a structural/schema concern and form
// names are runtime data, not structure.
const contentTypeName = "form_submission"

// Plugin is the forms capability's sdk.Plugin implementation.
type Plugin struct{}

// New returns a forms Plugin.
func New() *Plugin { return &Plugin{} }

// Manifest declares this capability's identity and its two consent-relevant
// axes: content[read,write] (PRD §7.2: "forms -> content"). No permissions
// axis — forms needs no admin_ui/scheduled_jobs/network grant for this
// slice's scope.
func (p *Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    "forms",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read", "write"}},
		},
	}
}

// Register defines the form_submission content type through host — the
// ONLY call this capability makes to set itself up; a denial here (e.g. a
// host built from a manifest that dropped content:write) propagates
// unmodified, proving Register carries no bypass of pkg/sdk's own gate.
func (p *Plugin) Register(host sdk.HostAPI) error {
	return host.RegisterContentType(context.Background(), contentTypeName, contract.ContentType{
		Fields: map[string]contract.Field{
			"form_name": {Type: contract.FieldString, Required: true},
			"data":      {Type: contract.FieldString, Required: true},
		},
	})
}

// Submit records one submission to formName, JSON-encoding data into the
// content type's "data" field, entirely through host.Content() — never a
// direct store/DB call. Returns the new item's ID.
func Submit(ctx context.Context, host sdk.HostAPI, formName string, data map[string]any) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("forms: encode submission data: %w", err)
	}
	item, err := host.Content().Create(ctx, contentTypeName, map[string]any{
		"form_name": formName,
		"data":      string(encoded),
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

// List returns the IDs of every submission recorded against formName, in
// whatever order host.Content().List returns them.
func List(ctx context.Context, host sdk.HostAPI, formName string) ([]string, error) {
	items, err := host.Content().List(ctx, contentTypeName)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, item := range items {
		if name, ok := formNameOf(item); ok && name == formName {
			ids = append(ids, item.ID)
		}
	}
	return ids, nil
}

// formNameOf extracts the "form_name" field pkg/sdk.ContentAPI's
// *content.Item.Data carries, if present and a string.
func formNameOf(item *content.Item) (string, bool) {
	v, ok := item.Data["form_name"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
