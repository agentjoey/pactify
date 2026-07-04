package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/projection"
)

// writePact lays a fake `.pact/` tree under dir: an optional memory.md and any
// number of skills files (name → contents).
func writePact(t *testing.T, dir, memory string, skills map[string]string) {
	t.Helper()
	pactDir := filepath.Join(dir, ".pact")
	if memory != "" {
		if err := os.MkdirAll(pactDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pactDir, "memory.md"), []byte(memory), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(skills) > 0 {
		skillsDir := filepath.Join(pactDir, "skills")
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range skills {
			if err := os.WriteFile(filepath.Join(skillsDir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Golden pre-change briefing bytes for a fixture worker/task, captured before
// the knowledge-injection change. With no `.pact/memory.md` and no
// `.pact/skills/`, KnowledgeFor returns "" and the briefing must be byte-for-byte
// identical to this.
const goldenWorkerBrief = "# pact worker — seat `alice` (roles: worker,backend)\n\n" +
	"You are seat `alice` working task `t-42` in this repo (pact protocol v1).\n\n" +
	"## 冷启动\n" +
	"1. `pactify join alice --roles worker,backend` — registers your seat and checks out the feature branch.\n" +
	"2. `pactify status` — confirm `t-42` is yours.\n\n" +
	"## 干活\n" +
	"- 读规格：`docs/specs/t-42.md`。只碰该 spec 列出的文件。\n" +
	"- TDD：先写失败测试，再实现，跑该 task 规格里的验收命令直到通过。\n" +
	"- 完成后 `pactify checkpoint t-42` 并附上 evidence（验收命令输出）交回给 reviewer。\n\n" +
	"## 边界\n" +
	"- 不要自标 `accepted`，不能自接受。worker 只把 task 置为 awaiting_review；只有该 task 的 reviewer 能 accept。\n" +
	"- 不碰别的 task，不碰 spec 以外的文件。\n"

const goldenReviewerBrief = "# pact reviewer — seat `bob`\n\n" +
	"You are seat `bob`, reviewing task `t-99` (status awaiting_review) in this repo (pact protocol v1).\n\n" +
	"## 审什么\n" +
	"- 读规格：`docs/specs/t-99.md`，确认验收标准。\n" +
	"- 看 worker 的改动：`git diff` 看代码、`git log` 看提交，比对是否切题、是否越界改了 spec 外的文件。\n" +
	"- 跑该 task 规格里的验收命令，亲自验证它真的过——不要只看 worker 自报的 evidence。\n\n" +
	"## 裁决\n" +
	"- 通过：`pactify accept t-99`。\n" +
	"- 打回：`pactify changes t-99 --reason \"<具体返工点>\"`，reason 要写清楚 worker 下一轮要改什么。\n\n" +
	"## 边界\n" +
	"- 不要自己改实现。你只裁决（accept / changes）；要改由 worker 下一轮做。\n"

// TestBriefNoKnowledgeByteIdentical is the critical guard: with no memory and no
// skills the briefing bytes are unchanged from before the knowledge change.
func TestBriefNoKnowledgeByteIdentical(t *testing.T) {
	dir := t.TempDir() // empty: no .pact/

	seat := projection.Seat{ID: "alice", Roles: []string{"worker", "backend"}}
	task := projection.Task{ID: "t-42", Owner: "alice", Status: "assigned", Spec: "docs/specs/t-42.md"}
	if got := workerBrief(dir, seat, task, "", false); got != goldenWorkerBrief {
		t.Errorf("worker brief not byte-identical to golden.\n--- got ---\n%q\n--- want ---\n%q", got, goldenWorkerBrief)
	}

	rseat := projection.Seat{ID: "bob", Roles: []string{"reviewer"}}
	rtask := projection.Task{ID: "t-99", Owner: "alice", Reviewer: "bob", Status: "awaiting_review", Spec: "docs/specs/t-99.md"}
	if got := reviewerBrief(dir, rseat, rtask); got != goldenReviewerBrief {
		t.Errorf("reviewer brief not byte-identical to golden.\n--- got ---\n%q\n--- want ---\n%q", got, goldenReviewerBrief)
	}
}

// TestBriefKnowledgeAppendedOnlyWhenPresent confirms memory.md flows into the
// worker brief as a "## 项目知识" section appended after the base briefing.
func TestBriefKnowledgeAppendedToWorkerBrief(t *testing.T) {
	dir := t.TempDir()
	writePact(t, dir, "记住：这个项目用 tabs 不用 spaces。", nil)

	seat := projection.Seat{ID: "alice", Roles: []string{"worker"}}
	task := projection.Task{ID: "t-42", Spec: "docs/specs/t-42.md"}
	body := workerBrief(dir, seat, task, "", false)

	// Base briefing preserved, knowledge appended after it.
	if !strings.HasPrefix(body, "# pact worker") {
		t.Fatalf("base briefing not preserved:\n%s", body)
	}
	mustContain(t, body, "## 项目知识", "knowledge header")
	mustContain(t, body, "tabs 不用 spaces", "memory body")
}

func TestKnowledgeForEmptyWhenNoFiles(t *testing.T) {
	dir := t.TempDir()
	if got := KnowledgeFor(dir, "worker", "docs/specs/x.md t-1"); got != "" {
		t.Errorf("expected empty knowledge with no files, got:\n%q", got)
	}
}

func TestKnowledgeForMemoryFirst(t *testing.T) {
	dir := t.TempDir()
	writePact(t, dir, "MEMORY-LINE", map[string]string{
		"a.md": "---\nroles: []\nkeywords: []\n---\nSKILL-BODY",
	})
	got := KnowledgeFor(dir, "worker", "anything")
	mi := strings.Index(got, "MEMORY-LINE")
	si := strings.Index(got, "SKILL-BODY")
	if mi == -1 || si == -1 {
		t.Fatalf("missing memory or skill:\n%s", got)
	}
	if mi > si {
		t.Errorf("memory should precede skills:\n%s", got)
	}
	if !strings.HasPrefix(got, "## 项目知识\n") {
		t.Errorf("section should start with header:\n%s", got)
	}
}

// TestKnowledgeRoleFilter: a reviewer-only skill is injected for reviewer but
// not for worker; an all-roles skill (empty roles) appears for both.
func TestKnowledgeRoleFilter(t *testing.T) {
	dir := t.TempDir()
	writePact(t, dir, "", map[string]string{
		"reviewer-only.md": "---\nroles: [reviewer]\nkeywords: []\n---\nREVIEWER-SKILL",
		"all-roles.md":     "---\nroles: []\nkeywords: []\n---\nALL-SKILL",
	})

	worker := KnowledgeFor(dir, "worker", "hint")
	if strings.Contains(worker, "REVIEWER-SKILL") {
		t.Errorf("reviewer-only skill leaked into worker knowledge:\n%s", worker)
	}
	if !strings.Contains(worker, "ALL-SKILL") {
		t.Errorf("all-roles skill missing from worker knowledge:\n%s", worker)
	}

	reviewer := KnowledgeFor(dir, "reviewer", "hint")
	if !strings.Contains(reviewer, "REVIEWER-SKILL") {
		t.Errorf("reviewer-only skill missing from reviewer knowledge:\n%s", reviewer)
	}
	if !strings.Contains(reviewer, "ALL-SKILL") {
		t.Errorf("all-roles skill missing from reviewer knowledge:\n%s", reviewer)
	}
}

// TestKnowledgeKeywordHitMiss: keyword match is a case-insensitive substring of
// the task hint; a non-matching keyword excludes the skill; empty keywords
// always match.
func TestKnowledgeKeywordHitMiss(t *testing.T) {
	dir := t.TempDir()
	writePact(t, dir, "", map[string]string{
		"frontend.md": "---\nroles: []\nkeywords: [Frontend, ui]\n---\nFRONTEND-SKILL",
		"backend.md":  "---\nroles: []\nkeywords: [database]\n---\nBACKEND-SKILL",
		"always.md":   "---\nroles: []\nkeywords: []\n---\nALWAYS-SKILL",
	})

	// Hint contains "frontend" (lowercase) — the [Frontend] keyword must match
	// case-insensitively; "database" must not.
	got := KnowledgeFor(dir, "worker", "docs/specs/frontend-widget.md build the FRONTEND")
	if !strings.Contains(got, "FRONTEND-SKILL") {
		t.Errorf("keyword hit missing (case-insensitive):\n%s", got)
	}
	if strings.Contains(got, "BACKEND-SKILL") {
		t.Errorf("non-matching keyword skill leaked in:\n%s", got)
	}
	if !strings.Contains(got, "ALWAYS-SKILL") {
		t.Errorf("empty-keywords skill should always match:\n%s", got)
	}
}

// TestKnowledgeTruncation: a section exceeding the 4KB budget is cut with the
// truncation marker.
func TestKnowledgeTruncation(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("X", 8000)
	writePact(t, dir, big, nil)

	got := KnowledgeFor(dir, "worker", "hint")
	if len(got) > knowledgeBudget {
		t.Errorf("knowledge exceeds budget: %d > %d", len(got), knowledgeBudget)
	}
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("truncated section should end with marker, got tail: %q", got[len(got)-40:])
	}
}

// TestKnowledgeMalformedSkipped: a skill with no closing frontmatter fence (or
// no fence at all) is skipped without error; well-formed siblings still inject.
func TestKnowledgeMalformedSkipped(t *testing.T) {
	dir := t.TempDir()
	writePact(t, dir, "", map[string]string{
		"broken-noclose.md": "---\nroles: []\nkeywords: []\nBROKEN-BODY-NO-CLOSE",
		"broken-nofence.md": "roles: worker\njust text BROKEN-NO-FENCE",
		"good.md":           "---\nroles: []\nkeywords: []\n---\nGOOD-SKILL",
	})

	got := KnowledgeFor(dir, "worker", "hint")
	if strings.Contains(got, "BROKEN-BODY-NO-CLOSE") || strings.Contains(got, "BROKEN-NO-FENCE") {
		t.Errorf("malformed skill body leaked in:\n%s", got)
	}
	if !strings.Contains(got, "GOOD-SKILL") {
		t.Errorf("well-formed skill missing after malformed sibling skipped:\n%s", got)
	}
}
