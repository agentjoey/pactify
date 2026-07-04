package remoteexec

import (
	"context"
	"io"
	"testing"
)

func TestParseRPC(t *testing.T) {
	rpc, err := ParseRPC([]byte(`{"type":"pact.assign","machineId":"m1","project":"demo","task":"t1","feature":"f","branch":"feat-f","owner":"kimi","reviewer":"claude","spec":"s.md","deps":["t0"]}`))
	if err != nil {
		t.Fatalf("valid assign: %v", err)
	}
	if rpc.Type != "pact.assign" || rpc.Task != "t1" || rpc.Owner != "kimi" || len(rpc.Deps) != 1 {
		t.Fatalf("assign decoded wrong: %+v", rpc)
	}
	if rpc.Account != "" {
		t.Fatalf("wire must not carry account, got %q", rpc.Account)
	}
	if _, err := ParseRPC([]byte(`{"type":"spawn","machineId":"m1"}`)); err == nil {
		t.Fatalf("non-pact type should be rejected")
	}
	if _, err := ParseRPC([]byte(`not json`)); err == nil {
		t.Fatalf("bad json should error")
	}
}

// scriptedTransport feeds a fixed list of raw payloads, then returns io.EOF.
// It records the replies couriered back for each.
type scriptedTransport struct {
	msgs    [][]byte
	i       int
	replies []Reply
}

func (s *scriptedTransport) Receive(_ context.Context) ([]byte, func(Reply) error, error) {
	if s.i >= len(s.msgs) {
		return nil, nil, io.EOF
	}
	raw := s.msgs[s.i]
	s.i++
	return raw, func(r Reply) error { s.replies = append(s.replies, r); return nil }, nil
}

func TestExecutor_DispatchesAndInjectsAccount(t *testing.T) {
	fe := &fakeEngine{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(fe)}
	ex := &Executor{Account: "acct1", Dispatcher: d}

	tr := &scriptedTransport{msgs: [][]byte{
		[]byte(`{"type":"pact.accept","machineId":"m1","project":"known","task":"t1"}`), // valid → dispatched
		[]byte(`{"type":"spawn","machineId":"m1"}`),                                     // non-pact → fail reply
	}}
	if err := ex.Run(context.Background(), tr); err != io.EOF {
		t.Fatalf("Run should end on transport EOF, got %v", err)
	}
	if len(tr.replies) != 2 {
		t.Fatalf("want 2 replies, got %d", len(tr.replies))
	}
	if !tr.replies[0].OK {
		t.Fatalf("valid accept should reply OK, got %+v", tr.replies[0])
	}
	if fe.called != "accept" || fe.args[0] != "t1" {
		t.Fatalf("accept not dispatched: called=%q args=%v", fe.called, fe.args)
	}
	if tr.replies[1].OK {
		t.Fatalf("non-pact rpc should reply not-OK")
	}
}

func TestExecutor_AccountMismatchRejected(t *testing.T) {
	// Executor account differs from the Dispatcher's configured account: the
	// stamped rpc.Account won't match, so the scope check fails closed.
	fe := &fakeEngine{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(fe)}
	ex := &Executor{Account: "intruder", Dispatcher: d}
	tr := &scriptedTransport{msgs: [][]byte{
		[]byte(`{"type":"pact.accept","machineId":"m1","project":"known","task":"t1"}`),
	}}
	_ = ex.Run(context.Background(), tr)
	if tr.replies[0].OK {
		t.Fatalf("account mismatch should fail closed, got OK")
	}
	if fe.called != "" {
		t.Fatalf("no verb should run on scope failure, ran %q", fe.called)
	}
}
