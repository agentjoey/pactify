package sub

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/lockx"
)

const DashboardJobLimit = 4

var ErrDashboardJobLimit = errors.New("too many jobs started from the dashboard are still running (limit 4)")

// ResumeDashboardJob reserves capacity durably by creating a marked queued job
// under the store-wide lock. Other serve processes see the same reservation.
func (s Store) ResumeDashboardJob(ctx context.Context, selector, brief string) (State, error) {
	release, err := lockx.Acquire(ctx, filepath.Join(s.Root+".dashboard.lock"))
	if err != nil {
		return State{}, err
	}
	defer release()
	states, err := s.States()
	if err != nil {
		return State{}, err
	}
	active := 0
	for _, st := range states {
		if st.Request.StartedFrom != "dashboard" || Terminal(st.Status) {
			continue
		}
		st, err = s.Recover(st.ID)
		if err != nil {
			return State{}, err
		}
		if !Terminal(st.Status) {
			active++
		}
	}
	if active >= DashboardJobLimit {
		return State{}, ErrDashboardJobLimit
	}
	return s.resumeJob(selector, brief, true)
}
