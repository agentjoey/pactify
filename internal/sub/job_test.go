package sub

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/vendorenv"
)

func TestJobIDFormat(t *testing.T) {
	id := NewJobID(time.Date(2026, 10, 15, 9, 15, 0, 0, time.UTC), bytes.NewReader([]byte{0xa1, 0xb2, 0xc3}))
	if id != "20261015-091500-a1b2c3" {
		t.Fatalf("id = %q", id)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := Store{Root: t.TempDir()}
	req := Request{Harness: "codex", Cwd: "/repo", Access: "write", Brief: "b", Model: "m", Effort: "high", Resume: "old"}
	st, err := s.Create("j1", req)
	if err != nil || st.Status != StatusQueued || st.CreatedAt.IsZero() || st.UpdatedAt.IsZero() {
		t.Fatalf("create: %v %+v", err, st)
	}
	st.Status, st.PID, st.FinishedAt = StatusSucceeded, 123, time.Now().UTC()
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	gotState, err := s.Load("j1")
	if err != nil || !reflect.DeepEqual(gotState, st) {
		t.Fatalf("load: %+v, %v; want %+v", gotState, err, st)
	}
	if err := os.WriteFile(s.ResultPath("j1"), []byte(`{"sessionId":"th-9","status":"succeeded"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.LogPath("j1") != filepath.Join(s.Dir("j1"), "log.jsonl") {
		t.Fatal("wrong log path")
	}
	entries, err := os.ReadDir(s.Dir("j1"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
}

func TestStoreDefaultAndMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	s, err := DefaultStore()
	if err != nil || s.Root != filepath.Join(home, "jobs") {
		t.Fatalf("store = %+v, %v", s, err)
	}
	if _, err := s.Load("missing"); !os.IsNotExist(err) {
		t.Fatalf("missing load: %v", err)
	}
	t.Setenv("PACTIFY_HOME", "")
	t.Setenv("HOME", home)
	s, err = DefaultStore()
	if err != nil || s.Root != filepath.Join(home, ".pactify", "jobs") {
		t.Fatalf("default store = %+v, %v", s, err)
	}
}

func TestStoreRejectsDuplicateAndUnsafeIDs(t *testing.T) {
	s := Store{Root: t.TempDir()}
	first, err := s.Create("job", Request{Brief: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("job", Request{Brief: "replace"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	got, err := s.Load("job")
	if err != nil || !reflect.DeepEqual(first, got) {
		t.Fatalf("duplicate replaced state: %+v %v", got, err)
	}
	for _, id := range []string{"", ".", "..", "../outside", "/absolute", "nested/job"} {
		if _, err := s.Create(id, Request{}); err == nil {
			t.Fatalf("create accepted %q", id)
		}
		if err := s.Save(State{ID: id}); err == nil {
			t.Fatalf("save accepted %q", id)
		}
		if _, err := s.Load(id); err == nil {
			t.Fatalf("load accepted %q", id)
		}
	}
}

func TestVendorEnvStripForSubKinds(t *testing.T) {
	for _, tc := range []struct{ kind, own string }{{"codex-cli", "OPENAI_API_KEY="}, {"kimi-cli", "KIMI_API_KEY="}} {
		got := vendorenv.Strip(tc.kind)
		if len(got) == 0 {
			t.Fatalf("no isolation for %s", tc.kind)
		}
		found := false
		for _, entry := range got {
			if entry == tc.own {
				t.Fatalf("own key stripped: %s", entry)
			}
			if entry == "ANTHROPIC_API_KEY=" {
				found = true
			}
		}
		if !found {
			t.Fatalf("sibling key not stripped: %v", got)
		}
	}
	if got := vendorenv.Strip("custom"); got != nil {
		t.Fatalf("unknown kind: %v", got)
	}
}
