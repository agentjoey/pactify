// Package remotemachine wires the relay socket (relaysock) to the pact command
// executor (remoteexec): pactify serve connects to the relay AS a machine,
// receives pact.* rpc pushed from a remote control plane, and runs them on the
// local engine. This is the top of the U3 reverse-control-plane down-channel.
package remotemachine

import (
	"context"
	"encoding/json"
	"time"

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

// Info is the machine's presence metadata sent on register + heartbeat.
type Info struct {
	Host       string   `json:"host,omitempty"`
	AgentKinds []string `json:"agentKinds"`
	Workdirs   []string `json:"workdirs,omitempty"`
}

// heartbeatInterval re-emits register so the relay refreshes lastSeenAt/online
// (well under the relay's board-hygiene TTL).
const heartbeatInterval = 25 * time.Second

// Config is what serve supplies to run the machine channel.
type Config struct {
	RelayURL  string
	Account   string
	MachineID string
	Token     string // account bearer (from the auth flow)
	Info      Info   // presence: host + agent kinds + workdirs
	// Resolve maps a project name → its pact engine. When nil, the machine is
	// PRESENCE-ONLY (registers + heartbeats, visible on the board) and ignores
	// rpc — the safe default. A non-nil resolver enables remote command execution
	// (opt-in via serve --remote-control).
	Resolve remoteexec.Resolver
	// Stint runs remote agent stints (pact.stint), policy-gated. Nil = disabled.
	Stint remoteexec.Stinter
}

// Run connects to the relay as a machine, registers its presence (host + agent
// kinds) and heartbeats it, and — when a resolver is configured — executes pact
// rpc locally. It blocks until the connection errors or ctx is cancelled,
// returning the terminating error so the caller can back off and reconnect.
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

	// Register presence + heartbeat (the relay upserts the Machine row on each).
	register := func() { _ = client.Emit("register", cfg.Info) }
	register()
	go func() {
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				register()
			}
		}
	}()

	// Remote command execution — only when a resolver or stinter is configured.
	if cfg.Resolve != nil || cfg.Stint != nil {
		tr := &socketTransport{ch: make(chan []byte, 32)}
		client.On("rpc", func(args []json.RawMessage) {
			if len(args) != 1 {
				return
			}
			select {
			case tr.ch <- []byte(args[0]):
			default: // buffer full — drop rather than block the read loop
			}
		})
		ex := &remoteexec.Executor{
			Account:    cfg.Account,
			Dispatcher: &remoteexec.Dispatcher{Account: cfg.Account, Resolve: cfg.Resolve, Stint: cfg.Stint},
		}
		go func() { _ = ex.Run(ctx, tr) }()
	}

	return client.Run(ctx)
}
