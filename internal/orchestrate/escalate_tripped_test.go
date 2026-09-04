package orchestrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// syncNotify is recNotify for the parallel driver, whose features notify from
// their own goroutines.
type syncNotify struct {
	mu   sync.Mutex
	msgs []string
}

func (n *syncNotify) Notify(m string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.msgs = append(n.msgs, m)
}

func (n *syncNotify) all() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return strings.Join(n.msgs, "\n")
}

// readEscalations returns the concatenated bodies of every escalation record in
// dir's orchestrate directory.
func readEscalations(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, ".pact", "orchestrate"))
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "escalation-") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, ".pact", "orchestrate", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
	}
	return sb.String()
}

func trippedOpts(t *testing.T, dir string) Options {
	t.Helper()
	return baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, &recNotify{}).withDefaults()
}

// escalateTripped, env class: writes the scope's proposal and hands the operator
// a copy-pasteable approval naming the TASK (spec §2.2/§2.5).
func TestEscalateTrippedEnvClassWritesScopedProposal(t *testing.T) {
	bindFallbackRoles(t)
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-x", spec)
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}

	opts := trippedOpts(t, dir)
	h := failHistory()
	h.Fails["t1"] = 2
	h.LastFail["t1"] = "worker run: run timeout (--run-timeout) exceeded"
	h.LastClass["t1"] = FailEnv
	act := Action{Kind: ActRunOwner, Feature: "f", Task: "t1", Seat: "w"}

	if err := opts.escalateTripped("f", act, "failure limit exceeded", h, st); err != nil {
		t.Fatalf("escalateTripped: %v", err)
	}

	p, ok := readProposal(dir, "f")
	if !ok {
		t.Fatal("an env-class trip must write the scope's fallback proposal")
	}
	if p.Task != "t1" || p.Seat != "w" || p.FromRole != "primary" || p.ToRole != "backup" {
		t.Fatalf("proposal = %+v, want t1/w primary→backup", p)
	}
	rec := readEscalations(t, dir)
	if !strings.Contains(rec, "--approve-fallback t1") {
		t.Fatalf("the hint must be copy-pasteable and name the task:\n%s", rec)
	}
	if !strings.Contains(rec, "failure history at trip") {
		t.Fatalf("the failure snapshot must be captured before the budget reset:\n%s", rec)
	}
	if _, still := h.Fails["t1"]; still {
		t.Fatal("the tripped task's failure budget must be reset")
	}
	b, err := os.ReadFile(historyPath(dir, "f"))
	if err != nil {
		t.Fatalf("history must be persisted for scope f: %v", err)
	}
	if strings.Contains(string(b), `"t1"`) {
		t.Fatalf("the reset budget must be what is persisted, got %s", b)
	}
}

// escalateTripped, logic class: no proposal, and the hint points at --reset-task.
func TestEscalateTrippedLogicClassHintsResetTask(t *testing.T) {
	bindFallbackRoles(t)
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-x", spec)
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}

	opts := trippedOpts(t, dir)
	h := failHistory()
	h.Fails["t1"] = 2
	h.LastClass["t1"] = FailLogic
	act := Action{Kind: ActRunOwner, Feature: "f", Task: "t1", Seat: "w"}
	if err := opts.escalateTripped("f", act, "failure limit exceeded", h, st); err != nil {
		t.Fatal(err)
	}
	if _, ok := readProposal(dir, "f"); ok {
		t.Fatal("a logic-class trip must not propose swapping the agent")
	}
	rec := readEscalations(t, dir)
	if !strings.Contains(rec, "--reset-task t1") {
		t.Fatalf("logic class must point at --reset-task:\n%s", rec)
	}
}

