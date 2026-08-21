package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/projection"
)

// qaResultMarker is the line prefix a QA stint ends its output with, carrying the
// experimental QA verdict (spec review-runtime-deepening §4 WS-I). Convention: a
// trailing line `QA_RESULT: PASS|FAIL: <one sentence>`.
const qaResultMarker = "QA_RESULT:"

// qaOutcome is the parsed verdict of a QA stint.
type qaOutcome int

const (
	// qaMissing is the lenient default: no marker line, or a marker with neither
	// PASS nor FAIL. An experimental feature must never hard-block, so a missing
	// verdict records a note and proceeds (spec §4).
	qaMissing qaOutcome = iota
	qaPass
	qaFail
)

// runQA is the experimental, task-level opt-in QA-agent gate (spec §4 WS-I). It is
// called for an awaiting_review task AFTER the verify gate is green (post
// fixUntilGreen) and BEFORE the critic/reviewer — the spec order is "先 QA 后
// critic". It returns:
//
//   - qaNote: the reviewer-briefing injection line pointing at the QA report
//     (empty when the task has no `qa:` line, so the reviewer brief stays
//     byte-for-byte unchanged).
//   - proceed: false ONLY when a QA FAIL exhausts the shared fix-round budget and
//     the run escalates (paused, not failed); true in every other case.
//
// Sequencing & the FAIL fix loop:
//   - No `qa:` line → returns ("", true, nil) immediately, launching NO stint, so
//     the default flow is byte-identical / zero extra stints.
//   - QA stint failure/timeout → soft & lenient: record a note, proceed (never
//     block — experimental).
//   - PASS → record a note, inject the report path, proceed.
//   - Missing/garbled marker → lenient: record a null note, inject the report
//     path, proceed.
//   - FAIL → treated like a RED verify gate: it feeds the SAME WS-F self-repair
//     mechanism, re-running the owner with a fix briefing and SHARING the fixRounds
//     budget + opts.MaxFixRounds bound with fixUntilGreen (a round burned by a
//     verify-red fix leaves fewer QA-fix rounds, and vice-versa). After each owner
//     fix it re-runs the QA stint. Rounds exhausted → escalate (proceed=false),
//     exactly like a permanently-red verify gate.
func (opts Options) runQA(ctx context.Context, st, view projection.State, act Action, h *History, fixRounds map[string]int) (qaNote string, proceed bool, err error) {
	_, task, ok := find(st, act.Feature, act.Task)
	if !ok {
		return "", true, nil
	}
	hint, ok := extractQA(readSpec(opts.Dir, task.Spec))
	if !ok {
		return "", true, nil // no `qa:` line → current behavior, zero stints
	}
	report := qaReportRel(act.Task)

	for {
		// The QA stint reuses the owner's runner/kind (spec §4: keep it simple).
		if runErr := opts.launchAgent(ctx, task.Owner, opts.kind(task.Owner), qaBrief(opts.Dir, task, hint, report), act.Task, opts.launchEffort(task)); runErr != nil {
			if ctx.Err() != nil {
				return "", false, runErr // cancellation: propagate, don't swallow
			}
			// A QA stint failure/timeout is soft and LENIENT (experimental — never
			// hard-block): record a null-result note and proceed, still pointing the
			// reviewer at the (possibly empty) report. An orchestrator-seat owner
			// (deterministically unlaunchable) stays lenient too, but the note
			// carries the real cause so the reviewer isn't left guessing.
			cause := ""
			if errors.Is(runErr, errOrchestratorSeat) {
				cause = runErr.Error()
			}
			opts.recordQANote(act.Task, "error", cause, task.Owner)
			return qaInjection(report, ""), true, nil
		}

		result, sentence := parseQAResult(readStreamTail(opts.runtimeDir(), act.Task))
		switch result {
		case qaPass:
			opts.recordQANote(act.Task, "pass", sentence, task.Owner)
			return qaInjection(report, sentence), true, nil
		case qaMissing:
			// Lenient: an experimental feature must never hard-block on a garbled or
			// absent verdict. Record a null note and proceed (spec §4).
			opts.recordQANote(act.Task, "missing", "", task.Owner)
			return qaInjection(report, ""), true, nil
		case qaFail:
			// Equivalent to a RED verify gate → feed the WS-F fix loop, SHARING the
			// round budget with fixUntilGreen (same map, same MaxFixRounds bound).
			if fixRounds[act.Task] >= opts.MaxFixRounds {
				reason := fmt.Sprintf("QA gate FAIL — fix rounds exhausted after %d round(s): %s",
					fixRounds[act.Task], sentence)
				opts.emitEscalatedStatus(view, act.Task, reason, *h)
				return "", false, opts.escalate(act.Feature, act.Task, reason, evidenceFor(st, act.Task),
					"人工修复 QA 失败后 pactify orchestrate 续跑")
			}
			// Count the shared round FIRST (driver in-memory only — not a failure, not a
			// ledger event) so a persistently-failing fixer still terminates.
			fixRounds[act.Task]++
			opts.emitFixingStatus(view, act, task.Owner, *h, fixRounds[act.Task])
			if runErr := opts.launchAgent(ctx, task.Owner, opts.kind(task.Owner), qaFixBrief(task, sentence, report), act.Task, opts.launchEffort(task)); runErr != nil {
				if ctx.Err() != nil {
					return "", false, runErr
				}
				// A fix-round agent error is not a task failure (in-stint self-repair):
				// leave h.Fails untouched; the round is counted, so this cannot spin.
			}
			opts.mirrorLedger()
			// loop: re-run the QA stint against the fixed tree.
		}
	}
}

