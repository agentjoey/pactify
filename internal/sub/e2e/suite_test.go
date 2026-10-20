//go:build harness_e2e

// Package e2e tests the built CLI across real harness process boundaries.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"github.com/agentjoey/pactify/internal/credentiallinks"
	"github.com/agentjoey/pactify/internal/sub"
)

type outcome struct {
	Scenario string   `json:"scenario"`
	Status   string   `json:"status"`
	Reason   string   `json:"reason"`
	Jobs     []string `json:"jobs,omitempty"`
	Seconds  float64  `json:"seconds"`
}
type report struct {
	Harness         string          `json:"harness"`
	Model           string          `json:"model"`
	Version         string          `json:"version"`
	Commit          string          `json:"commit"`
	Started         time.Time       `json:"started"`
	QuotaBefore     json.RawMessage `json:"quotaBefore,omitempty"`
	QuotaAfter      json.RawMessage `json:"quotaAfter,omitempty"`
	QuotaAfterError string          `json:"quotaAfterError,omitempty"`
	Results         []outcome       `json:"results"`
}
type suite struct {
	owner                             *testing.T
	bin, source, harness, model, user string
	quarantine                        string
	adapter                           sub.Harness
	report                            report
}
type fixture struct {
	credentials        map[string]string
	hostFallbacks      []string
	credentialsChecked bool
	preserveRoot       bool
	s                  *suite
	t                  *testing.T
	root, repo         string
	env                []string
	jobs               []string
	failures           []string
	related            []*fixture
}

