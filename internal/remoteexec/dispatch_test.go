package remoteexec

import (
	"errors"
	"reflect"
	"testing"
)

// fakeEngine records the last verb call so tests can assert dispatch routing.
type fakeEngine struct {
	called string
	args   []any
	err    error // returned by every verb, to test error propagation
}

func (f *fakeEngine) Assign(t, feat, br, own, rev, spec string, deps []string) error {
	f.called, f.args = "assign", []any{t, feat, br, own, rev, spec, deps}
	return f.err
}
func (f *fakeEngine) Accept(t string) error { f.called, f.args = "accept", []any{t}; return f.err }
func (f *fakeEngine) Changes(t, reason string) error {
	f.called, f.args = "changes", []any{t, reason}
	return f.err
}
func (f *fakeEngine) Merge(feat string) error { f.called, f.args = "merge", []any{feat}; return f.err }
func (f *fakeEngine) Checkpoint(t, ev string) error {
	f.called, f.args = "checkpoint", []any{t, ev}
	return f.err
}

func resolverFor(e PactEngine) Resolver {
	return func(project string) (PactEngine, error) {
		if project == "known" {
			return e, nil
		}
		return nil, errors.New("unregistered")
	}
}

func newDispatcher(e PactEngine) *Dispatcher {
	return &Dispatcher{Account: "acct1", Resolve: resolverFor(e)}
}

func TestHandle_RoutesEachVerb(t *testing.T) {
	cases := []struct {
		rpc      RPC
		want     string
		wantArgs []any
	}{
		{RPC{Type: "pact.assign", Task: "t1", Feature: "f", Branch: "feat-f", Owner: "kimi", Reviewer: "claude", Spec: "s.md", Deps: []string{"t0"}},
			"assign", []any{"t1", "f", "feat-f", "kimi", "claude", "s.md", []string{"t0"}}},
		{RPC{Type: "pact.accept", Task: "t1"}, "accept", []any{"t1"}},
		{RPC{Type: "pact.changes", Task: "t1", Reason: "fix"}, "changes", []any{"t1", "fix"}},
		{RPC{Type: "pact.merge", Feature: "f"}, "merge", []any{"f"}},
		{RPC{Type: "pact.checkpoint", Task: "t1", Evidence: "green"}, "checkpoint", []any{"t1", "green"}},
	}
	for _, c := range cases {
		fe := &fakeEngine{}
		d := newDispatcher(fe)
		c.rpc.Account, c.rpc.Project = "acct1", "known"
		got := d.Handle(c.rpc)
		if !got.OK {
			t.Fatalf("%s: want OK, got error %q", c.rpc.Type, got.Error)
		}
		if fe.called != c.want {
			t.Fatalf("%s: routed to %q, want %q", c.rpc.Type, fe.called, c.want)
		}
		if !reflect.DeepEqual(fe.args, c.wantArgs) {
			t.Fatalf("%s: args = %#v, want %#v", c.rpc.Type, fe.args, c.wantArgs)
		}
	}
}

func TestHandle_AccountScope(t *testing.T) {
	d := newDispatcher(&fakeEngine{})
	r := d.Handle(RPC{Type: "pact.accept", Account: "intruder", Project: "known", Task: "t1"})
	if r.OK || r.Error != ErrAccountScope.Error() {
		t.Fatalf("foreign account: want scope error, got %+v", r)
	}
	// Empty machine account fails closed even for an empty rpc account.
	d2 := &Dispatcher{Account: "", Resolve: resolverFor(&fakeEngine{})}
	if r := d2.Handle(RPC{Type: "pact.accept", Account: "", Project: "known"}); r.OK {
		t.Fatalf("empty account should fail closed, got OK")
	}
}

func TestHandle_UnknownProjectAndType(t *testing.T) {
	d := newDispatcher(&fakeEngine{})
	if r := d.Handle(RPC{Type: "pact.accept", Account: "acct1", Project: "nope", Task: "t1"}); r.OK {
		t.Fatalf("unknown project should fail")
	}
	if r := d.Handle(RPC{Type: "pact.nuke", Account: "acct1", Project: "known"}); r.OK {
		t.Fatalf("unknown rpc type should fail")
	}
	if r := d.Handle(RPC{Type: "pact.accept", Account: "acct1", Project: ""}); r.OK {
		t.Fatalf("empty project should fail")
	}
}

func TestHandle_VerbErrorPropagates(t *testing.T) {
	fe := &fakeEngine{err: errors.New("all tasks must be accepted")}
	d := newDispatcher(fe)
	r := d.Handle(RPC{Type: "pact.merge", Account: "acct1", Project: "known", Feature: "f"})
	if r.OK || r.Error != "all tasks must be accepted" {
		t.Fatalf("verb error should surface, got %+v", r)
	}
}

func TestHandle_NoResolver(t *testing.T) {
	d := &Dispatcher{Account: "acct1"}
	if r := d.Handle(RPC{Type: "pact.accept", Account: "acct1", Project: "known"}); r.OK {
		t.Fatalf("nil resolver should fail closed")
	}
}

type fakeStinter struct {
	got StintRequest
	err error
}

func (f *fakeStinter) RunStint(req StintRequest) error { f.got = req; return f.err }

