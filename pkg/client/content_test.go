package client_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glyphux/glyphux/pkg/client"
)

func loggedInClient(t *testing.T) *client.Client {
	t.Helper()
	ts, identities, _ := newTestServer(t)
	ctx := context.Background()
	if err := identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	c := client.New(ts.URL)
	if _, err := c.Auth.Login(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	return c
}

func TestContentCreateAndGet(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	created, err := c.Content.Create(ctx, "article", map[string]any{"title": "Hello", "body": "World"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create: item has no id")
	}
	if created.Data["title"] != "Hello" {
		t.Errorf("Create: data[title] = %v, want Hello", created.Data["title"])
	}
	if created.Status != "draft" {
		t.Errorf("Create: status = %q, want draft", created.Status)
	}

	got, err := c.Content.Get(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Data["title"] != "Hello" {
		t.Errorf("Get: data[title] = %v, want Hello", got.Data["title"])
	}
}

func TestContentListUpdateDelete(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	created, err := c.Content.Create(ctx, "article", map[string]any{"title": "Hello", "body": "World"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	items, err := c.Content.List(ctx, "article")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("List = %+v, want one item with id %q", items, created.ID)
	}

	updated, err := c.Content.Update(ctx, "article", created.ID, map[string]any{"title": "Updated", "body": "World"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Data["title"] != "Updated" {
		t.Errorf("Update: data[title] = %v, want Updated", updated.Data["title"])
	}

	if err := c.Content.Delete(ctx, "article", created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Content.Get(ctx, "article", created.ID); err == nil {
		t.Fatal("Get after Delete: want error, got nil")
	}
}

func TestContentPublishUnpublishVersionsRollback(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	created, err := c.Content.Create(ctx, "article", map[string]any{"title": "v1", "body": "World"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	published, err := c.Content.Publish(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if published.Status != "published" {
		t.Errorf("Publish: status = %q, want published", published.Status)
	}

	if _, err := c.Content.Update(ctx, "article", created.ID, map[string]any{"title": "v2", "body": "World"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	versions, err := c.Content.ListVersions(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) < 2 {
		t.Fatalf("ListVersions = %+v, want at least 2 versions", versions)
	}

	rolled, err := c.Content.Rollback(ctx, "article", created.ID, 1)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rolled.Data["title"] != "v1" {
		t.Errorf("Rollback: data[title] = %v, want v1", rolled.Data["title"])
	}

	unpublished, err := c.Content.Unpublish(ctx, "article", created.ID)
	if err != nil {
		t.Fatalf("Unpublish: %v", err)
	}
	if unpublished.Status != "draft" {
		t.Errorf("Unpublish: status = %q, want draft", unpublished.Status)
	}
}

func TestContentCreateValidationFailureExposesIssues(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	// "title" is required on the article type (see newTestServer); omitting
	// it must fail validation with the field issue surfaced on the error.
	_, err := c.Content.Create(ctx, "article", map[string]any{"body": "World"})
	var apiErr *client.APIError
	if !client.AsAPIError(err, &apiErr) {
		t.Fatalf("Create with missing required field error = %v (%T), want *client.APIError", err, err)
	}
	if apiErr.StatusCode != 422 {
		t.Fatalf("Create validation error status = %d, want 422", apiErr.StatusCode)
	}
	var issues []string
	if err := json.Unmarshal(apiErr.Issues, &issues); err != nil {
		t.Fatalf("decode APIError.Issues: %v (raw %s)", err, apiErr.Issues)
	}
	if len(issues) == 0 {
		t.Fatal("APIError.Issues is empty, want at least one validation issue")
	}
}

func TestContentGetUnknownIDReturnsAPIError(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()

	_, err := c.Content.Get(ctx, "article", "does-not-exist")
	var apiErr *client.APIError
	if !client.AsAPIError(err, &apiErr) {
		t.Fatalf("Get unknown id error = %v (%T), want *client.APIError", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("Get unknown id status = %d, want 404", apiErr.StatusCode)
	}
}
