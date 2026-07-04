package serve

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/remoteexec"
	"github.com/agentjoey/pactify/internal/remotemachine"
)

// StartRemoteChannel runs the U3 reverse-control-plane down-channel: connect to
// the relay AS this account's machine and execute pact.* rpc (from the hosted
// dashboard or another machine) on the local engine. It blocks until ctx is
// cancelled, reconnecting with capped exponential backoff and refreshing the
// bearer token between attempts. It is a no-op when the relay uploader isn't
// configured (no cloud session), so a purely-local serve is unaffected.
//
// MVP identity: machineId == accountId (one machine per account) — both serve
// here and the hosted RelaySource derive accountId from their auth login, so no
// machine-id discovery is needed. Multi-machine per account needs a real machine
// id (backlog). Commands execute as the serve's acting seat (s.seat); a verb the
// seat isn't allowed (e.g. accept by a non-reviewer) fails and is couriered back
// as a not-OK reply (currently a no-op — the effect, or its absence, shows on the
// board via the ledger).
func (s *Server) StartRemoteChannel(ctx context.Context) {
	if s.relay == nil {
		return
	}
	base := strings.TrimSuffix(s.relay.endpoint, "/v1/pact/ingest")
	account := s.relay.accountID
	resolve := func(project string) (remoteexec.PactEngine, error) {
		s.pmu.RLock()
		p, ok := s.projects[project]
		s.pmu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("unknown project %q", project)
		}
		return pact.At(p.Path).As(s.seat), nil
	}

	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for ctx.Err() == nil {
		s.relay.mu.Lock()
		token := s.relay.token
		s.relay.mu.Unlock()

		_ = remotemachine.Run(ctx, remotemachine.Config{
			RelayURL:  base,
			Account:   account,
			MachineID: account,
			Token:     token,
			Resolve:   resolve,
		})
		if ctx.Err() != nil {
			return
		}
		// Connection dropped: back off, then refresh the token before retrying
		// (an expired token is the most common cause of a rejected reconnect).
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
		if sess, err := s.relay.client.Authenticate(ctx, s.relay.master); err == nil {
			s.relay.mu.Lock()
			s.relay.token = sess.Token
			s.relay.mu.Unlock()
			backoff = time.Second // fresh creds → reset backoff
		}
	}
}
