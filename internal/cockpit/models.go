package cockpit

import "errors"

// ErrEffortUnavailable identifies a rejected model-specific effort selection.
var ErrEffortUnavailable = errors.New("requested effort is not offered for this model")

// ModelDiscovery contains only public selection metadata. Harness configuration
// and provider/authentication fields must never be retained here.
type ModelDiscovery struct {
	Warning string
	Items   []string
	Efforts []string

	// ModelEfforts retains model-specific choices for backend validation.
	// Efforts is their union for harness-level selection interfaces.
	ModelEfforts map[string][]string
	DefaultModel string
}