func TestHarnessE2E(t *testing.T) {
	h := os.Getenv("PACTIFY_E2E_HARNESS")
	if h != "codex" && h != "opencode" && h != "agy" && h != "kimi" {
		t.Fatal("PACTIFY_E2E_HARNESS must be codex, opencode, agy or kimi")
	}
	source, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	user, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	s := &suite{owner: t, source: source, harness: h, user: user, model: os.Getenv("PACTIFY_E2E_MODEL")}
	harness, _ := sub.Lookup(h)
	s.adapter = harness
	version := ""
	if h == "opencode" {
		// Resolve the protocol before choosing credential links or user roots.
		s.adapter, version, err = fixtureHarness(context.Background(), harness)
		if err != nil {
			t.Fatal("harness selection: ", err)
		}
	}
	quarantine, err := credentiallinks.QuarantineDir()
	if err != nil {
		t.Fatal(err)
	}
	s.quarantine, err = filepath.Abs(quarantine)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Credential quarantine outside fixture roots: %s", s.quarantine)
	s.bin = filepath.Join(t.TempDir(), "pactify")
	// Build in the source checkout; no caller-installed pactify can mask the diff.
	out, err := command(2*time.Minute, nil, source, "", "go", "build", "-o", s.bin, "./cmd/pactify")
	if err != nil {
		t.Fatalf("build: %v %s", err, safe(out))
	}
	commit, _ := command(time.Second*10, nil, source, "", "git", "rev-parse", "HEAD")
	s.report = report{Version: version, Harness: h, Started: time.Now().UTC(), Commit: strings.TrimSpace(string(commit))}
	before, err := metadata(s.userRoots(), false)
	if err != nil {
		t.Fatal(err)
	}
	// Persist completed rows even when a subtest fails. No credential contents enter reports.
	defer func() {
		if path := os.Getenv("PACTIFY_E2E_REPORT"); path != "" {
			b, e := json.MarshalIndent(s.report, "", "  ")
			if e == nil {
				e = os.WriteFile(path, []byte(golden.Sanitize(string(b))+"\n"), 0600)
			}
			if e != nil {
				t.Errorf("report: %v", e)
			}
		}
	}()
	setup := s.newFixture(t, true)
	if h != "opencode" {
		// Other harnesses retain their original isolated fixture version command.
		version, err := command(15*time.Second, setup.env, setup.repo, "", h, "--version")
		s.report.Version = strings.TrimSpace(safe(version))
		if err != nil {
			t.Logf("version unavailable: %v", err)
		}
	}
	setupErr := s.selectModel(setup)
	s.report.Model = s.model
	quotaErr := ""
	if h == "agy" {
		q, e := setup.quota()
		if e != nil {
			quotaErr = "quota safety check unavailable: " + e.Error()
		} else {
			s.report.QuotaBefore = q
			var wire struct {
				Command struct {
					Data struct {
						Groups []struct {
							Name    string `json:"name"`
							Buckets []struct {
								ID        string   `json:"id"`
								Window    string   `json:"window"`
								Remaining *float64 `json:"remaining_fraction"`
							} `json:"buckets"`
						} `json:"groups"`
					} `json:"data"`
				} `json:"command"`
			}
			_ = json.Unmarshal(q, &wire)
			found := false
			for _, g := range wire.Command.Data.Groups {
				for _, b := range g.Buckets {
					if strings.Contains(strings.ToLower(g.Name+" "+b.ID), "gemini") && !strings.Contains(strings.ToLower(g.Name+" "+b.ID), "argon") && (strings.Contains(strings.ToLower(b.Window), "week") || strings.Contains(strings.ToLower(b.ID), "week")) {
						found = true
						if b.Remaining == nil || *b.Remaining < 0 || *b.Remaining > 1 {
							quotaErr = "Gemini weekly quota value unavailable; live calls skipped"
							continue
						}
						if *b.Remaining < .2 {
							quotaErr = fmt.Sprintf("Gemini weekly quota %.0f%% below 20%%; live calls skipped", *b.Remaining*100)
						}
					}
				}
			}
			if !found {
				quotaErr = "Gemini weekly bucket not identified; live calls skipped"
			}
		}
	}
	scenarios := []struct {
		name string
		run  func(*fixture) string
	}{
		{"write_worktree", writeWorktree}, {"readonly_denied", readonlyDenied}, {"readonly_long", readonlyLong}, {"resume", resume}, {"cancel", cancelJob}, {"long_tool", longTool}, {"large_output", largeOutput}, {"parallel", parallelJobs}, {"model", modelCheck}, {"failure_surface", failureSurface}, {"records", records}, {"instructions", instructions},
	}
	selected := map[string]bool{}
	if v := os.Getenv("PACTIFY_E2E_SCENARIOS"); v != "" {
		for _, name := range strings.Split(v, ",") {
			selected[strings.TrimSpace(name)] = true
		}
	}
	valid := map[string]bool{"user_dirs_clean": true, "host_path": true}
	for _, sc := range scenarios {
		valid[sc.name] = true
	}
	for name := range selected {
		if !valid[name] {
			t.Fatalf("unknown scenario %q", name)
		}
	}
	run := func(name string, fn func(*fixture) string) {
		if len(selected) > 0 && !selected[name] {
			return
		}
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			f := s.newFixture(t, true)
			reason := ""
			status := "PASS"
			t.Cleanup(func() {
				f.checkCredentials()
				for _, child := range f.related {
					child.checkCredentials()
					f.failures = append(f.failures, child.failures...)
				}
				if len(f.failures) > 0 {
					t.Fail()
				}
				if t.Failed() {
					status = "FAIL"
					reason = "checks failed: " + strings.Join(f.failures, "; ") + "; " + reason
					f.diagnose()
				}
				if reason == "" {
					reason = "CLI and observable evidence matched"
				}
				row := outcome{name, status, strings.Join(strings.Fields(reason), " "), f.jobIDs(), time.Since(start).Seconds()}
				s.report.Results = append(s.report.Results, row)
				fmt.Printf("E2E %s %s %s %s\n", h, name, status, row.Reason)
			})
			if setupErr != nil && name != "failure_surface" && name != "user_dirs_clean" {
				reason = "model setup failed: " + setupErr.Error()
				t.Error(reason)
				return
			}
			if quotaErr != "" && name != "user_dirs_clean" {
				reason = quotaErr
				t.Error(reason)
				return
			}
			reason = fn(f)
		})
	}
	for _, sc := range scenarios {
		run(sc.name, sc.run)
	}
	if os.Getenv("PACTIFY_E2E_HOST") == "1" {
		run("host_path", hostPath)
	}
	if h == "agy" {
		if q, e := setup.quota(); e == nil {
			s.report.QuotaAfter = q
		} else {
			s.report.QuotaAfterError = safe([]byte(e.Error()))
			t.Errorf("closing quota: %s", s.report.QuotaAfterError)
		}
	}
	run("user_dirs_clean", func(f *fixture) string {
		setup.checkCredentials()
		after, e := metadata(s.userRoots(), false)
		if e != nil {
			t.Fatal(e)
		}
		metadataHarness := h
		if s.adapter.Opencode2 {
			metadataHarness = "opencode2"
		} // No user DB or credential refresh is expected in v2.
		changes, refresh := compareMetadata(before, after, metadataHarness)
		if len(changes) > 0 {
			f.errorf("user directory metadata changed: %s", safe([]byte(strings.Join(changes, ", "))))
		}
		return fmt.Sprintf("metadata changes=%d; credential refresh paths=%v (metadata only)", len(changes), refresh)
	})
}

