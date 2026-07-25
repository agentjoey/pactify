package orchestrate

// FailClass splits a failed stint into the two cases that deserve different
// responses: the agent COULD NOT WORK (env — quota, auth, a missing binary, a
// hang before producing anything), versus the agent WORKED BUT THE OUTPUT IS
// WRONG (logic — a red verify, repeated review rejections). Swapping to another
// agent can rescue the first and is pointless for the second.
type FailClass string

const (
	// FailEnv: the agent produced nothing. Another (agent, model) profile may
	// well succeed, so this is the class that proposes a fallback.
	FailEnv FailClass = "env"
	// FailLogic: the agent did work and the work is wrong. A different agent is
	// unlikely to help; this escalates to a human.
	FailLogic FailClass = "logic"
)

func (c FailClass) String() string { return string(c) }

// classifyFailure decides a failed stint's class from two signals only: that it
// failed, and whether it DELIVERED anything (the launch-window tree fingerprint
// the driver already computes). Deliberately no stderr pattern matching —
// quota exhaustion is not detectable across vendors (gemini's free tier
// silently downgrades to flash on 429 and never errors at all) — and
// deliberately no elapsed-time threshold: a stint that ran ten minutes and
// produced nothing is just as unable-to-work as one that died instantly, so
// the extra knob would add a magic number without changing any verdict.
func classifyFailure(runErr error, delivered bool) FailClass {
	if runErr == nil {
		return FailLogic
	}
	if !delivered {
		return FailEnv
	}
	return FailLogic
}
