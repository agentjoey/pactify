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
func workerBrief(seat projection.Seat, task projection.Task, changesReason string) string {
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

	return b.String()
}

// reviewerBrief renders the briefing handed to a headless agent launched to
// review an awaiting_review task. It names the seat, the task id + spec path,
// points at the worker's changes via `git diff` / `git log`, instructs running
// the spec's acceptance commands, and gives the accept / changes verbs while
// forbidding the reviewer from editing the implementation.
func reviewerBrief(seat projection.Seat, task projection.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pact reviewer — seat `%s`\n\n", seat.ID)
	fmt.Fprintf(&b, "You are seat `%s`, reviewing task `%s` (status awaiting_review) in this repo (pact protocol v1).\n\n", seat.ID, task.ID)

	b.WriteString("## 审什么\n")
	fmt.Fprintf(&b, "- 读规格：`%s`，确认验收标准。\n", task.Spec)
	b.WriteString("- 看 worker 的改动：`git diff` 看代码、`git log` 看提交，比对是否切题、是否越界改了 spec 外的文件。\n")
	b.WriteString("- 跑该 task 规格里的验收命令，亲自验证它真的过——不要只看 worker 自报的 evidence。\n\n")

	b.WriteString("## 裁决\n")
	fmt.Fprintf(&b, "- 通过：`pactify accept %s`。\n", task.ID)
	fmt.Fprintf(&b, "- 打回：`pactify changes %s --reason \"<具体返工点>\"`，reason 要写清楚 worker 下一轮要改什么。\n\n", task.ID)

	b.WriteString("## 边界\n")
	b.WriteString("- 不要自己改实现。你只裁决（accept / changes）；要改由 worker 下一轮做。\n")

	return b.String()
}
