package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorageRawReferencesIncludesTruncatedFields(t *testing.T) {
	s := storageStore(t)
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		t.Fatal(err)
	}
	r, err := s.openStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, raw := range []string{`{"resumeOf":"20261021-085712-69dfbc-truncated`, `{"resumeOf":"prefix20261021-085712-69dfbc`, `{"id":"corrupt","resumeOf":"20261021-085712-69dfbc","other":"unrelated"}`} {
		if err := os.WriteFile(filepath.Join(s.storageHome(), "raw.json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		refs, err := storageRawReferences(r, "raw.json", "corrupt", map[string]bool{"20261021-085712-69dfbc": true, "corrupt": true, "unrelated": true, "absent": true})
		if err != nil || !refs["20261021-085712-69dfbc"] || refs["corrupt"] || refs["absent"] {
			t.Fatalf("raw %q: refs=%v err=%v", raw, refs, err)
		}
	}
}

func TestStorageAgyReceiptPlanningReadOnly(t *testing.T) {
	s := storageStore(t)
	home := "harness-homes/agy"
	legacy := filepath.Join(s.storageHome(), home, ".pactify-pruned-sessions.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(legacy, map[string]bool{"deleted": true}); err != nil {
		t.Fatal(err)
	}
	r, err := s.openStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := storageVerifyAgy(context.Background(), r, home, []string{"deleted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lstat("storage-receipts"); !os.IsNotExist(err) {
		t.Fatalf("read-only receipt verification created durable receipts: %v", err)
	}
	if _, err := os.Lstat(legacy); err != nil {
		t.Fatalf("planning removed legacy receipt: %v", err)
	}
}

func TestStorageIncompleteDirectoryReasonNamesCreationGrace(t *testing.T) {
	s := storageStore(t)
	path := s.Dir("incomplete")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanStorage(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	item := storageItem(plan, "jobs/incomplete")
	if item == nil || !item.Protected || !strings.Contains(item.Reason, "one-hour creation grace") || strings.Contains(item.Reason, "within retention") {
		t.Fatalf("misleading crash-leftover reason: %+v", item)
	}
}
