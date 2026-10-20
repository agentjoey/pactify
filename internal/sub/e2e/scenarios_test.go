//go:build harness_e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/sub"
)

func randomWord() string {
	var b [6]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func writeWorktree(f *fixture) string {
	r := f.run("Create e2e.txt containing exactly alpha followed by a newline. Replace existing.txt with exactly after followed by a newline. Use tools. Do not commit. Reply E2E_WRITE_DONE.", "--worktree")
	f.success(r)
	if r.WorktreePath == "" || r.WorktreePath == f.repo {
		f.fatal("missing isolated worktree")
	}
	f.file(r.WorktreePath, "e2e.txt", "alpha\n")
	f.file(r.WorktreePath, "existing.txt", "after\n")
	f.clean()
	f.file(f.repo, "existing.txt", "before\n")
	b, e := f.cli("", "apply", r.JobID)
	if e != nil {
		f.fatalf("apply: %v %s", e, safe(b))
	}
	f.file(f.repo, "e2e.txt", "alpha\n")
	f.file(f.repo, "existing.txt", "after\n")
	b, e = f.cli("", "discard", r.JobID)
	if e != nil {
		f.fatalf("discard: %v %s", e, safe(b))
	}
	if _, e = os.Lstat(r.WorktreePath); !os.IsNotExist(e) {
		f.error("discard did not remove worktree")
	}
	return "isolated changes, apply and discard verified"
}
func readonlyDenied(f *fixture) string {
	before, e := metadata([]string{f.repo}, true)
	must(f.t, e)
	prompt := "Read existing.txt using the read tool. Do not create denied-shell.txt or denied-tool.txt. Explain briefly that writing tools are unavailable. Do not seek a workaround. Finish with E2E_DENIED_DONE."
	var r sub.Result
	if readonlyToolPolicy(f.s.harness, f.s.adapter.Opencode2) {
		r = f.run(prompt, "--read-only")
	} else {
		r = f.run("Authorized read-only permission diagnostic: actually attempt the shell command `echo x > denied-shell.txt` once, then attempt to create denied-tool.txt containing x using your dedicated file write/edit tool once. Both writes must be denied. Do not simulate calls or merely explain the policy. Report actual tool errors and finish with E2E_DENIED_DONE.", "--read-only")
	}

	f.success(r)
	after, e := metadata([]string{f.repo}, true)
	must(f.t, e)
	if !reflect.DeepEqual(before, after) {
		f.error("read-only repository metadata changed")
	}
	f.clean()
	for _, n := range []string{"denied-shell.txt", "denied-tool.txt"} {
		if _, e := os.Lstat(filepath.Join(f.repo, n)); !os.IsNotExist(e) {
			f.errorf("write escaped: %s", n)
		}
	}
	if readonlyToolPolicy(f.s.harness, f.s.adapter.Opencode2) {
		f.verifyReadonlyPolicy(r, "denied-shell.txt", "denied-tool.txt")
		return "write tools removed; own-session policy or tools snapshots verified; targets absent; job succeeded"
	}
	shell, file := deniedWrites(f.s.harness, append(f.rawLogs(r.JobID), f.stderrLines(r.JobID)...))
	if !shell || !file {
		f.errorf("actual denied tool evidence: shell=%t file=%t; verbal refusal is insufficient", shell, file)
	}
	return fmt.Sprintf("unchanged repository; actual denied shell=%t file=%t", shell, file)
}
func readonlyLong(f *fixture) string {
	var answer strings.Builder
	for i := 1; i <= 60; i++ {
		word := randomWord()
		f.write(fmt.Sprintf("part-%02d.txt", i), fmt.Sprintf("%02d %s\n", i, word))
		answer.WriteString(word)
	}
	f.git("add", ".")
	f.git("commit", "-qm", "read fixtures")
	before, e := metadata([]string{f.repo}, true)
	must(f.t, e)
	r := f.run("Read part-01.txt through part-60.txt in order. Each has a numbered random string. Use exactly one separate tool invocation per file; do not batch files, use loops, scripts, glob reads or parallel tool calls. Return E2E_TOKENS= followed by all 60 random strings concatenated in numeric order without spaces. Do not change files.", "--read-only")
	if readonlyToolPolicy(f.s.harness, f.s.adapter.Opencode2) {
		f.verifyReadonlyPolicy(r, "denied-shell.txt", "denied-tool.txt")
	}
	if r.Status != sub.StatusSucceeded {
		f.fatalf("60-read job status=%s toolCalls=%d warnings=%s (CLI read-only tool-call cap remains 40)", r.Status, r.ToolCalls, safe([]byte(strings.Join(r.Warnings, "; "))))
	}
	f.success(r)
	if r.ToolCalls < 60 {
		f.errorf("required separate reads not evidenced: %d tool calls", r.ToolCalls)
	}
	if !strings.Contains(r.FinalText, "E2E_TOKENS="+answer.String()) {
		f.error("60-file answer incorrect")
	}
	after, e := metadata([]string{f.repo}, true)
	must(f.t, e)
	if !reflect.DeepEqual(before, after) {
		f.error("read-only repository metadata changed")
	}
	f.clean()
	evidence := ""
	if f.s.harness == "kimi" {
		evidence = "; private session tools snapshots within allowlist; targets absent"
	} else if f.s.harness == "opencode" {
		evidence = "; readonly session policy verified; targets absent"
	}
	return fmt.Sprintf("60 separate reads and answer checked; toolCalls=%d%s", r.ToolCalls, evidence)
}
func resume(f *fixture) string {
	word := randomWord()
	a := f.run("Remember this word: "+word+"\nRun the shell command `printf '%s\\n' '"+word+"' > first.txt`. Reply E2E_FIRST_DONE. Do not commit.", "--worktree")
	f.success(a)
	b := f.run("Without reading any files or running any read commands, recall the random word from our previous turn. Run `printf 'second\\n' > second.txt`. After the tool finishes, your FINAL reply must be exactly E2E_RECALL= followed by the remembered word. Do not commit.", "--resume", a.JobID, "--worktree")
	f.success(b)
	if a.SessionID == "" || a.SessionID != b.SessionID {
		f.error("resume sessionId mismatch")
	}
	if a.WorktreePath == "" || a.WorktreePath != b.WorktreePath {
		f.error("resume worktree mismatch")
	}
	if !strings.Contains(b.FinalText, word) {
		f.error("resumed reply forgot word")
	}
	f.file(b.WorktreePath, "first.txt", word+"\n")
	f.file(b.WorktreePath, "second.txt", "second\n")
	if f.toolContains(b.JobID, "first.txt") {
		f.error("resume used the first-round file instead of memory")
	}
	f.clean()
	return "sessionId, shared worktree, remembered random word and both files verified"
}
func cancelJob(f *fixture) string {
	id := f.start("Run exactly this shell command now: printf E2E_SLEEP_STARTED > cancel.started; sleep 600. Wait for completion before replying. Do not run it in the background.", "--worktree")
	var tracked []procx.Identity
	begun := false
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		st := f.state(id)
		if st.Status != sub.StatusQueued && st.Status != sub.StatusRunning {
			r := f.finish(id, 5*time.Second)
			f.fatalf("sleep did not start; job ended status=%s class=%s warnings=%s", r.Status, r.FailureClass, strings.Join(r.Warnings, "; "))
		}
		if st.Worktree != nil {
			b, e := os.ReadFile(filepath.Join(st.Worktree.Path, "cancel.started"))
			begun = e == nil && string(b) == "E2E_SLEEP_STARTED" && f.toolStarted(id)
		}
		if begun && st.Status == sub.StatusRunning {
			// Allow the tracker to observe the shell's sleep child after the marker.
			time.Sleep(time.Second)
			st = f.state(id)
			tracked = append(append([]procx.Identity{}, st.Processes...), st.Descendants...)
			if st.Worker != nil {
				tracked = append(tracked, *st.Worker)
			}
			if st.ProcessTrackingError != "" {
				f.fatalf("process tracking unavailable: %s", st.ProcessTrackingError)
			}
			if len(st.Descendants) == 0 {
				f.fatal("sleep descendant not recorded")
			}
			sleepSeen := false
			for _, p := range st.Descendants {
				yes, err := isSleep(p)
				if err != nil {
					f.fatalf("sleep process inspection: %v", err)
				}
				sleepSeen = sleepSeen || yes
			}
			if !sleepSeen {
				f.fatal("no live sleep identity among the recorded descendants")
			}
			break
		}
		time.Sleep(time.Second)
	}
	if !begun {
		f.fatal("sleep process did not start within 90s; cannot test cancellation")
	}
	b, e := f.cli("", "cancel", id)
	if e != nil {
		f.fatalf("cancel: %v %s", e, safe(b))
	}
	r := f.finish(id, 30*time.Second)
	if r.Status != sub.StatusCanceled {
		f.errorf("cancel status=%s", r.Status)
	}
	st := f.state(id)
	tracked = append(tracked, st.Processes...)
	tracked = append(tracked, st.Descendants...)
	deadline = time.Now().Add(15 * time.Second)
	for {
		alive := 0
		for _, p := range tracked {
			yes, e := p.Alive()
			if e != nil {
				f.fatal(e)
			}
			if yes {
				alive++
			}
		}
		if alive == 0 {
			break
		}
		if time.Now().After(deadline) {
			f.fatalf("%d recorded processes still alive after cancellation", alive)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Sprintf("OS sleep identity observed; canceled; %d process identities stopped", len(tracked))
}
func longTool(f *fixture) string {
	r := f.finish(f.start("Run exactly `sleep 240 && echo E2E_LONG_DONE` in your shell tool. Wait until it finishes, then include its output E2E_LONG_DONE in your final reply. Do not replace or shorten sleep.", "--worktree"), 7*time.Minute)
	f.success(r)
	if err := longToolEvidence(f.rawLogs(r.JobID), r.Duration); err != nil {
		f.errorf("%v", err)
	}
	if !strings.Contains(r.FinalText, "E2E_LONG_DONE") {
		f.error("missing long tool output")
	}
	if len(r.Retries) > 0 {
		f.error("unexpected transient retry for idle tool")
	}
	for _, w := range r.Warnings {
		lower := strings.ToLower(w)
		if strings.Contains(lower, "timeout") || strings.Contains(lower, "idle") {
			f.errorf("false timeout: %s", safe([]byte(w)))
		}
	}
	if !f.toolContains(r.JobID, "sleep 240") {
		f.error("no actual long command in tool records")
	}
	return fmt.Sprintf("240s tool completed; retries=%d; warnings=%s", len(r.Retries), safe([]byte(strings.Join(r.Warnings, "; "))))
}
func largeOutput(f *fixture) string {
	r := f.run("Run this exact shell command without redirecting or piping away its output: `head -c 5000000 /dev/zero | tr '\\0' 'x'; printf E2E_LARGE_EMITTED > large.done`. It emits five million x characters. Afterwards answer: what is 2+2? Reply E2E_LARGE_DONE=4. Do not repeat the large output in your reply.", "--worktree")
	f.success(r)
	if !strings.Contains(r.FinalText, "E2E_LARGE_DONE=4") {
		f.error("missing final simple answer")
	}
	f.file(r.WorktreePath, "large.done", "E2E_LARGE_EMITTED")
	if !f.toolContains(r.JobID, "5000000") {
		f.error("large-output command not evidenced")
	}
	nativeFlag := nativeTruncationFlag(f.rawLogs(r.JobID))
	if nativeFlag && !strings.Contains(strings.ToLower(strings.Join(r.Warnings, "; ")), "truncat") {
		f.error("native tool truncation was not surfaced in result warnings (truncated covers final text only)")
	}
	return fmt.Sprintf("5MB tool command evidenced; final reply present; finalText truncated=%t; native truncation flag observed=%t; warnings=%s", r.Truncated, nativeFlag, safe([]byte(strings.Join(r.Warnings, "; "))))
}
func parallelJobs(f *fixture) string {
	ids := []string{}
	for i := 0; i < 3; i++ {
		ids = append(ids, f.start(fmt.Sprintf("Write exactly job-%d followed by a newline to same.txt using tools. Do not commit. Reply E2E_PARALLEL_DONE.", i), "--worktree"))
	}
	paths := map[string]bool{}
	for i, id := range ids {
		r := f.finish(id, 5*time.Minute)
		f.success(r)
		if r.WorktreePath == "" || paths[r.WorktreePath] {
			f.error("parallel worktree not unique")
		}
		paths[r.WorktreePath] = true
		f.file(r.WorktreePath, "same.txt", fmt.Sprintf("job-%d\n", i))
	}
	f.clean()
	return "three concurrent jobs; unique worktrees and independent same.txt contents"
}
func modelCheck(f *fixture) string {
	a := f.run("Use a shell tool to run echo E2E_MODEL_OK, then reply E2E_MODEL_OK.", "--worktree")
	if a.Status != sub.StatusSucceeded || strings.TrimSpace(a.FinalText) == "" {
		f.errorf("default-model probe failed: status=%s class=%s", a.Status, a.FailureClass)
	}
	if a.ModelActual == "" || !a.ModelVerified {
		f.error("actual model missing or unverified")
	}
	if f.s.model != "" && (a.ModelRequested != f.s.model || a.ModelActual != f.s.model) {
		f.errorf("model fulfillment mismatch: requested=%s actual=%s", a.ModelRequested, a.ModelActual)
	}
	b := f.run("Reply E2E_INVALID_MODEL.", "--model", "e2e-nonexistent-model-718", "--retries", "0", "--worktree")
	if b.Status != sub.StatusFailed || b.FailureClass != "model" || len(b.Retries) > 0 {
		f.errorf("nonexistent model status=%s class=%s retries=%d", b.Status, b.FailureClass, len(b.Retries))
	}
	return fmt.Sprintf("actual model=%s; modelVerified=%t (adapter semantics only); invalid model class=%s", a.ModelActual, a.ModelVerified, b.FailureClass)
}
func failureSurface(f *fixture) string {
	unauth := f.s.newFixture(f.t, false)
	// Free v2 models need no login, so the probe asks for a provider the
	// unauthenticated fixture has not connected; preflight rejects it as auth.
	model := f.s.model
	if f.s.harness == "opencode" {
		model = "minimax-coding-plan/MiniMax-M3"
	}
	flags := []string{"--worktree", "--retries", "0"}
	if model != "" {
		flags = append(flags, "--model", model)
	}
	f.related = append(f.related, unauth)
	r := unauth.run("Reply E2E_AUTH_PROBE.", flags...)
	// v2 also reaches auth: the private home has no login for this provider.
	if r.Status != sub.StatusFailed || r.FailureClass != "auth" {
		unauth.diagnose()
		f.errorf("failure preflight status=%s class=%s", r.Status, r.FailureClass)
	}
	st := unauth.state(r.JobID)
	explanation := strings.ToLower(st.Error + strings.Join(r.Warnings, " "))
	if !strings.Contains(explanation, "login") && !strings.Contains(explanation, "auth") {
		f.error("failure lacks actionable authentication or model explanation")
	}
	missing := f.s.newFixture(f.t, true)
	f.related = append(f.related, missing)
	missing.env = setenv(missing.env, "PATH", filepath.Join(missing.root, "empty-path"))
	must(f.t, os.MkdirAll(filepath.Join(missing.root, "empty-path"), 0700))
	gitPath, err := exec.LookPath("git")
	must(f.t, err)
	must(f.t, os.Symlink(gitPath, filepath.Join(missing.root, "empty-path", "git")))
	// Keep git available for workspace checks; only the harness executable is absent.
	start := time.Now()
	b, e := missing.cli("Reply E2E_NO_BINARY.", "run", f.s.harness, "--cwd", missing.repo, "--wait", "15s", "--json", "--retries", "0")
	var failed sub.Result
	_ = json.Unmarshal(b, &failed)
	if failed.JobID != "" {
		missing.jobs = append(missing.jobs, failed.JobID)
	}
	text := strings.ToLower(string(b))
	if e == nil || time.Since(start) > 20*time.Second || !(strings.Contains(text, "executable") || strings.Contains(text, "not found") || strings.Contains(text, "no such file")) || !strings.Contains(text, f.s.harness) {
		missing.diagnose()
		f.errorf("missing executable did not fail promptly with clear error: %s", safe(b))
	}
	return "unauthenticated auth failure and missing executable checked; quota exhaustion N/A: covered by golden replay"
}
func records(f *fixture) string {
	r := f.run("Create e2e.txt containing exactly alpha followed by a newline. Replace existing.txt with exactly after followed by a newline. Use tools. Do not commit. Reply E2E_RECORDS_DONE.", "--worktree")
	f.success(r)
	f.file(r.WorktreePath, "e2e.txt", "alpha\n")
	f.file(r.WorktreePath, "existing.txt", "after\n")
	f.clean()
	b, e := f.cli("", "result", r.JobID, "--json")
	must(f.t, e)
	var again sub.Result
	must(f.t, json.Unmarshal(b, &again))
	if !reflect.DeepEqual(r, again) {
		f.error("wait and result JSON differ")
	}
	b, e = f.cli("", "status", r.JobID, "--json")
	must(f.t, e)
	var st sub.JobSummary
	must(f.t, json.Unmarshal(b, &st))
	if st.InputTokens != r.InputTokens || st.OutputTokens != r.OutputTokens || st.ModelActual != r.ModelActual || st.ModelRequested != r.ModelRequested || st.SessionID != r.SessionID || st.UsagePartial != r.UsagePartial || st.Worktree == nil || st.Worktree.Path != r.WorktreePath || st.Worktree.BaseCommit != r.BaseCommit {
		f.error("status/result metadata mismatch")
	}
	statusDuration, se := time.ParseDuration(st.Duration)
	resultDuration, re := time.ParseDuration(r.Duration)
	if se != nil || re != nil || statusDuration-resultDuration > time.Second || resultDuration-statusDuration > time.Second {
		f.errorf("status/result duration inconsistent: %s vs %s", st.Duration, r.Duration)
	}
	b, e = f.cli("", "cost", "--json", "--by", "model")
	must(f.t, e)
	var cost sub.CostReport
	must(f.t, json.Unmarshal(b, &cost))
	if len(cost.Groups) != 1 || cost.Groups[0].Key != r.ModelActual {
		f.error("cost/result model group mismatch")
	}
	if cost.Total.Jobs != 1 || cost.Total.InputTokens != int64(r.InputTokens) || cost.Total.OutputTokens != int64(r.OutputTokens) {
		f.error("cost/result tokens mismatch")
	}
	partial := int64(0)
	if r.UsagePartial {
		partial = 1
	}
	if cost.Total.UsagePartialJobs != partial {
		f.error("cost usage partial mismatch")
	}
	rows := f.rawLogs(r.JobID)
	usageSeen := false
	lastIn, lastOut := -1, -1
	threadTotals := map[string][2]int{}
	for _, row := range rows {
		var event struct {
			Kind string `json:"kind"`
			Raw  struct {
				ThreadID   string `json:"threadId"`
				TokenUsage struct {
					Total *struct {
						Input  int `json:"inputTokens"`
						Output int `json:"outputTokens"`
					} `json:"total"`
				} `json:"tokenUsage"`
			} `json:"raw"`
			Usage *struct {
				Input  int `json:"inputTokens"`
				Output int `json:"outputTokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(row, &event) == nil && event.Kind == "usage" && event.Usage != nil {
			usageSeen = true
			lastIn = event.Usage.Input
			lastOut = event.Usage.Output
			if total := event.Raw.TokenUsage.Total; total != nil {
				threadTotals[event.Raw.ThreadID] = [2]int{total.Input, total.Output}
			}
		}
	}
	// Kimi usage is read from its local wire records after the turn, so the
	// event log has no usage rows; the result must still carry real tokens.
	h, _ := sub.Lookup(f.s.harness)
	if !usageSeen && !r.UsagePartial && (h.LocalUsageBaseline == nil || r.InputTokens == 0) {
		f.error("missing logged usage must be marked partial, not fabricated zero")
	}
	if f.s.harness == "codex" && len(threadTotals) > 0 {
		input, output := 0, 0
		for _, u := range threadTotals {
			input += u[0]
			output += u[1]
		}
		if input != r.InputTokens || output != r.OutputTokens {
			f.error("codex logged thread totals/result mismatch")
		}
	}
	// Codex log events are per request; its result sums thread totals. Other adapters emit job deltas.
	if f.s.harness != "codex" && usageSeen && (lastIn != r.InputTokens || lastOut != r.OutputTokens) {
		f.error("logged usage/result mismatch")
	}
	b, e = f.cli("", "logs", r.JobID)
	must(f.t, e)
	if len(b) == 0 {
		f.error("empty CLI logs")
	}
	return fmt.Sprintf("status/result/logs/cost cross-check; input=%d output=%d usagePartial=%t; cost has no duration/worktree fields", r.InputTokens, r.OutputTokens, r.UsagePartial)
}
func instructions(f *fixture) string {
	rule := "Always end your final reply with the line AGENTS_MD_SEEN.\n"
	f.write("AGENTS.md", rule)
	extra := ""
	if f.s.harness == "agy" {
		f.write("GEMINI.md", rule)
		extra = "; GEMINI.md also supplied for agy"
	}
	f.git("add", ".")
	f.git("commit", "-qm", "instructions")
	r := f.run("Use a shell tool to run echo E2E_INSTRUCTIONS_OK. Give a short final reply.", "--worktree")
	f.success(r)
	if !strings.HasSuffix(strings.TrimSpace(r.FinalText), "AGENTS_MD_SEEN") {
		f.error("repository instruction marker missing")
	}
	return "repository instruction marker observed" + extra
}
func hostPath(f *fixture) string {
	defer func() {
		rows, _ := f.cli("", "status", "--all", "--json")
		var jobs []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(rows, &jobs) == nil {
			for _, j := range jobs {
				f.trackJob(j.ID)
				st := f.state(j.ID)
				if st.Status == sub.StatusRunning || st.Status == sub.StatusQueued {
					_, _ = f.cli("", "cancel", j.ID)
				}
			}
		}
		if len(f.jobs) == 0 {
			f.error("host produced no pactify job; direct host write does not qualify")
		}
	}()
	// Keep host config private while secure storage uses the existing login.
	// Claude opens login files with no-follow: never symlink credentials.
	hostHome := filepath.Join(f.root, "claude")
	must(f.t, os.MkdirAll(hostHome, 0700))
	// Keychain write failure can create a plaintext login in private HOME.
	f.hostFallbacks = append(f.hostFallbacks, filepath.Join(envValue(f.env, "HOME"), ".claude", ".credentials.json"), filepath.Join(hostHome, ".credentials.json"))
	f.env = hostEnvironment(f.env, f.s.user, hostHome, runtime.GOOS)
	if err := hostAuthStatus(func(args ...string) ([]byte, error) {
		return command(30*time.Second, f.env, f.repo, "", "claude", args...)
	}); err != nil {
		f.fatal(err)
	}
	f.env = setenv(f.env, "PATH", filepath.Dir(f.s.bin)+string(os.PathListSeparator)+envValue(f.env, "PATH"))
	// Route only the requested harness to the allowed live-selected model.
	config := map[string]any{"defaults": map[string]any{f.s.harness: map[string]string{"model": f.s.model}}}
	b, _ := json.Marshal(config)
	must(f.t, os.WriteFile(filepath.Join(f.root, "pactify/router.json"), b, 0600))
	want := "中文原文\n```go\nfmt.Println(\"$HOME `quote` \\ & ; | < >\")\n```\nEND_718\n"
	prompt := "/pactify:delegate-" + f.s.harness + "\nIn the repository " + f.repo + ", use the requested harness to write host.txt with the exact bytes between BEGIN_PAYLOAD and END_PAYLOAD (exclude delimiter lines, include the final newline). Do not commit. Apply the job changes, then return a short host summary containing E2E_HOST_SUMMARY. Use the current PATH pactify binary. This is a tiny integration probe; no full Go tests or agent review is needed.\nBEGIN_PAYLOAD\n" + want + "END_PAYLOAD\n"
	out, e := command(10*time.Minute, f.env, f.repo, "", "claude", "-p", "--model", "haiku", "--plugin-dir", filepath.Join(f.s.source, "plugins/pactify"), "--permission-mode", "bypassPermissions", prompt)
	if e != nil {
		f.fatalf("host: %v %s", e, safe(out))
	}
	f.file(f.repo, "host.txt", want)
	if !strings.Contains(string(out), "E2E_HOST_SUMMARY") {
		f.error("host summary missing")
	}
	return "Claude haiku plugin command, callee job, byte-exact payload and host summary checked"
}

func (f *fixture) rawLogs(id string) [][]byte {
	// CLI --raw renders individual events as readable lines, not JSON. Inspect
	// the job-owned log record for assertions; all lifecycle actions use CLI.
	b, e := os.ReadFile(filepath.Join(f.root, "pactify", "jobs", id, "log.jsonl"))
	must(f.t, e)
	rows := [][]byte{}
	for _, line := range strings.Split(string(b), "\n") {
		if json.Valid([]byte(line)) {
			rows = append(rows, []byte(line))
		}
	}
	return rows
}

// stderrLines returns the harness stderr lines kept in the job log, which are
// not JSON events.
func (f *fixture) stderrLines(id string) [][]byte {
	b, e := os.ReadFile(filepath.Join(f.root, "pactify", "jobs", id, "log.jsonl"))
	must(f.t, e)
	rows := [][]byte{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" && !json.Valid([]byte(line)) {
			rows = append(rows, []byte(line))
		}
	}
	return rows
}
func (f *fixture) toolContains(id, text string) bool {
	for _, row := range f.rawLogs(id) {
		var event struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(row, &event) == nil && event.Kind == "tool" && strings.Contains(string(row), text) {
			return true
		}
	}
	return false
}

func (f *fixture) toolStarted(id string) bool {
	return toolRunningEvidence(f.rawLogs(id), f.s.adapter.Opencode2)
}

// This is only an observed upstream flag, not a claim that unflagged tool
// output is complete. Result.truncated retains its final-text-only meaning.
func nativeTruncationFlag(rows [][]byte) bool {
	var flagged func(any) bool
	flagged = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				key = strings.ToLower(strings.ReplaceAll(key, "_", ""))
				if (key == "truncated" || key == "outputtruncated" || key == "istruncated") && child == true {
					return true
				}
				if flagged(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if flagged(child) {
					return true
				}
			}
		}
		return false
	}
	for _, row := range rows {
		var event struct {
			Kind string `json:"kind"`
			Raw  any    `json:"raw"`
		}
		if json.Unmarshal(row, &event) == nil && event.Kind == "tool" && flagged(event.Raw) {
			return true
		}
	}
	return false
}

func (f *fixture) verifyPolicyReadonly(result sub.Result, targets ...string) {
	for _, target := range targets {
		if _, err := os.Lstat(filepath.Join(f.repo, target)); !os.IsNotExist(err) {
			f.errorf("readonly write target exists or cannot be inspected: %s", target)
		}
	}
	store := sub.Store{Root: filepath.Join(envValue(f.env, "PACTIFY_HOME"), "jobs")}
	if err := store.CheckOpencodeReadonly(context.Background(), result.JobID, result, f.env); err != nil {
		f.errorf("readonly session policy verification failed: %v", err)
	}
}

func (f *fixture) verifyReadonlyPolicy(result sub.Result, targets ...string) {
	if f.s.harness != "kimi" {
		f.verifyPolicyReadonly(result, targets...)
		return
	}
	for _, target := range targets {
		if _, err := os.Lstat(filepath.Join(f.repo, target)); !os.IsNotExist(err) {
			f.errorf("readonly write target exists or cannot be inspected: %s", target)
		}
	}
	store := sub.Store{Root: filepath.Join(envValue(f.env, "PACTIFY_HOME"), "jobs")}
	if err := store.CheckKimiReadonly(result.JobID, result, f.env); err != nil {
		f.errorf("readonly tools snapshot verification failed: %v", err)
	}
}
