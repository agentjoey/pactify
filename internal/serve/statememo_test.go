package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

// appendLogLine appends one raw event line to root's .pact/log.jsonl.
func appendLogLine(t *testing.T, root, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(root, ".pact", "log.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append log: %v", err)
	}
}

const assignLine = `{"event_id":"2","ts":"2026-01-02T00:00:00Z","agent_id":"claude-opus","role":"orchestrator","event_type":"assign","task_id":"T1","feature":"F","payload":{"owner":"claude-opus","reviewer":"claude-opus","branch":"feat/x","spec":"s"}}`

// TestProjectStateFullMatchesUncached: the memoized full fold must be
// indistinguishable from the uncached ProjectState, on first read and on a
// repeat read.
func TestProjectStateFullMatchesUncached(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	s := New([]registry.Project{{Name: "pactify", Path: root}})

	want, err := ProjectState(root)
	if err != nil {
		t.Fatalf("ProjectState: %v", err)
	}
	for i := 0; i < 2; i++ { // second pass exercises the memo-hit path
		got, evs, err := s.projectStateFull("pactify", root)
		if err != nil {
			t.Fatalf("projectStateFull #%d: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("projectStateFull #%d = %+v, want %+v", i, got, want)
		}
		if len(evs) != 1 || evs[0].EventType != "init" {
			t.Fatalf("projectStateFull #%d events = %+v", i, evs)
		}
	}
}

// TestProjectStateFullServesFromMemo proves the second identical read is a
// cache hit (not a silent refold): poison the cached DTO and observe it
// returned while the log is unchanged.
func TestProjectStateFullServesFromMemo(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	s := New([]registry.Project{{Name: "pactify", Path: root}})

	if _, _, err := s.projectStateFull("pactify", root); err != nil {
		t.Fatalf("prime: %v", err)
	}
	s.memoMu.Lock()
	m := s.stateMemos["pactify"]
	m.dto.Project = "SENTINEL"
	s.stateMemos["pactify"] = m
	s.memoMu.Unlock()

	got, _, err := s.projectStateFull("pactify", root)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got.Project != "SENTINEL" {
		t.Fatalf("unchanged log must hit the memo; got project %q", got.Project)
	}
}

// TestProjectStateFullRefreshesAfterAppend: an append (external writer, no
// fsnotify involvement) must invalidate the memo via the size/mtime stamp.
func TestProjectStateFullRefreshesAfterAppend(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	s := New([]registry.Project{{Name: "pactify", Path: root}})

	if _, evs, err := s.projectStateFull("pactify", root); err != nil || len(evs) != 1 {
		t.Fatalf("prime: evs=%d err=%v", len(evs), err)
	}
	appendLogLine(t, root, assignLine)
	dto, evs, err := s.projectStateFull("pactify", root)
	if err != nil {
		t.Fatalf("after append: %v", err)
	}
	if len(evs) != 2 || evs[1].EventType != "assign" || len(dto.Features) != 1 {
		t.Fatalf("memo did not refresh after append: dto=%+v evs=%+v", dto, evs)
	}
}

// TestProjectStateFullMissingLog matches ProjectState's missing-log contract:
// empty fold, no error.
func TestProjectStateFullMissingLog(t *testing.T) {
	s := New([]registry.Project{{Name: "p", Path: t.TempDir()}})
	dto, evs, err := s.projectStateFull("p", t.TempDir())
	if err != nil {
		t.Fatalf("missing log: %v", err)
	}
	if len(evs) != 0 || dto.Agents == nil || dto.Features == nil {
		t.Fatalf("want empty non-null fold, got dto=%+v evs=%+v", dto, evs)
	}
}

// TestHandleStateMemoIdenticalAndFresh: GET /state (the memoized path) returns
// byte-identical bodies for an unchanged log and fresh state after an append.
func TestHandleStateMemoIdenticalAndFresh(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	s := New([]registry.Project{{Name: "pactify", Path: root}})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	get := func() string {
		resp, err := http.Get(ts.URL + "/api/projects/pactify/state")
		if err != nil {
			t.Fatalf("GET state: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("state: want 200 got %d", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	first := get()
	if second := get(); second != first {
		t.Fatalf("unchanged log, differing bodies:\n%s\n%s", first, second)
	}
	appendLogLine(t, root, assignLine)
	var st StateDTO
	if err := json.Unmarshal([]byte(get()), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(st.Features) != 1 || st.Features[0].ID != "F" {
		t.Fatalf("state after append not fresh: %+v", st)
	}
}

// TestFoldStatusSingleRead: the restructured foldStatus still reports seats
// and lastEventTs from its single (memoized) log read.
func TestFoldStatusSingleRead(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	s := New([]registry.Project{{Name: "pactify", Path: root}})

	st := s.foldStatus("pactify", root)
	if st.Seats != 1 || st.LastEventTs != "2026-01-01T00:00:00Z" {
		t.Fatalf("foldStatus = %+v", st)
	}
	appendLogLine(t, root, assignLine)
	if st = s.foldStatus("pactify", root); st.LastEventTs != "2026-01-02T00:00:00Z" {
		t.Fatalf("foldStatus after append = %+v", st)
	}
}

// TestRemoveProjectDropsMemo: removing (or renaming) a project must not leave
// its folded state cached under the old id.
func TestRemoveProjectDropsMemo(t *testing.T) {
	root := t.TempDir()
	seedProject(t, root, "pactify")
	s := New([]registry.Project{{Name: "pactify", Path: root}})
	if _, _, err := s.projectStateFull("pactify", root); err != nil {
		t.Fatalf("prime: %v", err)
	}
	if err := s.RemoveProject("pactify"); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	s.memoMu.Lock()
	_, ok := s.stateMemos["pactify"]
	s.memoMu.Unlock()
	if ok {
		t.Fatal("memo entry survived RemoveProject")
	}
}
