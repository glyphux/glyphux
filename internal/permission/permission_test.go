package permission_test

import (
	"testing"

	"github.com/glyphux/glyphux/internal/permission"
)

func TestAdminCanWriteContent(t *testing.T) {
	if !permission.Allows("admin", permission.ContentWrite) {
		t.Error("admin should hold content:write")
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