// recordQANote records the QA verdict as a task-level note event (reusing the
// `start` event_type via pact.Note — no new event_type, I-3), keeping the verdict
// ledger-derivable. Best-effort: a write failure never blocks the flow (the
// in-memory injection line still reaches the reviewer). Distinguished from a critic
// note by the `qa_by` payload key.
func (opts Options) recordQANote(task, result, sentence, by string) {
	_ = pact.At(opts.Dir).As(opts.Orchestrator).Note(task, map[string]any{
		"qa_by": by, "qa_result": result, "qa_note": sentence,
	})
	opts.mirrorLedger()
}

// qaReportRel is the repo-relative path a QA stint writes its report to, one file
// per task under the driver's runtime dir. It lives beside status.json/streams so
// it is already covered by the .pact/orchestrate/ local gitignore.
func qaReportRel(task string) string {
	return filepath.Join(".pact", "orchestrate", "qa-"+task+".md")
}

// parseQAResult extracts the experimental verdict from a QA stint's output. It
// scans bottom-up for the LAST line carrying qaResultMarker (only the QA stint is
// instructed to emit it) and classifies the leading PASS/FAIL token. A missing
// marker, or a marker with no recognizable verdict, yields (qaMissing, "") — the
// lenient default, never an error. The remainder after the verdict token is the
// one-sentence reason.
func parseQAResult(output string) (qaOutcome, string) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		idx := strings.Index(line, qaResultMarker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(qaResultMarker):])
		upper := strings.ToUpper(rest)
		switch {
		case strings.HasPrefix(upper, "PASS"):
			return qaPass, trimVerdict(rest, len("PASS"))
		case strings.HasPrefix(upper, "FAIL"):
			return qaFail, trimVerdict(rest, len("FAIL"))
		default:
			return qaMissing, "" // marker present but no PASS/FAIL → lenient
		}
	}
	return qaMissing, ""
}

// trimVerdict strips the verdict token and any leading separator punctuation from
// a QA_RESULT remainder, leaving the one-sentence reason.
func trimVerdict(rest string, n int) string {
	s := strings.TrimSpace(rest[n:])
	return strings.TrimSpace(strings.TrimLeft(s, ": \t-—–."))
}

// qaInjection renders the line injected into the reviewer briefing pointing at the
// QA report (spec §4: "QA 报告路径写进 reviewer briefing"). The sentence, when
// present, is the QA verdict's one-liner.
func qaInjection(report, sentence string) string {
	line := "QA 已把软件真跑起来验证,报告见 `" + report + "`"
	if sentence != "" {
		line += ":" + sentence
	}
	return line + "。评审时请结合 QA 报告一起看。"
}

// joinNotes joins the non-empty pre-review injections (QA report + critic score)
// with a blank line, so both land in the reviewer briefing's single pre-review
// section. All-empty → "" (the reviewer brief stays byte-for-byte unchanged).
func joinNotes(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

// qaBrief renders the briefing handed to the QA stint: run the software (not read
// the code) to verify the `qa:` hint, write a report to the per-task path, and end
// with the QA_RESULT marker line. The knowledge layer (`.pact/skills/qa-guide.md`
// when tagged for the `qa` role) is appended via the same helper the worker/
// reviewer briefs use (spec §4: 包1 知识层管道复用).
func qaBrief(dir string, task projection.Task, hint, report string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pact QA — task `%s`\n\n", task.ID)
	b.WriteString("你是 QA:把软件**真的跑起来**验证行为,不是读代码、不跑 pact 动作。\n\n")
	b.WriteString("## 验证什么\n")
	fmt.Fprintf(&b, "- 读规格:`%s`,弄清它交付什么。\n", task.Spec)
	fmt.Fprintf(&b, "- 真跑软件,验证:%s\n\n", hint)
	b.WriteString("## 输出(必须)\n")
	fmt.Fprintf(&b, "- 把验证过程与结论写到 `%s`。\n", report)
	b.WriteString("- 最后一行必须是:`" + qaResultMarker + " PASS|FAIL: <一句话>`(PASS=跑通,FAIL=没跑通)。\n")

	// git-native knowledge injection (spec §3/§4): a qa-guide skill tagged for the
	// `qa` role flows in here; nothing tagged → KnowledgeFor returns "" and the
	// briefing is unchanged.
	if kn := KnowledgeFor(dir, "qa", task.Spec+" "+task.ID+" "+hint); kn != "" {
		b.WriteString("\n" + kn)
	}
	return b.String()
}

// qaFixBrief renders the fix-round briefing when a QA stint returns FAIL. It is a
// WS-F fix round (shared budget) honest about being QA-driven: the marker
// `pact fix round` keeps it in the same self-repair class as a verify-red fix.
func qaFixBrief(task projection.Task, sentence, report string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pact fix round (QA) — task `%s`\n\n", task.ID)
	b.WriteString("驱动器在交给 reviewer 前把软件真跑起来做了 QA——**QA 判定 FAIL**。\n")
	b.WriteString("这不是返工、也不算失败,是评审前的自愈轮:请修到 QA 能跑通。\n\n")
	b.WriteString("## 修什么\n")
	fmt.Fprintf(&b, "- 读规格:`%s`。只碰该 spec 列出的文件。\n", task.Spec)
	fmt.Fprintf(&b, "- QA 失败原因:%s\n", sentence)
	fmt.Fprintf(&b, "- QA 报告见 `%s`。\n", report)
	fmt.Fprintf(&b, "- 修好后重新 `pactify checkpoint %s`。修到 QA 绿为止,不要自标 accepted。\n", task.ID)
	return b.String()
}
