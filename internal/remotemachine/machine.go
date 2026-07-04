// Package remotemachine wires the relay socket (relaysock) to the pact command
// executor (remoteexec): pactify serve connects to the relay AS a machine,
// receives pact.* rpc pushed from a remote control plane, and runs them on the
// local engine. This is the top of the U3 reverse-control-plane down-channel.
package remotemachine

import (
	"context"
	"encoding/json"

	"github.com/agentjoey/pactify/internal/relaysock"
	"github.com/agentjoey/pactify/internal/remoteexec"
)

// socketTransport bridges relaysock.Client's push-style On("rpc") into the
// pull-style remoteexec.Transport the Executor loop consumes. A bounded buffer
// drops rpcs only under extreme backpressure (never blocks the socket read loop).
type socketTransport struct {
	ch chan []byte
}

func (t *socketTransport) Receive(ctx context.Context) ([]byte, func(remoteexec.Reply) error, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case raw := <-t.ch:
		// Reply is a no-op: a pact rpc's effect returns through the event stream
		// (the executed verb's ledger event, uploaded by serve's relay uploader),
		// not a direct socket ack — see the U3 design. Kept for the interface.
		return raw, func(remoteexec.Reply) error { return nil }, nil
	}
}

// Config is what serve supplies to run the machine down-channel.
type Config struct {
	RelayURL  string
	Account   string
	MachineID string
	Token     string // account bearer (from the auth flow)
	// Resolve maps a project name → its pact engine; unknown projects are rejected.
	Resolve remoteexec.Resolver
}

// Run connects to the relay as a machine and processes pact rpc until the
// connection errors or ctx is cancelled, returning the terminating error so the
// caller can back off and reconnect. It never executes a verb for an unresolved
// project or a mismatched account (remoteexec enforces both).
func Run(ctx context.Context, cfg Config) error {
	client, err := relaysock.Dial(ctx, cfg.RelayURL, map[string]string{
		"token":     cfg.Token,
		"role":      "machine",
		"machineId": cfg.MachineID,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	t := &socketTransport{ch: make(chan []byte, 32)}
	client.On("rpc", func(args []json.RawMessage) {
		if len(args) != 1 {
			return
		}
		select {
		case t.ch <- []byte(args[0]):
		default: // buffer full — drop rather than block the read loop
		}
	})

	ex := &remoteexec.Executor{
		Account:    cfg.Account,
		Dispatcher: &remoteexec.Dispatcher{Account: cfg.Account, Resolve: cfg.Resolve},
	}
	go func() { _ = ex.Run(ctx, t) }()

	return client.Run(ctx)
}
