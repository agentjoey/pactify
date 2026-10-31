package sub

import (
	"context"
	"fmt"
	"time"
)

// Dismiss acknowledges a finished job without changing its result or worktree.
// A pending worktree stays pending and keeps its own attention.
func (s Store) Dismiss(ctx context.Context, id string) error {
	return s.WithState(ctx, id, func(current *State) error {
		if !Terminal(current.Status) {
			return fmt.Errorf("job %s is still %s; wait for it or cancel it first", id, current.Status)
		}
		if !current.DismissedAt.IsZero() {
			return nil
		}
		current.DismissedAt = time.Now().UTC()
		current.UpdatedAt = current.DismissedAt
		return s.Save(*current)
	})
}
