package subpanel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func fixture(t *testing.T) (*Server, sub.Store) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		st, err := store.Create(fmt.Sprintf("job-%02d", i), sub.Request{Harness: "codex", Brief: "# Example-659 Local panel", Cwd: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		st.Status = sub.StatusSucceeded
		st.CreatedAt = time.Date(2026, 10, 16, 0, i, 0, 0, time.UTC)
		st.FinishedAt = st.CreatedAt.Add(time.Minute)
		st.Repo = "/repo/a"
		if i == 11 {
			st.Status = sub.StatusRunning
			st.FinishedAt = time.Time{}
		}
		if i == 10 {
			st.ResumeOf = "job-00"
			st.Repo = "/repo/b"
		}
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	result := sub.Result{JobID: "job-00", Harness: "codex", Status: sub.StatusSucceeded, FinalText: "Finished **panel**", Diff: sub.Diff{Files: []sub.FileDelta{{Path: "main.go", Added: 2}}}}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(store.ResultPath("job-00"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	return s, store
}
func request(s *Server, method, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestAuthenticationAndReadOnlyRoutes(t *testing.T) {
	s, _ := fixture(t)
	for _, path := range []string{"/api/jobs", "/api/jobs/job-00", "/api/jobs/job-00/diff", "/api/jobs/job-00/logs"} {
		for _, token := range []string{"", "wrong"} {
			if w := request(s, "GET", path, token); w.Code != 401 {
				t.Fatalf("%s: %d", path, w.Code)
			}
		}
		if w := request(s, "POST", path, s.token); w.Code != 405 {
			t.Fatalf("write route %s: %d", path, w.Code)
		}
	}
	if w := request(s, "GET", "/api/jobs?token="+s.token, ""); w.Code != 401 {
		t.Fatal("API accepted query token")
	}
	w := request(s, "GET", "/?token="+s.launchToken, "")
	if w.Code != 200 || len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), `content="`+s.token+`"`) {
		t.Fatalf("exchange: %d %v", w.Code, w.Header())
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/jobs", nil)
	r.Header.Set("Authorization", "Bearer "+s.token)
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, r)
	if out.Code != 200 {
		t.Fatal(out.Code)
	}
	r.Host = "attacker.example"
	out = httptest.NewRecorder()
	s.Handler().ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal("untrusted host accepted")
	}
	other, _ := New(s.store)
	if other.token == s.token || len(s.token) < 40 {
		t.Fatal("token not random")
	}
	if w := request(s, "GET", "/", s.token); w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("embedded UI: %d", w.Code)
	}
	if w := request(s, "GET", "/state.json", s.token); w.Code != 404 {
		t.Fatal("unknown asset exposed")
	}
}
func TestListDetailAndRecordedDiff(t *testing.T) {
	s, store := fixture(t)
	for _, tc := range []struct {
		query string
		n     int
	}{{"", 10}, {"?all=1", 12}, {"?all=1&repo=/repo/b", 1}, {"?repo=/missing", 0}} {
		w := request(s, "GET", "/api/jobs"+tc.query, s.token)
		var rows []sub.JobSummary
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(rows) != tc.n {
			t.Fatalf("list %s: %d %s", tc.query, w.Code, w.Body.String())
		}
		if tc.n > 1 && rows[0].ID != "job-11" {
			t.Fatal("ordering")
		}
	}
	w := request(s, "GET", "/api/jobs/job-00", s.token)
	var d Detail
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Brief != "# Example-659 Local panel" || d.Result.FinalText != "Finished **panel**" || !strings.Contains(d.Markdown, "Final reply") || len(d.Job.ResumedBy) != 1 {
		t.Fatalf("detail: %s", w.Body.String())
	}
	w = request(s, "GET", "/api/jobs/job-00/diff", s.token)
	var diff DiffResponse
	_ = json.Unmarshal(w.Body.Bytes(), &diff)
	if w.Code != 200 || len(diff.Files) != 1 || diff.Files[0].Path != "main.go" || diff.Note == "" {
		t.Fatalf("recorded diff: %s", w.Body.String())
	}
	if w := request(s, "GET", "/api/jobs/missing", s.token); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if err := os.WriteFile(store.ResultPath("job-00"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	w = request(s, "GET", "/api/jobs/job-00", s.token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "resultError") {
		t.Fatal(w.Body.String())
	}
	// Queries never recover or write orphan running jobs.
	st, _ := store.Load("job-11")
	if st.Status != sub.StatusRunning {
		t.Fatal("read changed state")
	}
}
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func TestWorktreeDiffIncludesAllChangesAndIsBounded(t *testing.T) {
	s, store := fixture(t)
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.name", "Test")
	git(t, repo, "config", "user.email", "test@example.invalid")
	write := func(name, text string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(repo, name), []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("tracked.txt", "before\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "base")
	base := git(t, repo, "rev-parse", "HEAD")
	write("tracked.txt", "after\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "worker commit")
	write("staged.txt", "staged\n")
	git(t, repo, "add", "staged.txt")
	write("new.txt", "new\n")
	st, _ := store.Load("job-00")
	st.Worktree = &sub.Worktree{Path: repo, BaseCommit: base, OriginalRoot: repo}
	if e := store.Save(st); e != nil {
		t.Fatal(e)
	}
	before := git(t, repo, "status", "--porcelain=v1")
	w := request(s, "GET", "/api/jobs/job-00/diff", s.token)
	var d DiffResponse
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	for _, text := range []string{"tracked.txt", "staged.txt", "new.txt", "+after"} {
		if !strings.Contains(d.Patch, text) {
			t.Fatalf("missing %s: %d %s", text, w.Code, d.Patch)
		}
	}
	if after := git(t, repo, "status", "--porcelain=v1"); before != after {
		t.Fatal("diff changed index")
	}
	write("huge.txt", strings.Repeat("long line\n", 100000))
	w = request(s, "GET", "/api/jobs/job-00/diff", s.token)
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if !d.Truncated || len(d.Patch) > MaxDiffBytes || d.Note == "" {
		t.Fatalf("unbounded diff: %d %v", len(d.Patch), d.Truncated)
	}
	if e := os.RemoveAll(repo); e != nil {
		t.Fatal(e)
	}
	w = request(s, "GET", "/api/jobs/job-00/diff", s.token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "note") {
		t.Fatal(w.Body.String())
	}
}
func TestSSETailFollowPartialAndClose(t *testing.T) {
	s, store := fixture(t)
	path := store.LogPath("job-11")
	if e := os.WriteFile(path, []byte(strings.Repeat("old\n", 120)+"{\"kind\":\"message\",\"text\":\"hel"), 0600); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/jobs/job-11/logs", nil)
	r.Header.Set("Authorization", "Bearer "+s.token)
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(resp.Header)
	}
	// The server flushes the initial tail before following.
	first := make([]byte, 1)
	if _, e := io.ReadFull(resp.Body, first); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.WriteString("lo\",\"final\":true}\n")
	_ = f.Close()
	if e != nil {
		t.Fatal(e)
	}
	st, _ := store.Load("job-11")
	st.Status = sub.StatusSucceeded
	if e := store.Save(st); e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	body := string(first) + string(data)
	if strings.Count(body, "stderr: old") != 100 || !strings.Contains(body, "message hello") || strings.Contains(body, "message hel\"") || !strings.Contains(body, "event: done") {
		t.Fatal(body)
	}
	// A disconnected client must not leave the handler waiting.
	st.Status = sub.StatusRunning
	_ = store.Save(st)
	ctx2, cancel2 := context.WithCancel(context.Background())
	r, _ = http.NewRequestWithContext(ctx2, "GET", server.URL+"/api/jobs/job-11/logs", nil)
	r.Header.Set("Authorization", "Bearer "+s.token)
	resp, e = http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	cancel2()
	_ = resp.Body.Close()
}
func TestListenAndShutdown(t *testing.T) {
	s, _ := fixture(t)
	ln, e := Listen(0)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") {
		t.Fatal(ln.Addr())
	}
	u, _ := url.Parse(s.URL(ln))
	if u.Query().Get("token") != s.launchToken {
		t.Fatal(u)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown hung")
	}
	for _, port := range []int{-1, 65536} {
		if ln, e := Listen(port); e == nil {
			ln.Close()
			t.Fatal("invalid port")
		}
	}
}

func TestShutdownClosesActiveLogStream(t *testing.T) {
	s, _ := fixture(t)
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/api/jobs/job-11/logs", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cancel()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("active SSE prevented shutdown")
	}
}

func TestDiffNeverFollowsUntrackedSymlinks(t *testing.T) {
	s, store := fixture(t)
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.name", "Test")
	git(t, repo, "config", "user.email", "test@example.invalid")
	git(t, repo, "commit", "--allow-empty", "-qm", "base")
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("outside file contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load("job-00")
	st.Worktree = &sub.Worktree{Path: repo, OriginalRoot: repo, BaseCommit: git(t, repo, "rev-parse", "HEAD")}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/jobs/job-00/diff", s.token)
	if w.Code != 200 || strings.Contains(w.Body.String(), "outside file contents") || !strings.Contains(w.Body.String(), "120000") {
		t.Fatal(w.Body.String())
	}
}

func TestPanelSessionIsScopedToLaunch(t *testing.T) {
	s, _ := fixture(t)
	if w := request(s, "GET", "/?token="+s.launchToken, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	next, err := New(s.store)
	if err != nil {
		t.Fatal(err)
	}
	if w := request(next, "GET", "/api/jobs", s.token); w.Code != 401 {
		t.Fatal("previous launch session accepted")
	}
	if w := request(s, "GET", "/api/jobs", "wrong"); w.Code != 401 {
		t.Fatal("incorrect session accepted")
	}
	if w := request(s, "GET", "/?token=wrong", ""); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("invalid exchange accepted")
	}
}
func TestEmptyPanelList(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/jobs?all=1", s.token)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty list: %d %s", w.Code, w.Body.String())
	}
}

func TestDiffPreservesTextAfterNonUTF8Bytes(t *testing.T) {
	s, store := fixture(t)
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.name", "Test")
	git(t, repo, "config", "user.email", "test@example.invalid")
	git(t, repo, "commit", "--allow-empty", "-qm", "base")
	if err := os.WriteFile(filepath.Join(repo, "latin.txt"), []byte("\xff\nreadable after invalid byte\n"), 0600); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load("job-00")
	st.Worktree = &sub.Worktree{Path: repo, OriginalRoot: repo, BaseCommit: git(t, repo, "rev-parse", "HEAD")}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/jobs/job-00/diff", s.token)
	var diff DiffResponse
	if err := json.Unmarshal(w.Body.Bytes(), &diff); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "readable after invalid byte") {
		t.Fatalf("lost readable content: %s", diff.Patch)
	}
}
