package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

// WS-B ships the ref store DARK: the file remains authoritative and the ref is
// written alongside it only when explicitly enabled, so a bug in the new storage
// cannot cost anyone an event. These tests pin "off by default" as hard as they
// pin the shadow behavior itself.

func TestShadowIsOffByDefault(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "")
	if ShadowEnabled() {
		t.Fatal("the ref store must be opt-in — WS-B is a dark launch, not a switch-over")
	}
}

func TestShadowEnabledOnlyForKnownValues(t *testing.T) {
	for _, v := range []string{"1", "shadow", "true"} {
		t.Setenv("PACTIFY_LEDGER_REF", v)
		if !ShadowEnabled() {
			t.Errorf("PACTIFY_LEDGER_REF=%q should enable shadow writes", v)
		}
	}
	for _, v := range []string{"", "0", "off", "no", "maybe"} {
		t.Setenv("PACTIFY_LEDGER_REF", v)
		if ShadowEnabled() {
			t.Errorf("PACTIFY_LEDGER_REF=%q must NOT enable shadow writes", v)
		}
	}
}

// A failure in the shadow path must never surface to the caller: the file write
// already succeeded, and the protocol verb must not fail because an experimental
// mirror had a problem.
func TestShadowAppendNeverFailsTheCaller(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "1")
	// Not a git repo: the ref store cannot possibly work here.
	if err := ShadowAppend(t.TempDir(), `{"event_id":"a1"}`); err != nil {
		t.Fatalf("shadow append must swallow its own errors, got: %v", err)
	}
}

func TestShadowAppendIsANoOpWhenDisabled(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "")
	dir := newGitRepo(t)

	if err := ShadowAppend(dir, `{"event_id":"a1"}`); err != nil {
		t.Fatal(err)
	}
	head, err := RefHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if head != "" {
		t.Error("disabled shadow must not create the ledger ref at all")
	}
}

func TestShadowAppendMirrorsTheEventWhenEnabled(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "1")
	dir := newGitRepo(t)

	if err := ShadowAppend(dir, `{"event_id":"a1"}`); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadRef(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("want the event mirrored into the ref, got %v", lines)
	}
}

// The dual-READ half: comparing the two stores is how we learn whether the ref
// could be trusted as canonical, before WS-C makes it so.
func TestVerifyReportsAgreement(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "1")
	dir := newGitRepo(t)
	// 按**真实时序**：引擎每写一条文件就镜像同一条。早先这条测试先把整个文件写好
	// 再补镜像，那个状态在生产里不会出现，也因此掩盖了「中途开启需要播种」这件事。
	appendBoth(t, dir, `{"event_id":"a1"}`)
	appendBoth(t, dir, `{"event_id":"a2"}`)

	drift, err := Verify(dir)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if drift != "" {
		t.Errorf("stores agree, want no drift, got: %s", drift)
	}
}

func TestVerifyReportsDriftWithDetail(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "1")
	dir := newGitRepo(t)
	// 半途失败的镜像：第一条两边都有，第二条只落了文件。
	appendBoth(t, dir, `{"event_id":"a1"}`)
	writeFileLedger(t, dir, `{"event_id":"a1"}`, `{"event_id":"a2"}`)

	drift, err := Verify(dir)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if drift == "" {
		t.Fatal("stores disagree (2 file events vs 1 in ref) — Verify must say so")
	}
	// The report has to be actionable: counts, not just "they differ".
	for _, want := range []string{"2", "1"} {
		if !contains(drift, want) {
			t.Errorf("drift report %q should carry the counts", drift)
		}
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// appendBoth 模拟引擎的真实时序：先把事件追加进文件，再镜像同一行。
func appendBoth(t *testing.T, dir string, line string) {
	t.Helper()
	existing, _ := os.ReadFile(Path(dir))
	if err := os.MkdirAll(Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir), append(existing, []byte(line+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ShadowAppend(dir, line); err != nil {
		t.Fatal(err)
	}
}

func writeFileLedger(t *testing.T, dir string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(Dir(dir), "log.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 中途开启 flag 的仓库：文件里已经有历史事件，而 ref 是空的。若镜像只写「从现在起」
// 的事件，Verify 会对每个这样的仓库报漂移——那不是真漂移，是没有回填，
// 双读校验也就从第一天起没法用。首次写入必须把文件现状播种进 ref。
func TestShadowSeedsRefFromExistingFileOnFirstWrite(t *testing.T) {
	t.Setenv("PACTIFY_LEDGER_REF", "1")
	dir := newGitRepo(t)
	// 仓库已经跑了一阵：文件里有两条，ref 还不存在。
	writeFileLedger(t, dir, `{"event_id":"old1"}`, `{"event_id":"old2"}`)

	// 第三条事件在 flag 开启后到达（文件那边由引擎写，这里模拟其已落盘）。
	writeFileLedger(t, dir, `{"event_id":"old1"}`, `{"event_id":"old2"}`, `{"event_id":"new3"}`)
	if err := ShadowAppend(dir, `{"event_id":"new3"}`); err != nil {
		t.Fatal(err)
	}

	drift, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if drift != "" {
		t.Fatalf("首次镜像必须把已有历史一并播种，否则双读校验从第一天就失效：%s", drift)
	}
}
