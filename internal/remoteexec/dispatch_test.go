package remoteexec

import (
	"errors"
	"reflect"
	"strings"
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

type fakeTaskAuthor struct {
	gotID, gotSpec string
	err            error
}

func (f *fakeTaskAuthor) AuthorTask(project, id, specMD string) error {
	f.gotID, f.gotSpec = id, specMD
	return f.err
}

func TestHandle_Task(t *testing.T) {
	fa := &fakeTaskAuthor{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Task: fa}
	r := d.Handle(RPC{Type: "pact.task", Account: "acct1", Project: "known", ID: "add-login", SpecMD: "# spec"})
	if !r.OK || fa.gotID != "add-login" || fa.gotSpec != "# spec" {
		t.Fatalf("pact.task should author, got %+v / %+v", r, fa)
	}
	if r := d.Handle(RPC{Type: "pact.task", Account: "acct1", Project: "known"}); r.OK {
		t.Fatal("pact.task without id should fail")
	}
	d2 := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{})}
	if r := d2.Handle(RPC{Type: "pact.task", Account: "acct1", Project: "known", ID: "x", SpecMD: "y"}); r.OK {
		t.Fatal("pact.task disabled when Task nil")
	}
}

type fakeCockpiter struct {
	lastMethod string
	lastArgs   []string
	err        error
}

func (f *fakeCockpiter) Prompt(project, seat, text string) error {
	f.lastMethod, f.lastArgs = "prompt", []string{project, seat, text}
	return f.err
}
func (f *fakeCockpiter) Permission(project, seat, requestID, decision string) error {
	f.lastMethod, f.lastArgs = "permission", []string{project, seat, requestID, decision}
	return f.err
}
func (f *fakeCockpiter) Cancel(project, seat string) error {
	f.lastMethod, f.lastArgs = "cancel", []string{project, seat}
	return f.err
}
func (f *fakeCockpiter) Resume(project, seat string) error {
	f.lastMethod, f.lastArgs = "resume", []string{project, seat}
	return f.err
}
func (f *fakeCockpiter) Subscribe(project, seat string) error {
	f.lastMethod, f.lastArgs = "subscribe", []string{project, seat}
	return f.err
}

func cockpitDispatcher(cp CockpitPolicy) (*Dispatcher, *fakeCockpiter) {
	fc := &fakeCockpiter{}
	return &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Cockpit: fc, CockpitPolicy: cp}, fc
}

func TestHandle_CockpitRoutes(t *testing.T) {
	cp := func(project string) (bool, error) {
		if project == "known" {
			return true, nil
		}
		return false, errors.New("unregistered")
	}

	d, fc := cockpitDispatcher(cp)
	r := d.Handle(RPC{Type: "cockpit.prompt", Account: "acct1", Project: "known", Seat: "kimi", Text: "hi"})
	if !r.OK || fc.lastMethod != "prompt" || fc.lastArgs[2] != "hi" {
		t.Fatalf("prompt: got %+v / method=%q args=%v", r, fc.lastMethod, fc.lastArgs)
	}

	d, fc = cockpitDispatcher(cp)
	r = d.Handle(RPC{Type: "cockpit.permission", Account: "acct1", Project: "known", Seat: "kimi", RequestID: "ap1", Decision: "allow"})
	if !r.OK || fc.lastMethod != "permission" || fc.lastArgs[3] != "allow" {
		t.Fatalf("permission: got %+v / method=%q args=%v", r, fc.lastMethod, fc.lastArgs)
	}

	d, fc = cockpitDispatcher(cp)
	for _, typ := range []string{"cockpit.cancel", "cockpit.resume", "cockpit.subscribe"} {
		r = d.Handle(RPC{Type: typ, Account: "acct1", Project: "known", Seat: "kimi"})
		if !r.OK {
			t.Fatalf("%s: got %+v", typ, r)
		}
		if fc.lastMethod != typ[len("cockpit."):] {
			t.Fatalf("%s: routed to %q", typ, fc.lastMethod)
		}
	}

	// Missing required fields.
	d, _ = cockpitDispatcher(cp)
	if r := d.Handle(RPC{Type: "cockpit.prompt", Account: "acct1", Project: "known", Seat: "kimi"}); r.OK {
		t.Fatal("prompt without text should fail")
	}
	if r := d.Handle(RPC{Type: "cockpit.permission", Account: "acct1", Project: "known", Seat: "kimi"}); r.OK {
		t.Fatal("permission without requestId/decision should fail")
	}
	if r := d.Handle(RPC{Type: "cockpit.cancel", Account: "acct1", Project: "known"}); r.OK {
		t.Fatal("cancel without seat should fail")
	}

	// Unknown cockpit subtype.
	if r := d.Handle(RPC{Type: "cockpit.nuke", Account: "acct1", Project: "known", Seat: "kimi"}); r.OK {
		t.Fatal("unknown cockpit type should fail")
	}
}

func TestHandle_CockpitPolicyGate(t *testing.T) {
	// Gate closed for a known project.
	closed := func(project string) (bool, error) { return project == "open", nil }
	d, _ := cockpitDispatcher(closed)
	r := d.Handle(RPC{Type: "cockpit.prompt", Account: "acct1", Project: "known", Seat: "kimi", Text: "hi"})
	if r.OK || !containsStr(r.Error, "not enabled") {
		t.Fatalf("closed gate: want not-enabled error, got %+v", r)
	}

	// Gate open.
	r = d.Handle(RPC{Type: "cockpit.prompt", Account: "acct1", Project: "open", Seat: "kimi", Text: "hi"})
	if !r.OK {
		t.Fatalf("open gate should accept, got %+v", r)
	}

	// Unknown project (resolver not used for cockpit; policy returns error).
	unknown := func(project string) (bool, error) { return false, errors.New("unknown project") }
	d, _ = cockpitDispatcher(unknown)
	r = d.Handle(RPC{Type: "cockpit.prompt", Account: "acct1", Project: "known", Seat: "kimi", Text: "hi"})
	if r.OK || !containsStr(r.Error, "unknown project") {
		t.Fatalf("unknown project: want error, got %+v", r)
	}

	// Missing project.
	d, _ = cockpitDispatcher(closed)
	if r := d.Handle(RPC{Type: "cockpit.prompt", Account: "acct1", Seat: "kimi", Text: "hi"}); r.OK {
		t.Fatal("missing project should fail")
	}
}

func TestHandle_CockpitAccountScope(t *testing.T) {
	open := func(project string) (bool, error) { return true, nil }
	d, _ := cockpitDispatcher(open)
	r := d.Handle(RPC{Type: "cockpit.prompt", Account: "intruder", Project: "known", Seat: "kimi", Text: "hi"})
	if r.OK || r.Error != ErrAccountScope.Error() {
		t.Fatalf("foreign account: want scope error, got %+v", r)
	}
}

func TestHandle_CockpitDisabled(t *testing.T) {
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{})}
	if r := d.Handle(RPC{Type: "cockpit.prompt", Account: "acct1", Project: "known", Seat: "kimi", Text: "hi"}); r.OK {
		t.Fatal("cockpit should be disabled when Cockpit nil")
	}
}

func containsStr(s, substr string) bool { return strings.Contains(s, substr) }
