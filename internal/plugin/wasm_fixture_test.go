package plugin_test

import (
	"os"
	"path/filepath"
	"testing"
)

// readWasmFixture loads the committed events_guest.wasm fixture from
// pkg/runtime/wasm/testdata (the same fixture pkg/runtime/wasm's own tests
// use; see that package's REBUILD note).
func readWasmFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "pkg", "runtime", "wasm", "testdata", "events_guest.wasm"))
	if err != nil {
		t.Fatalf("read events_guest.wasm: %v", err)
	}
	return b
}
