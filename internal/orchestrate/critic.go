package orchestrate

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/projection"
)

// criticScoreMarker is the line prefix a critic stint ends its output with,
// carrying the pre-review score (spec review-runtime-deepening §3 WS-H). The
// convention: a trailing line `CRITIC_SCORE: <0.0-1.0> — <one sentence why>`.
const criticScoreMarker = "CRITIC_SCORE:"

// criticSeat resolves the seat to run as the pre-review critic: the --critic flag
// override (opts.Critic) when set, else the project's `config critic` setting.
// "" means no critic is configured — the feature is OFF and the driver's flow is
// byte-for-byte unchanged.
func (opts Options) criticSeat() string {
	if opts.Critic != "" {
		return opts.Critic
	}
	seat, ok, err := pact.At(opts.Dir).CriticConfig()
	if err != nil || !ok {
		return ""
	}
	return seat
}

// runCritic runs the configured critic seat read-only AFTER the verify gate is
// green and BEFORE the reviewer (spec §3 WS-H). It records the critic's score as
// a task-level note event (reusing the `start` event_type via pact.Note — no new
// event_type, I-3) and returns the reviewer-briefing injection line.
//
// The score has NO gating power (I-2): a low score never bounces the task; it only
// steers the reviewer's attention. Every failure mode is soft and non-blocking:
//   - No critic configured → returns "" without launching anything (default OFF,
//     zero extra stints, byte-identical flow).
//   - Critic stint failure/timeout → skipped silently, returns "" (never blocks
//     the reviewer, no retry, no escalation).
//   - Critic ran but emitted no parseable score → records a note with a null
//     score and returns "" (the reviewer still runs).
func (opts Options) runCritic(ctx context.Context, st projection.State, act Action) string {
	seat := opts.criticSeat()
	if seat == "" {
		return "" // feature OFF (default): zero behavior change, no stint launched
	}
	_, task, ok := find(st, act.Feature, act.Task)
	if !ok {
		return ""
	}

	if runErr := opts.launchAgent(ctx, seat, opts.kind(seat), criticBrief(task), act.Task); runErr != nil {
		// Critic stint failure/timeout is soft: skip silently. NEVER block the flow —
		// the reviewer runs regardless (do not even record a note; the critic did not
		// produce one).
		return ""
	}

	score, reason := parseCriticScore(readStreamTail(opts.runtimeDir(), act.Task))
	// Record the note on the ledger (best-effort; keeps the score ledger-derivable,
	// I-3). A write failure never blocks — the in-memory injection line below still
	// reaches the reviewer.
	_ = pact.At(opts.Dir).As(opts.Orchestrator).Note(act.Task, criticNotePayload(score, reason, seat))
	opts.mirrorLedger()
	return criticInjection(score, reason)
}

// parseCriticScore extracts the pre-review score from a critic stint's output. It
// scans lines bottom-up for the LAST line carrying criticScoreMarker (only the
// critic is instructed to emit it), parses the leading float after the marker,
// and CLAMPS it into [0.0, 1.0]. The remainder of the line (after the number) is
// the reason. A missing marker or an unparseable number yields (nil, "") — a null
// score, recorded as such, never an error.
func parseCriticScore(output string) (*float64, string) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		idx := strings.Index(line, criticScoreMarker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(criticScoreMarker):])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return nil, ""
		}
		f, err := strconv.ParseFloat(strings.Trim(fields[0], ",;"), 64)
		if err != nil {
			return nil, ""
		}
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		reason := strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
		reason = strings.TrimSpace(strings.TrimLeft(reason, " \t-—–:."))
		return &f, reason
	}
	return nil, ""
}

// criticInjection renders the line injected into the reviewer briefing when a
// critic score is present. A null score (parse failure) injects nothing — the
// reviewer briefing stays unchanged (spec §3: parse failure records a null note
// and proceeds, no injection).
func criticInjection(score *float64, reason string) string {
	if score == nil {
		return ""
	}
	line := fmt.Sprintf("critic pre-review %.1f", *score)
	if reason != "" {
		line += ": " + reason
	}
	return line + " — focus your check here."
}

// criticNotePayload builds the note event payload. A null score marshals to JSON
// null (kept distinct from 0.0 — a legitimate low score).
func criticNotePayload(score *float64, reason, by string) map[string]any {
	p := map[string]any{"critic_by": by, "reason": reason}
	if score == nil {
		p["critic_score"] = nil
	} else {
		p["critic_score"] = *score
	}
	return p
}

// criticBrief renders the read-only briefing handed to the critic stint: assess
// the diff against the spec and end with the CRITIC_SCORE marker line. It forbids
// any write / protocol action — the critic only scores.
func criticBrief(task projection.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pact critic — 预评审打分 task `%s`\n\n", task.ID)
	b.WriteString("你是**只读** critic:不改任何文件、不跑任何 pact 动作(不 accept / 不 changes / 不 checkpoint)。\n\n")
	b.WriteString("## 做什么\n")
	fmt.Fprintf(&b, "- 读规格:`%s`,弄清它要求交付什么。\n", task.Spec)
	b.WriteString("- 看 worker 的改动:`git diff` 看代码、`git log` 看提交,对照 spec 评估切题度与质量。\n\n")
	b.WriteString("## 输出(必须)\n")
	b.WriteString("- 最后一行必须是:`" + criticScoreMarker + " <0.0-1.0> — <一句话理由>`(0=差,1=优)。\n")
	b.WriteString("- 这个分数**没有门控权**——只是提示 reviewer 重点看哪里,不决定去留。\n")
	return b.String()
}

// readStreamTail reads the tail of a task's live agent-output stream (where the
// runner mirrors the critic stint's stdout). It returns "" on any read error —
// a missing stream just means no parseable score (a null note), never a failure.
func readStreamTail(dir, task string) string {
	b, err := os.ReadFile(StreamPath(dir, task))
	if err != nil {
		return ""
	}
	const maxTail = 16 << 10 // last 16 KiB: enough for the trailing CRITIC_SCORE line
	if len(b) > maxTail {
		b = b[len(b)-maxTail:]
	}
	return string(b)
}
