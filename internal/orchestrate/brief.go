// Package orchestrate drives headless agents through the pact protocol: it
// launches a seat as a worker or reviewer and feeds it a role-specific briefing.
package orchestrate

import (
	"fmt"
	"strings"

	"github.com/agentjoey/pactify/internal/projection"
)

// workerBrief renders the briefing handed to a headless agent launched to do
// work on task. The seat's id and roles, the cold-start `pactify join`, the
// task id + spec path, the `pactify checkpoint` handoff and the no-self-accept
// rule are always present. When changesReason is non-empty (a rework launch
// after the reviewer requested changes) the verbatim reason is carried in so
// the worker addresses it.
func workerBrief(dir string, seat projection.Seat, task projection.Task, changesReason string, retrying bool) string {
	roles := strings.Join(seat.Roles, ",")

	var b strings.Builder
	fmt.Fprintf(&b, "# pact worker — seat `%s` (roles: %s)\n\n", seat.ID, roles)
	fmt.Fprintf(&b, "You are seat `%s` working task `%s` in this repo (pact protocol v1).\n\n", seat.ID, task.ID)

	b.WriteString("## 冷启动\n")
	fmt.Fprintf(&b, "1. `pactify join %s --roles %s` — registers your seat and checks out the feature branch.\n", seat.ID, roles)
	b.WriteString("2. `pactify status` — confirm `" + task.ID + "` is yours.\n\n")

	b.WriteString("## 干活\n")
	fmt.Fprintf(&b, "- 读规格：`%s`。只碰该 spec 列出的文件。\n", task.Spec)
	b.WriteString("- TDD：先写失败测试，再实现，跑该 task 规格里的验收命令直到通过。\n")
	fmt.Fprintf(&b, "- 完成后 `pactify checkpoint %s` 并附上 evidence（验收命令输出）交回给 reviewer。\n\n", task.ID)

	if retrying {
		b.WriteString("## 这是重试棒（上一轮没干完/被超时杀掉）\n")
		b.WriteString("上一轮这个 task 的 agent 运行没能把它推进到 awaiting_review（崩溃、超时、或卡死被杀）。\n")
		b.WriteString("- 先 `git status` / `git diff` 看清楚工作树里**已有的半成品**——上一轮可能已经写了一部分。\n")
		b.WriteString("- 能续就续（在半成品基础上补完），续不动或半成品是残状态就 `git checkout -- <file>` 重来该文件，别被半残状态带偏。\n")
		b.WriteString("- 目标不变：跑通验收命令后 `pactify checkpoint` 交回。你始终是干活的人，不要指望别人接手。\n\n")
	}

	if changesReason != "" {
		b.WriteString("## 上次评审的返工原因\n")
		b.WriteString("reviewer 打回了这个 task，原因如下，必须逐条解决：\n\n")
		// Keep a multi-line reason inside the blockquote: a raw newline would drop
		// later lines out of the quote and bleed into the next section.
		b.WriteString("> " + strings.ReplaceAll(changesReason, "\n", "\n> ") + "\n\n")
	}

	b.WriteString("## 边界\n")
	b.WriteString("- 不要自标 `accepted`，不能自接受。worker 只把 task 置为 awaiting_review；只有该 task 的 reviewer 能 accept。\n")
	b.WriteString("- 不碰别的 task，不碰 spec 以外的文件。\n")

	// git-native knowledge injection (spec §3): memory + matching skills. When
	// nothing applies KnowledgeFor returns "" and the briefing is unchanged.
	if kn := KnowledgeFor(dir, "worker", task.Spec+" "+task.ID); kn != "" {
		b.WriteString("\n" + kn)
	}

	return b.String()
}

// reviewerBrief renders the briefing handed to a headless agent launched to
// review an awaiting_review task. It names the seat, the task id + spec path,
// points at the worker's changes via `git diff` / `git log`, instructs running
// the spec's acceptance commands, and gives the accept / changes verbs while
// forbidding the reviewer from editing the implementation.
//
// criticNote, when non-empty, is the pre-review critic's score+reason line (spec
// §3 WS-H); it is injected as a leading section to steer the reviewer's attention.
// An empty criticNote (no critic configured, or a critic that produced no
// parseable score) leaves the briefing byte-for-byte identical to the pre-WS-H one.
func reviewerBrief(dir string, seat projection.Seat, task projection.Task, criticNote string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pact reviewer — seat `%s`\n\n", seat.ID)
	fmt.Fprintf(&b, "You are seat `%s`, reviewing task `%s` (status awaiting_review) in this repo (pact protocol v1).\n\n", seat.ID, task.ID)

	if criticNote != "" {
		b.WriteString("## critic 预评\n")
		b.WriteString(criticNote + "\n\n")
	}

	b.WriteString("## 审什么\n")
	fmt.Fprintf(&b, "- 读规格：`%s`，确认验收标准。\n", task.Spec)
	b.WriteString("- 看 worker 的改动：`git diff` 看代码、`git log` 看提交，比对是否切题、是否越界改了 spec 外的文件。\n")
	b.WriteString("- 跑该 task 规格里的验收命令，亲自验证它真的过——不要只看 worker 自报的 evidence。\n\n")

	b.WriteString("## 裁决\n")
	fmt.Fprintf(&b, "- 通过：`pactify accept %s`。\n", task.ID)
	fmt.Fprintf(&b, "- 打回：`pactify changes %s --reason \"<具体返工点>\"`，reason 要写清楚 worker 下一轮要改什么。\n\n", task.ID)

	b.WriteString("## 边界\n")
	b.WriteString("- 不要自己改实现。你只裁决（accept / changes）；要改由 worker 下一轮做。\n")

	// git-native knowledge injection (spec §3): memory + matching skills. When
	// nothing applies KnowledgeFor returns "" and the briefing is unchanged.
	if kn := KnowledgeFor(dir, "reviewer", task.Spec+" "+task.ID); kn != "" {
		b.WriteString("\n" + kn)
	}

	return b.String()
}
