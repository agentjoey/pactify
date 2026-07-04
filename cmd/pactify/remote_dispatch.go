package main

import (
	"context"
	"fmt"

	"github.com/agentjoey/pactify/internal/cloudauth"
	"github.com/agentjoey/pactify/internal/cloudclient"
	"github.com/agentjoey/pactify/internal/orchestrate"
	"github.com/agentjoey/pactify/internal/relaysock"
)

// newRelayStintDispatch builds the production StintDispatch for --seat-host: it
// authenticates with the account master secret, opens one relay socket as a
// client, and emits machine-targeted pact.stint rpcs on it. The returned closer
// tears the socket down when the run ends.
func newRelayStintDispatch(ctx context.Context) (orchestrate.StintDispatch, func(), error) {
	relayURL := cloudclient.RelayURLFromEnv()
	if relayURL == "" {
		return nil, nil, fmt.Errorf("no relay URL (set $PACT_RELAY_URL)")
	}
	master, err := cloudauth.LoadMasterSecret()
	if err != nil {
		return nil, nil, fmt.Errorf("load master secret: %w", err)
	}
	sess, err := cloudclient.New(relayURL).Authenticate(ctx, master)
	if err != nil {
		return nil, nil, fmt.Errorf("relay auth: %w", err)
	}
	client, err := relaysock.Dial(ctx, relayURL, map[string]string{"token": sess.Token, "role": "client"})
	if err != nil {
		return nil, nil, fmt.Errorf("relay dial: %w", err)
	}
	go func() { _ = client.Run(ctx) }()

	dispatch := func(machineID string, s orchestrate.StintRPC) error {
		return client.Emit("rpc", map[string]any{
			"type":      "pact.stint",
			"machineId": machineID,
			"project":   s.Project,
			"task":      s.Task,
			"seat":      s.Seat,
			"agentKind": s.AgentKind,
			"briefing":  s.Briefing,
			"branch":    s.Branch,
		})
	}
	return dispatch, func() { _ = client.Close() }, nil
}
