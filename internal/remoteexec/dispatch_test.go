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
