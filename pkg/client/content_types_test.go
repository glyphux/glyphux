package client_test

import (
	"context"
	"testing"

	"github.com/glyphux/glyphux/pkg/client"
	"github.com/glyphux/glyphux/pkg/contract"
)

func TestContentTypesDefineListDelete(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	defined, err := c.ContentTypes.Define(ctx, "product", contract.ContentType{
		Fields: map[string]contract.Field{
			"name": {Type: contract.FieldString, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if len(defined.Fields) != 1 {
		t.Fatalf("Define: fields = %v, want 1", defined.Fields)
	}

	types, err := c.ContentTypes.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, ok := types["product"]; !ok {
		t.Fatalf("List = %v, want product present", types)
	}
	// "article" is seeded by newTestServer.
	if _, ok := types["article"]; !ok {
		t.Fatalf("List = %v, want article present", types)
	}

	if err := c.ContentTypes.Delete(ctx, "product"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	types, err = c.ContentTypes.List(ctx)
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if _, ok := types["product"]; ok {
		t.Fatalf("List after Delete = %v, want product absent", types)
	}
}

func TestContentTypesDeleteBlockedWhenItemsExist(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	if _, err := c.Content.Create(ctx, "article", map[string]any{"title": "x", "body": "y"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := c.ContentTypes.Delete(ctx, "article")
	var apiErr *client.APIError
	if !client.AsAPIError(err, &apiErr) {
		t.Fatalf("Delete with items error = %v (%T), want *client.APIError", err, err)
	}
	if apiErr.StatusCode != 409 {
		t.Errorf("Delete with items status = %d, want 409", apiErr.StatusCode)
	}
}
