package sub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSummaryIndexUsesSnapshot(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	cwd := t.TempDir()
	parent, err := s.Create("parent", Request{Harness: "fake", Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, s, parent.ID, StatusSucceeded)
	parent, _ = s.Load(parent.ID)
	child, err := s.ResumeRun("child", parent.Request, false, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	index := s.NewSummaryIndex([]State{parent, child})
	// After indexing, actions, result metadata and reverse links must not scan or
	// reload the job store. A removed snapshot source makes such reads visible.
	if err := os.RemoveAll(s.Root); err != nil {
		t.Fatal(err)
	}
	row := index.Summarize(parent, time.Now())
	if !reflect.DeepEqual(row.ResumedBy, []string{child.ID}) || len(row.Actions) != 0 || row.SessionID != "session-parent" {
		t.Fatal(row)
	}
}

func benchmarkSummaryJobs(b *testing.B, n int) {
	b.Helper()
	b.Setenv("PACTIFY_SUB_FAKE", "1")
	s := Store{Root: filepath.Join(b.TempDir(), "jobs")}
	root := b.TempDir()
	states := make([]State, 0, n)
	for j := 0; j < n; j++ {
		id := fmt.Sprintf("job-%03d", j)
		st, err := s.Create(id, Request{Harness: "fake", Cwd: root})
		if err != nil {
			b.Fatal(err)
		}
		st.Status = StatusSucceeded
		st.Worktree = &Worktree{Owner: id, Path: root, Cwd: root, OriginalRoot: root}
		if err := s.Save(st); err != nil {
			b.Fatal(err)
		}
		data, _ := json.Marshal(Result{SessionID: id})
		if err := os.WriteFile(s.ResultPath(id), data, 0600); err != nil {
			b.Fatal(err)
		}
		states = append(states, st)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		index := s.NewSummaryIndex(states)
		for _, st := range states {
			index.Summarize(st, time.Now())
		}
	}
}
func BenchmarkSummaryJobs(b *testing.B) {
	for _, n := range []int{100, 300} {
		b.Run(fmt.Sprint(n), func(b *testing.B) { benchmarkSummaryJobs(b, n) })
	}
}
