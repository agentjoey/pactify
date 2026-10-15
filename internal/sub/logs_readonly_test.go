package sub

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestLogsReadOnlyDoesNotRecoverOrphan(t *testing.T) {
	store := Store{Root: t.TempDir()}
	st, err := store.Create("orphan", Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusRunning
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LogPath(st.ID), []byte("ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := store.Logs(context.Background(), st.ID, &out, LogOptions{Tail: 10, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	current, err := store.Load(st.ID)
	if err != nil || current.Status != StatusRunning {
		t.Fatalf("read mutated job: %+v %v", current, err)
	}
	if out.String() != "stderr: ready\n" {
		t.Fatal(out.String())
	}
}
