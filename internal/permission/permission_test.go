package permission_test

import (
	"testing"

	"github.com/glyphux/glyphux/internal/permission"
)

func TestAdminHoldsEveryCapability(t *testing.T) {
	for _, c := range []permission.Capability{
		permission.ContentRead, permission.ContentReadDrafts, permission.ContentWrite,
		permission.ContentPublish, permission.MediaWrite, permission.UsersManage,
		permission.ContentTypesManage,
	} {
		if !permission.Allows(permission.RoleAdmin, c) {
			t.Errorf("admin should hold %s", c)
		}
	}
}

func TestOnlyAdminHoldsContentTypesManage(t *testing.T) {
	if permission.Allows(permission.RoleEditor, permission.ContentTypesManage) {
		t.Error("editor should not hold content_types:manage")
	}
	if permission.Allows(permission.RoleViewer, permission.ContentTypesManage) {
		t.Error("viewer should not hold content_types:manage")
	}
}

func TestEditorCanWriteButNotPublishOrManageUsers(t *testing.T) {
	if !permission.Allows(permission.RoleEditor, permission.ContentWrite) {
		t.Error("editor should hold content:write")
	}
	if !permission.Allows(permission.RoleEditor, permission.ContentReadDrafts) {
		t.Error("editor should hold content:read_drafts")
	}
	if permission.Allows(permission.RoleEditor, permission.ContentPublish) {
		t.Error("editor should not hold content:publish")
	}
	if permission.Allows(permission.RoleEditor, permission.UsersManage) {
		t.Error("editor should not hold users:manage")
	}
}

func TestViewerIsReadOnlyAndCannotSeeDrafts(t *testing.T) {
	if !permission.Allows(permission.RoleViewer, permission.ContentRead) {
		t.Error("viewer should hold content:read")
	}
	if permission.Allows(permission.RoleViewer, permission.ContentReadDrafts) {
		t.Error("viewer should not hold content:read_drafts")
	}
	if permission.Allows(permission.RoleViewer, permission.ContentWrite) {
		t.Error("viewer should not hold content:write")
	}
}

func TestUnknownRoleHasNoCapabilities(t *testing.T) {
	if permission.Allows("guest", permission.ContentWrite) {
		t.Error("unknown role should not hold content:write")
	}
	if permission.Allows("", permission.ContentRead) {
		t.Error("empty role should not hold content:read")
	}
}

func TestValidRoleRecognizesKnownRolesOnly(t *testing.T) {
	for _, r := range []string{permission.RoleAdmin, permission.RoleEditor, permission.RoleViewer} {
		if !permission.ValidRole(r) {
			t.Errorf("ValidRole(%q) = false, want true", r)
		}
	}
	if permission.ValidRole("superuser") {
		t.Error("ValidRole(superuser) = true, want false")
	}
}
