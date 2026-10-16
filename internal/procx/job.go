package procx

import "slices"

// FindJobProcesses finds current-user processes with an exact PACTIFY_JOB marker,
// even after reparenting or leaving the harness's session. Unreadable process
// environments are skipped; failure to enumerate processes is returned.
func FindJobProcesses(jobID string) ([]Identity, error) {
	if jobID == "" {
		return nil, nil
	}
	entries, err := userProcesses()
	if err != nil {
		return nil, err
	}
	return jobProcessesFromSnapshot(jobID, entries, processEnvironment, Inspect), nil
}

func jobProcessesFromSnapshot(jobID string, entries []Identity, environment func(int) ([]string, error), inspect func(int) (Identity, error)) []Identity {
	if jobID == "" {
		return nil
	}
	var matches []Identity
	for _, p := range entries {
		if p.PID <= 1 || p.Start == "" {
			continue
		}
		env, err := environment(p.PID)
		if err != nil || !slices.Contains(env, "PACTIFY_JOB="+jobID) {
			continue
		}
		// Bind the environment read to the identity from enumeration. A reused
		// PID must not adopt the old process's marker. Signal checks it again.
		if current, err := inspect(p.PID); err == nil && current == p {
			matches = append(matches, p)
		}
	}
	return matches
}