func TestHandle_Stint(t *testing.T) {
	st := &fakeStinter{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Stint: st}
	r := d.Handle(RPC{Type: "pact.stint", Account: "acct1", Project: "known", Task: "t1", Seat: "kimi-worker", AgentKind: "kimi-cli", Briefing: "do it"})
	if !r.OK {
		t.Fatalf("stint should accept, got %+v", r)
	}
	if st.got.Task != "t1" || st.got.Seat != "kimi-worker" || st.got.AgentKind != "kimi-cli" {
		t.Fatalf("stint req wrong: %+v", st.got)
	}
	// Disabled (nil Stinter) → rejected.
	d2 := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{})}
	if r := d2.Handle(RPC{Type: "pact.stint", Account: "acct1", Project: "known", Task: "t1", Seat: "s", AgentKind: "k"}); r.OK {
		t.Fatalf("stint should be disabled when Stinter nil")
	}
	// Missing seat → rejected.
	if r := d.Handle(RPC{Type: "pact.stint", Account: "acct1", Project: "known", Task: "t1", AgentKind: "k"}); r.OK {
		t.Fatalf("stint missing seat should fail")
	}
	// Policy denied (Stinter returns error) → not-OK.
	st.err = errorsNew("remote stint not allowed for project")
	if r := d.Handle(RPC{Type: "pact.stint", Account: "acct1", Project: "known", Task: "t1", Seat: "s", AgentKind: "k"}); r.OK {
		t.Fatalf("policy-denied stint should fail")
	}
}

func errorsNew(s string) error { return &strErr{s} }

type strErr struct{ s string }

func (e *strErr) Error() string { return e.s }

type fakeOrch struct {
	got OrchestrateRequest
	err error
}

func (f *fakeOrch) RunOrchestrate(req OrchestrateRequest) error { f.got = req; return f.err }

func TestHandle_Orchestrate(t *testing.T) {
	fo := &fakeOrch{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Orch: fo}
	r := d.Handle(RPC{Type: "orchestrate.run", Account: "acct1", Project: "known", Feature: "f1", SeatKinds: map[string]string{"w": "opencode"}})
	if !r.OK {
		t.Fatalf("orchestrate.run should accept, got %+v", r)
	}
	if fo.got.Feature != "f1" || fo.got.Resume || fo.got.SeatKinds["w"] != "opencode" {
		t.Fatalf("orchestrate req wrong: %+v", fo.got)
	}
	if r := d.Handle(RPC{Type: "orchestrate.resume", Account: "acct1", Project: "known"}); !r.OK || !fo.got.Resume {
		t.Fatalf("resume should set Resume, got %+v / %+v", r, fo.got)
	}
	// Disabled → rejected.
	d2 := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{})}
	if r := d2.Handle(RPC{Type: "orchestrate.run", Account: "acct1", Project: "known"}); r.OK {
		t.Fatalf("orchestrate should be disabled when Orch nil")
	}
}

type fakePlanner struct {
	got PlanRequest
	err error
}

func (f *fakePlanner) RunPlan(req PlanRequest) error { f.got = req; return f.err }

func TestHandle_Plan(t *testing.T) {
	fp := &fakePlanner{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Plan: fp}
	r := d.Handle(RPC{Type: "plan.generate", Account: "acct1", Project: "known", Feature: "f1", Goal: "build X", PlannerKind: "claude-code"})
	if !r.OK {
		t.Fatalf("plan.generate should accept, got %+v", r)
	}
	if fp.got.Goal != "build X" || fp.got.Feature != "f1" || fp.got.Apply {
		t.Fatalf("plan req wrong: %+v", fp.got)
	}
	if r := d.Handle(RPC{Type: "plan.apply", Account: "acct1", Project: "known", Feature: "f1"}); !r.OK || !fp.got.Apply {
		t.Fatalf("plan.apply should set Apply, got %+v / %+v", r, fp.got)
	}
	// Missing feature → rejected.
	if r := d.Handle(RPC{Type: "plan.generate", Account: "acct1", Project: "known", Goal: "x"}); r.OK {
		t.Fatal("plan without feature should fail")
	}
	// Disabled → rejected.
	d2 := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{})}
	if r := d2.Handle(RPC{Type: "plan.generate", Account: "acct1", Project: "known", Feature: "f1", Goal: "x"}); r.OK {
		t.Fatal("plan should be disabled when Plan nil")
	}
}

type fakeProv struct {
	gotURL, gotName string
	err             error
}

func (f *fakeProv) Provision(url, name string) (string, error) {
	f.gotURL, f.gotName = url, name
	return "demo", f.err
}

func TestHandle_Provision(t *testing.T) {
	fp := &fakeProv{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Prov: fp}
	// No project required for provision.
	r := d.Handle(RPC{Type: "pact.provision", Account: "acct1", RepoURL: "git@x:demo.git", Name: "demo"})
	if !r.OK || r.RunID != "demo" {
		t.Fatalf("provision should accept + return name, got %+v", r)
	}
	if fp.gotURL != "git@x:demo.git" || fp.gotName != "demo" {
		t.Fatalf("provision args wrong: %s / %s", fp.gotURL, fp.gotName)
	}
	// Missing repoUrl → rejected.
	if r := d.Handle(RPC{Type: "pact.provision", Account: "acct1", Name: "demo"}); r.OK {
		t.Fatal("provision without repoUrl should fail")
	}
	// Disabled → rejected.
	d2 := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{})}
	if r := d2.Handle(RPC{Type: "pact.provision", Account: "acct1", RepoURL: "u", Name: "n"}); r.OK {
		t.Fatal("provision should be disabled when Prov nil")
	}
}
