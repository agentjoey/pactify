package sub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/lockx"
)

// withResumeLock serializes CLI and dashboard check/create across processes.
// The original cwd/harness/session is stable across all turns, including native
// harnesses that report a replacement session id after resume.
func (s Store) withResumeLock(previous State, f func(*SummaryIndex) (State, error)) (State, error) {
	index, err := s.actionIndex(previous)
	if err != nil {
		return State{}, err
	}
	scope, ok := index.scopes[previous.ID]
	if !ok {
		return State{}, actionConflict("resume chain contains a cycle")
	}
	data, _ := json.Marshal(scope)
	key := fmt.Sprintf("%x", sha256.Sum256(data))
	release, err := lockx.Acquire(context.Background(), filepath.Join(s.Root+".resume-locks", key+".lock"))
	if err != nil {
		return State{}, err
	}
	defer release()
	// The snapshot before acquisition is only for choosing the shared lock key.
	// Check/create must use a fresh view while that lock is held.
	index, err = s.actionIndex(previous)
	if err != nil {
		return State{}, err
	}
	if err := index.checkResumeIdle(previous); err != nil {
		return State{}, err
	}
	return f(index)
}
