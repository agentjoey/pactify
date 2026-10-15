package sub

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/orchestrate"
)

func TestJobIDFormat(t *testing.T) {
	id := NewJobID(time.Date(2026, 10, 15, 9, 15, 0, 0, time.UTC), bytes.NewReader([]byte{0xa1, 0xb2, 0xc3}))
	if id != "20261015-091500-a1b2c3" {
		t.Fatalf("id = %q", id)
	}
}

func TestStoreRoundTripAndLastSession(t *testing.T) {
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
	got, err := s.LastSessionFor("codex", "/repo")
	if err != nil || got != "th-9" {
		t.Fatalf("last session = %q, %v", got, err)
	}
	for _, pair := range [][2]string{{"kimi", "/repo"}, {"codex", "/other"}} {
		if got, err := s.LastSessionFor(pair[0], pair[1]); err != nil || got != "" {
			t.Fatalf("other request matched %q, %v", got, err)
		}
	}
	if s.LogPath("j1") != filepath.Join(s.Dir("j1"), "log.jsonl") {
		t.Fatal("wrong log path")
	}
	entries, err := os.ReadDir(s.Dir("j1"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
}

func TestStoreLastSessionUsesFinishedAt(t *testing.T) {
	s := Store{Root: t.TempDir()}
	for _, tc := range []struct {
		id, session string
		status      Status
		finished    int64
	}{
		{"z-old", "old", StatusSucceeded, 1}, {"a-new", "new", StatusSucceeded, 2}, {"failed", "bad", StatusFailed, 3},
	} {
		st, err := s.Create(tc.id, Request{Harness: "kimi", Cwd: "/repo"})
		if err != nil {
			t.Fatal(err)
		}
		st.Status, st.FinishedAt = tc.status, time.Unix(tc.finished, 0).UTC()
		if err := s.Save(st); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.ResultPath(tc.id), []byte(`{"sessionId":"`+tc.session+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.LastSessionFor("kimi", "/repo"); got != "new" || err != nil {
		t.Fatalf("last = %q, %v", got, err)
	}
}

func TestStoreDefaultAndMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	s, err := DefaultStore()
	if err != nil || s.Root != filepath.Join(home, "jobs") {
		t.Fatalf("store = %+v, %v", s, err)
	}
	if got, err := s.LastSessionFor("codex", "/repo"); got != "" || err != nil {
		t.Fatalf("empty store = %q, %v", got, err)
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

func TestStoreLastSessionSkipsInvalidStates(t *testing.T) {
	s := Store{Root: t.TempDir()}
	for _, id := range []string{"a-corrupt", "b-missing", "c-unreadable"} {
		if err := os.Mkdir(s.Dir(id), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.Dir("a-corrupt"), "state.json"), []byte(`{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory at the file path reliably produces a read error, even as root.
	if err := os.Mkdir(filepath.Join(s.Dir("c-unreadable"), "state.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := s.Create("z-good", Request{Harness: "codex", Cwd: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status, st.FinishedAt = StatusSucceeded, time.Unix(1, 0).UTC()
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ResultPath(st.ID), []byte(`{"sessionId":"good-session"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LastSessionFor("codex", "/repo"); got != "good-session" || err != nil {
		t.Fatalf("last session = %q, %v; want good-session", got, err)
	}
}

func TestStoreLastSessionSkipsInvalidResults(t *testing.T) {
	for _, damage := range []string{"missing", "corrupt", "unreadable"} {
		t.Run(damage, func(t *testing.T) {
			s := Store{Root: t.TempDir()}
			for i, id := range []string{"good", "newer"} {
				st, err := s.Create(id, Request{Harness: "codex", Cwd: "/repo"})
				if err != nil {
					t.Fatal(err)
				}
				st.Status, st.FinishedAt = StatusSucceeded, time.Unix(int64(i+1), 0).UTC()
				if err := s.Save(st); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(s.ResultPath("good"), []byte(`{"sessionId":"good-session"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "corrupt":
				if err := os.WriteFile(s.ResultPath("newer"), []byte(`{"sessionId":`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if err := os.Mkdir(s.ResultPath("newer"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := s.LastSessionFor("codex", "/repo"); got != "good-session" || err != nil {
				t.Fatalf("last session = %q, %v; want good-session", got, err)
			}
		})
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

func TestCrossVendorStripExport(t *testing.T) {
	for _, tc := range []struct{ kind, own string }{{"codex-cli", "OPENAI_API_KEY="}, {"kimi-cli", "KIMI_API_KEY="}} {
		got := orchestrate.CrossVendorStrip(tc.kind)
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
	if got := orchestrate.CrossVendorStrip("custom"); got != nil {
		t.Fatalf("unknown kind: %v", got)
	}
}
