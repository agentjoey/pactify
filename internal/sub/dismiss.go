package sub

import (
	"context"
	"fmt"
	"time"
)

// Dismiss acknowledges a finished job without changing its result or worktree.
func (s Store) Dismiss(ctx context.Context, id string) error {
	return s.WithState(ctx, id, func(current *State) error {
		if !Terminal(current.Status) {
			return fmt.Errorf("job %s is still %s; wait for it or cancel it first", id, current.Status)
		}
		if w := current.Worktree; w != nil && !w.Applied && !w.Discarded {
			return fmt.Errorf("job %s has a pending worktree; apply or discard it first", id)
		}
		if !current.DismissedAt.IsZero() {
			return nil
		}
		current.DismissedAt = time.Now().UTC()
		current.UpdatedAt = current.DismissedAt
		return s.Save(*current)
	})
}
