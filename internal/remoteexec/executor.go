package remoteexec

import "context"

// Transport is the machine's connection to the relay, abstracted so the Executor
// unit-tests without a real socket. A real socket.io(v4) client implements it in
// a later slice — the only external-dependency piece of U3 P-c.
type Transport interface {
	// Receive blocks until the next rpc arrives, returning the raw wire payload
	// and a reply func to courier the Reply back to the caller. A non-nil error
	// (context cancellation, closed connection) ends the Run loop.
	Receive(ctx context.Context) (raw []byte, reply func(Reply) error, err error)
}

// Executor drives one relay connection: receive rpc → parse → dispatch → reply,
// stamping the connection's authenticated account onto each rpc so the
// Dispatcher's scope check and project resolution stay account-bound. It owns no
// socket (the Transport does), so it is deterministic and testable.
type Executor struct {
	// Account is the authenticated account of this connection; every handled rpc
	// is stamped with it (the wire never carries an account — the relay scopes
	// delivery, this re-asserts it into the Dispatcher's check).
	Account    string
	Dispatcher *Dispatcher
}

// Run processes rpcs until the transport returns an error (connection closed or
// ctx cancelled), which Run returns. A malformed payload or a verb error is
// couriered back as Reply{OK:false} and the loop continues — one bad command
// never kills the connection. A reply-send failure is non-fatal (best-effort).
func (e *Executor) Run(ctx context.Context, t Transport) error {
	for {
		raw, reply, err := t.Receive(ctx)
		if err != nil {
			return err
		}
		var resp Reply
		if rpc, perr := ParseRPC(raw); perr != nil {
			resp = fail(perr.Error())
		} else {
			rpc.Account = e.Account
			resp = e.Dispatcher.Handle(rpc)
		}
		if reply != nil {
			_ = reply(resp)
		}
	}
}
