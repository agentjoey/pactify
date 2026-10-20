package sub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain must keep the package's default store out of the developer's ~/.pactify.
func TestDefaultStoreIsIsolated(t *testing.T) {
	user, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(user, ".pactify")
	if rel, err := filepath.Rel(real, store.Root); err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("default store %s is inside the real user store %s", store.Root, real)
	}
}