func command(timeout time.Duration, env []string, dir, input, binary string, args ...string) ([]byte, error) {
	ctx, stop := context.WithTimeout(context.Background(), timeout)
	defer stop()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	var out, diagnostics bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostics
	err := cmd.Run()
	if err != nil {
		out.Write(diagnostics.Bytes())
	}
	if ctx.Err() != nil {
		return out.Bytes(), ctx.Err()
	}
	return out.Bytes(), err
}
func safe(b []byte) string { return golden.SanitizeText(string(b)) }
func (f *fixture) cli(input string, args ...string) ([]byte, error) {
	return command(45*time.Second, f.env, f.repo, input, f.s.bin, append([]string{"sub"}, args...)...)
}
func (f *fixture) state(id string) sub.State {
	b, e := f.cli("", "status", id, "--json")
	if e != nil {
		f.fatalf("status %s: %v %s", id, e, safe(b))
	}
	var st sub.State
	if json.Unmarshal(b, &st) != nil {
		f.t.Fatal("invalid status JSON")
	}
	return st
}
func (f *fixture) start(prompt string, flags ...string) string {
	args := []string{"run", f.s.harness, "--cwd", f.repo, "--wait", "0s", "--json", "--retries", "1"}
	if f.s.model != "" {
		args = append(args, "--model", f.s.model)
	}
	args = append(args, flags...)
	b, e := f.cli(prompt, args...)
	var r struct {
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(b, &r)
	if r.JobID == "" {
		f.fatalf("start: %v %s", e, safe(b))
	}
	f.trackJob(r.JobID)
	return r.JobID
}

func (f *fixture) trackJob(id string) {
	f.jobs = append(f.jobs, id)
	f.t.Cleanup(func() {
		st := f.state(id)
		if st.Status == sub.StatusRunning || st.Status == sub.StatusQueued {
			if _, err := f.cli("", "cancel", id); err != nil {
				f.preserveRoot = true
				f.errorf("job %s cancellation failed; fixture retained at %s", id, f.root)
			}
		}
	})
}
func (f *fixture) finish(id string, timeout time.Duration) sub.Result {
	b, e := command(timeout+15*time.Second, f.env, f.repo, "", f.s.bin, "sub", "wait", id, "--timeout", timeout.String(), "--json")
	var r sub.Result
	if json.Unmarshal(b, &r) != nil || r.Status == sub.StatusRunning || r.Status == "" {
		_, _ = f.cli("", "cancel", id)
		f.fatalf("wait %s: %v %s", id, e, safe(b))
	}
	return r
}
func (f *fixture) success(r sub.Result) {
	if r.Status != sub.StatusSucceeded || strings.TrimSpace(r.FinalText) == "" {
		f.fatalf("job %s status=%s class=%s warnings=%s", r.JobID, r.Status, r.FailureClass, safe([]byte(strings.Join(r.Warnings, "; "))))
	}
}
func (f *fixture) run(prompt string, flags ...string) sub.Result {
	return f.finish(f.start(prompt, flags...), 5*time.Minute)
}
func (f *fixture) diagnose() {
	for _, child := range f.related {
		child.diagnose()
	}
	for _, id := range f.jobs {
		b, _ := f.cli("", "status", id, "--json")
		var st map[string]any
		_ = json.Unmarshal(b, &st)
		keys := []string{"status", "error", "failureClass", "sessionId", "modelRequested", "modelActual", "duration", "worktree", "retryCount", "processTrackingError", "inputTokens", "outputTokens", "usagePartial"}
		fields := map[string]any{"jobId": id}
		for _, k := range keys {
			if v, ok := st[k]; ok {
				fields[k] = v
			}
		}
		v, _ := json.Marshal(fields)
		f.t.Log(safe(v))
		b, _ = f.cli("", "logs", id, "--tail", "40")
		f.t.Logf("job %s logs: %s", id, safe(b))
	}
}

func (f *fixture) errorf(format string, args ...any) {
	msg := safe([]byte(fmt.Sprintf(format, args...)))
	f.failures = append(f.failures, msg)
	f.t.Error(msg)
}
func (f *fixture) fatalf(format string, args ...any) {
	msg := safe([]byte(fmt.Sprintf(format, args...)))
	f.failures = append(f.failures, msg)
	f.t.Fatal(msg)
}
func (f *fixture) error(args ...any) { f.errorf("%s", fmt.Sprint(args...)) }
func (f *fixture) fatal(args ...any) { f.fatalf("%s", fmt.Sprint(args...)) }

func (f *fixture) jobIDs() []string {
	ids := append([]string{}, f.jobs...)
	for _, child := range f.related {
		ids = append(ids, child.jobIDs()...)
	}
	return ids
}
