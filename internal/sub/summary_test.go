package sub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func summaryJSON(t *testing.T, s Store, st State, peers []State) map[string]any {
	t.Helper()
	data, err := json.Marshal(s.Summarize(st, peers, st.CreatedAt.Add(10*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSummaryTitleWithoutIssueExtraction(t *testing.T) {
	s := Store{Root: t.TempDir()}
	for _, brief := range []string{"# Short title\nAJ-701 in body", "Background:\nA later line"} {
		out := summaryJSON(t, s, State{Request: Request{Brief: brief}}, nil)
		if _, exists := out["issue"]; exists {
			t.Errorf("issue must not be emitted: %v", out)
		}
		want := strings.TrimPrefix(strings.Split(brief, "\n")[0], "# ")
		if out["title"] != want {
			t.Errorf("title=%q, want first line %q", out["title"], want)
		}
	}
}

func TestSummaryMetadataAndLegacy(t *testing.T) {
	s := Store{Root: t.TempDir()}
	root := repo(t)
	cwd := filepath.Join(root, "nested")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	st, err := s.Create("metadata", Request{Harness: "codex", Model: "requested", Effort: "high", Access: "write", Cwd: cwd, Brief: "\n # **AJ-657** `任务摘要`\nfollow up AJ-658"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusFailed
	st.FinishedAt = st.CreatedAt.Add(3 * time.Second)
	result := Result{ModelActual: "actual", CostUSD: 0.125, InputTokens: 12, OutputTokens: 4, SessionID: "session", Warnings: []string{"warning"}, Diff: Diff{Files: []FileDelta{{Path: "a"}}, Added: 5, Deleted: 2}}
	if err := atomicJSON(s.ResultPath(st.ID), result); err != nil {
		t.Fatal(err)
	}
	out := summaryJSON(t, s, st, nil)
	for key, want := range map[string]any{"id": "metadata", "title": "AJ-657 任务摘要", "harness": "codex", "modelRequested": "requested", "modelActual": "actual", "effort": "high", "costUSD": 0.125, "access": "write", "repo": NormalizeRepo(root), "cwd": cwd, "duration": "3s", "inputTokens": float64(12), "outputTokens": float64(4), "changedFiles": float64(1), "addedLines": float64(5), "deletedLines": float64(2), "warningCount": float64(1), "sessionId": "session"} {
		if out[key] != want {
			t.Errorf("%s=%v, want %v", key, out[key], want)
		}
	}
	if len(out["attention"].([]any)) != 2 {
		t.Fatalf("attention=%v", out["attention"])
	}
	// Old records and missing/corrupt result remain readable, without inventing metadata.
	st.Repo = ""
	st.ConfigMode = ""
	st.Request.Brief = ""
	st.Request.Cwd = "/missing/repo"
	st.Status = StatusSucceeded
	if err := os.WriteFile(s.ResultPath(st.ID), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	out = summaryJSON(t, s, st, nil)
	for _, key := range []string{"title", "repo", "configMode", "resumeOf", "sessionId"} {
		if out[key] != "" {
			t.Errorf("legacy %s=%v", key, out[key])
		}
	}
	if _, ok := out["costUSD"]; ok {
		t.Errorf("unknown cost invented: %v", out)
	}
	if _, ok := out["startedAt"]; ok {
		t.Errorf("legacy startedAt invented: %v", out)
	}
}

func TestSummaryBriefBounds(t *testing.T) {
	s := Store{}
	cases := []struct{ brief, title string }{
		{" \n### **标题** `code`\nAJ-123", "标题 code"},
		{"\n\n", ""},
		{"title\n1\n2\n3\n4\n5\n6\n7\n8\n9\nAJ-99", "title"},
		{strings.Repeat("字", 100), strings.Repeat("字", 79) + "…"},
	}
	for _, tc := range cases {
		out := summaryJSON(t, s, State{Request: Request{Brief: tc.brief}}, nil)
		if out["title"] != tc.title || !utf8.ValidString(out["title"].(string)) {
			t.Errorf("brief: %v", out)
		}
	}
}

func TestSummaryActivityBoundedTail(t *testing.T) {
	s := Store{Root: t.TempDir()}
	st, err := s.Create("live", Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusRunning
	f, err := os.Create(s.LogPath(st.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Sparse prefix proves reading depends only on the bounded tail, not total log size.
	if _, err = f.Seek(1<<30, 0); err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n" + `{"kind":"message","text":"old","final":true}` + "\n" + `{"kind":"usage","usage":{"inputTokens":1,"outputTokens":2},"raw":{"threadId":"main","tokenUsage":{"total":{"inputTokens":100,"outputTokens":20}}}}` + "\n" + `{"kind":"tool","tool":{"name":"shell","phase":"start","text":"go test\nsecret second line"}}` + "\n" + `{"kind":"message","text":"unfinished`)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	out := summaryJSON(t, s, st, nil)
	a, ok := out["activity"].(map[string]any)
	if !ok {
		t.Fatalf("activity=%v", out)
	}
	if a["summary"] != "tool shell go test" || a["inputTokens"] != float64(100) || a["outputTokens"] != float64(20) {
		t.Errorf("activity=%v", a)
	}
	if len(out["attention"].([]any)) != 0 {
		t.Errorf("running attention=%v", out)
	}
	st.Status = StatusSucceeded
	if _, ok := summaryJSON(t, s, st, nil)["activity"]; ok {
		t.Error("terminal activity")
	}
}

func TestSummaryResumeAndWorktreeLifecycle(t *testing.T) {
	s, first := worktreeFixture(t)
	finishWorktree(t, s, first.ID, StatusSucceeded)
	first, _ = s.Load(first.ID)
	out := summaryJSON(t, s, first, nil)
	if out["cwd"] != first.Worktree.Cwd || out["repo"] != first.Worktree.OriginalRoot || out["worktree"].(map[string]any)["status"] != "pending" || len(out["attention"].([]any)) != 1 {
		t.Fatalf("pending=%v", out)
	}
	if err := s.ApplyWorktree(first.ID); err != nil {
		t.Fatal(err)
	}
	first, _ = s.Load(first.ID)
	if got := summaryJSON(t, s, first, nil)["worktree"].(map[string]any)["status"]; got != "applied" {
		t.Fatalf("apply=%v", got)
	}
	next, err := s.CreateRun("next", first.Request, false, true)
	if err != nil {
		t.Fatal(err)
	}
	out = summaryJSON(t, s, next, []State{first, next})
	if out["resumeOf"] != first.ID || out["worktree"].(map[string]any)["status"] != "pending" {
		t.Fatalf("resume=%v", out)
	}
	parent := summaryJSON(t, s, first, []State{first, next})
	if got := parent["resumedBy"].([]any); len(got) != 1 || got[0] != "next" {
		t.Fatalf("children=%v", parent)
	}
	finishWorktree(t, s, next.ID, StatusSucceeded)
	if err := s.DiscardWorktree(next.ID); err != nil {
		t.Fatal(err)
	}
	first, _ = s.Load(first.ID)
	out = summaryJSON(t, s, first, nil)
	if out["worktree"].(map[string]any)["status"] != "discarded" || len(out["attention"].([]any)) != 0 {
		t.Fatalf("discard=%v", out)
	}
}

func TestSummaryPlainResumeAndStartedAt(t *testing.T) {
	s := Store{Root: t.TempDir()}
	first, err := s.CreateRun("first", Request{Harness: "codex", Cwd: repo(t)}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	started := first.CreatedAt.Add(time.Second)
	if _, err := s.claim(t.Context(), first.ID, started); err != nil {
		t.Fatal(err)
	}
	first, _ = s.Load(first.ID)
	out := summaryJSON(t, s, first, nil)
	if out["startedAt"] != started.Format(time.RFC3339Nano) {
		t.Errorf("started=%v", out)
	}
	if err := s.Finish(first.ID, &Result{Status: StatusSucceeded, SessionID: "session"}, "", started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	next, err := s.CreateRun("next", first.Request, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if out := summaryJSON(t, s, next, nil); out["resumeOf"] != "first" {
		t.Errorf("resume=%v", out)
	}
}

func TestSummaryBriefFirstNonemptyLine(t *testing.T) {
	title := briefSummary(strings.Repeat("\n", 12) + "# first\nsecond AJ-99")
	if title != "first" {
		t.Fatalf("title=%q", title)
	}
	title = briefSummary("***\nsecond")
	if title != "" {
		t.Fatalf("first nonempty line changed: %q", title)
	}
}

func TestSummaryActivityLatestAndReadable(t *testing.T) {
	s := Store{Root: t.TempDir()}
	st, err := s.Create("readable", Request{})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusRunning
	cases := []struct{ log, want string }{
		{`{"kind":"message","text":"hel"}` + "\n" + `{"kind":"message","text":"lo"}` + "\n", "message hello"},
		{`{"kind":"message","text":"old message"}` + "\n" + `{"kind":"tool","tool":{"name":"shell","phase":"output","text":"latest output"}}` + "\n", "tool shell latest output"},
		{`{"kind":"tool","tool":{"name":"shell","phase":"start","text":"run"}}` + "\n" + `{"kind":"tool","tool":{"name":"shell","phase":"end"},"raw":{"item":{"exitCode":2,"aggregatedOutput":"bad result"}}}` + "\n", "tool shell exit 2: bad result"},
		{`{"kind":"message","text":"first"}` + "\n" + `{"kind":"message","text":"replacement","final":true}` + "\n", "message replacement"},
		{`{"kind":"usage","usage":{"inputTokens":0,"outputTokens":0}}` + "\n", ""},
	}
	for _, tc := range cases {
		if err := os.WriteFile(s.LogPath(st.ID), []byte(tc.log), 0600); err != nil {
			t.Fatal(err)
		}
		a := s.Summarize(st, nil, time.Now()).Activity
		if a.Summary != tc.want {
			t.Errorf("activity=%q, want %q", a.Summary, tc.want)
		}
	}
}

func TestSummaryApplyRealPatchAndFailure(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[conflict], func(t *testing.T) {
			s, st := worktreeFixture(t)
			write(t, st.Worktree.Path, "a.txt", "changed\n")
			if conflict {
				write(t, st.Request.Cwd, "a.txt", "user edit\n")
			}
			finishWorktree(t, s, st.ID, StatusSucceeded)
			err := s.ApplyWorktree(st.ID)
			if (err != nil) != conflict {
				t.Fatalf("apply: %v", err)
			}
			st, err = s.Load(st.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := WorktreeApplied
			if conflict {
				want = WorktreePending
			}
			if got := s.Summarize(st, nil, time.Now()).Worktree.Status; got != want {
				t.Errorf("status=%s, want %s", got, want)
			}
		})
	}
}

func TestSummaryMarkdownClosingHeading(t *testing.T) {
	title := briefSummary("## **task** `code` ##\nbody")
	if title != "task code" {
		t.Fatalf("title=%q", title)
	}
}

func TestBriefSummaryKeepsIdentifiersAndFirstLine(t *testing.T) {
	title := briefSummary("# AJ-643：kimi 改用 **独立** 目录（`KIMI_CODE_HOME`）\n")
	if title != "AJ-643：kimi 改用 独立 目录（KIMI_CODE_HOME）" {
		t.Fatalf("title=%q", title)
	}
	if title := briefSummary("Background:\n\nFix the stdin of orchestrate workers.\n"); title != "Background:" {
		t.Fatalf("first line changed: %q", title)
	}
	if title := briefSummary("## 背景\n背景：\n修复 stdin\n"); title != "背景" {
		t.Fatalf("heading without colon is a real title: %q", title)
	}
}

func TestActionableWarningsIgnoresInformationalNotes(t *testing.T) {
	h, _ := Lookup("kimi")
	r := Result{Harness: "kimi", Warnings: []string{h.Experimental, dirtyWorktreeWarning, "kimi 2.2.0 has not been checked; run pactify sub doctor first"}}
	if n := actionableWarnings(r); n != 0 {
		t.Fatalf("informational warnings counted: %d", n)
	}
	r.Warnings = append(r.Warnings, "Actual model does not match the requested model")
	if n := actionableWarnings(r); n != 1 {
		t.Fatalf("actionable warning not counted: %d", n)
	}
}

func TestSummaryInformationalWarnings(t *testing.T) {
	s := Store{Root: t.TempDir()}
	st, err := s.Create("informational", Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusSucceeded
	r := Result{Warnings: []string{partialUsageWarning, "CLI version changed: 1.0 → 1.1"}}
	for _, actionable := range []bool{false, true} {
		if actionable {
			r.Warnings = append(r.Warnings, "empty final reply after one follow-up: the harness ended without a text answer. Check pactify sub logs informational, or resume to ask for the result.")
		}
		if err := atomicJSON(s.ResultPath(st.ID), r); err != nil {
			t.Fatal(err)
		}
		row := s.Summarize(st, nil, time.Now())
		if row.WarningCount != len(r.Warnings) || (len(row.Attention) > 0) != actionable {
			t.Fatalf("actionable=%v: %+v", actionable, row)
		}
		if actionable && (len(row.Attention) != 1 || row.Attention[0].Kind != AttentionWarnings) {
			t.Fatalf("empty reply must remain actionable: %v", row.Attention)
		}
	}
}

func TestSummaryAcknowledgesSharedWorktree(t *testing.T) {
	for _, action := range []string{"apply", "discard"} {
		t.Run(action, func(t *testing.T) {
			s, first := worktreeFixture(t)
			finish := func(id string) {
				t.Helper()
				r := Result{JobID: id, Status: StatusFailed, SessionID: "session", FailureClass: "other", Warnings: []string{"review this"}}
				if err := s.Finish(id, &r, "failed", time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			finish(first.ID)
			next, err := s.CreateRun("resumed", first.Request, false, true)
			if err != nil {
				t.Fatal(err)
			}
			finish(next.ID)
			write(t, first.Worktree.Path, "a.txt", "changed\n")
			for _, id := range []string{first.ID, next.ID} {
				st, err := s.Load(id)
				if err != nil {
					t.Fatal(err)
				}
				if row := s.Summarize(st, nil, time.Now()); len(row.Attention) != 3 || row.Worktree.Status != WorktreePending {
					t.Fatalf("pending %s: %+v", id, row)
				}
			}
			want := WorktreeApplied
			if action == "apply" {
				err = s.ApplyWorktree(next.ID)
			} else {
				want = WorktreeDiscarded
				err = s.DiscardWorktree(next.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{first.ID, next.ID} {
				st, err := s.Load(id)
				if err != nil {
					t.Fatal(err)
				}
				row := s.Summarize(st, nil, time.Now())
				if len(row.Attention) != 0 || row.Status != StatusFailed || row.FailureClass != "other" || row.WarningCount != 1 || row.Worktree.Status != want || !row.DismissedAt.IsZero() {
					t.Fatalf("acknowledged %s: %+v", id, row)
				}
			}
		})
	}
}