// The chain is exhausted → plain escalation, no proposal (spec §3).
func TestEscalateTrippedExhaustedChainStopsProposing(t *testing.T) {
	bindFallbackRoles(t)
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-x", spec)
	st, err := pact.At(dir).StateProjection()
	if err != nil {
		t.Fatal(err)
	}
	opts := trippedOpts(t, dir)
	opts.triedFallbacks = map[string]map[string][]string{"f": {"w": {"backup"}}}
	h := failHistory()
	h.LastClass["t1"] = FailEnv
	act := Action{Kind: ActRunOwner, Feature: "f", Task: "t1", Seat: "w"}
	if err := opts.escalateTripped("f", act, "failure limit exceeded", h, st); err != nil {
		t.Fatal(err)
	}
	if _, ok := readProposal(dir, "f"); ok {
		t.Fatal("an exhausted fallback chain must escalate without a proposal")
	}
	if rec := readEscalations(t, dir); !strings.Contains(rec, "failure limit exceeded") {
		t.Fatalf("the plain escalation must still be written:\n%s", rec)
	}
}

// Serial regression guard: the whole serial trip path still writes a proposal
// (now under the run's scope) plus the snapshot and the pause notification —
// byte-identical in every respect the spec did not intend to change.
func TestSerialEnvTripStillProposesUnderTheRunScope(t *testing.T) {
	bindFallbackRoles(t)
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-x", spec)

	notify := &recNotify{}
	opts := baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, notify)
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	p, ok := readProposal(dir, historyScopeAll)
	if !ok {
		t.Fatalf("an unfiltered serial run files its proposal under %q; notify=%v", historyScopeAll, notify.msgs)
	}
	if p.Seat != "w" || p.ToRole != "backup" || p.FromRole != "primary" || p.Task != "t1" {
		t.Fatalf("proposal = %+v", p)
	}
	if !strings.Contains(strings.Join(notify.msgs, "\n"), "orchestrate paused") {
		t.Fatalf("the run must still pause: %v", notify.msgs)
	}
	rec := readEscalations(t, dir)
	for _, want := range []string{"failure history at trip", "--approve-fallback t1"} {
		if !strings.Contains(rec, want) {
			t.Fatalf("serial escalation record missing %q:\n%s", want, rec)
		}
	}
}

// A serial run scoped with --feature files its proposal under that feature.
func TestSerialFilteredRunScopesItsProposalToTheFeature(t *testing.T) {
	bindFallbackRoles(t)
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-x", spec)

	opts := baseOpts(dir, errRunner{context.DeadlineExceeded}, &okExec{}, &recNotify{})
	opts.Feature = "f"
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := readProposal(dir, "f"); !ok {
		t.Fatal("a --feature run must file its proposal under that feature's scope")
	}
	if _, ok := readProposal(dir, historyScopeAll); ok {
		t.Fatal("a --feature run must not file under the unfiltered scope")
	}
}

// A run that names an approval with no pending proposal must not start.
func TestSerialRunRefusesUnknownApproval(t *testing.T) {
	bindFallbackRoles(t)
	dir := newProject(t)
	spec := writeSpec(t, dir, "t1", "true")
	assign(t, dir, "t1", "f", "feat-x", spec)

	run := &noLaunchRunner{}
	opts := baseOpts(dir, run, &okExec{}, &recNotify{})
	opts.ApproveFallback = []string{"no-such-task"}
	if err := Run(context.Background(), opts); err == nil {
		t.Fatal("a run must refuse to start when an approval names no pending proposal")
	}
	if run.launched.Load() {
		t.Fatal("the run must refuse BEFORE launching any agent")
	}
}

// noLaunchRunner records that an agent was launched and fails the stint. It
// deliberately does NOT call t.Fatal: the parallel driver runs it on a feature
// goroutine, where Goexit would strand the coordinator waiting on that feature's
// result instead of failing the test.
type noLaunchRunner struct{ launched atomic.Bool }

func (r *noLaunchRunner) Run(context.Context, LaunchContext) error {
	r.launched.Store(true)
	return errors.New("no agent may launch")
}
