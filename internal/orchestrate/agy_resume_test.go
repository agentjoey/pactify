package orchestrate

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/tokens"
)

// TestParseAntigravityConversationID_RealSample uses the same VERIFIED real
// agy result object as TestParseAntigravityTokens_RealSample (agy_tokens_test.go)
// to prove both parsers read off the SAME captured stdout without interfering.
func TestParseAntigravityConversationID_RealSample(t *testing.T) {
	id, ok := parseAntigravityConversationID(realAgySample)
	if !ok || id != "9940825b-1234" {
		t.Fatalf("parseAntigravityConversationID(real sample) = (%q,%v), want (9940825b-1234,true)", id, ok)
	}
}

func TestParseAntigravityConversationID_PrettyPrinted(t *testing.T) {
	out := "{\n  \"conversation_id\": \"pp-id-1\",\n  \"status\": \"SUCCESS\"\n}\n"
	id, ok := parseAntigravityConversationID(out)
	if !ok || id != "pp-id-1" {
		t.Fatalf("parseAntigravityConversationID(pretty) = (%q,%v), want (pp-id-1,true)", id, ok)
	}
}

func TestParseAntigravityConversationID_MalformedJSON(t *testing.T) {
	id, ok := parseAntigravityConversationID(`{"conversation_id":`)
	if ok || id != "" {
		t.Fatalf("parseAntigravityConversationID(malformed) = (%q,%v), want (\"\",false)", id, ok)
	}
}

func TestParseAntigravityConversationID_EmptyOutput(t *testing.T) {
	id, ok := parseAntigravityConversationID("")
	if ok || id != "" {
		t.Fatalf("parseAntigravityConversationID(empty) = (%q,%v), want (\"\",false)", id, ok)
	}
}

// TestParseAntigravityConversationID_IsNotAnErrorFilter pins what an ERROR-status
// result actually looks like, and what this parser is (and is not) responsible for.
//
// CORRECTION (audit, 2026-08-22): this test previously asserted only the empty-id
// shape and its comment claimed a real ERROR result carries conversation_id:"".
// That premise is FALSE in general — it holds only for an argv-validation failure
// (e.g. a bad --model), which fails before any conversation exists. A MID-RUN
// failure was captured live carrying a POPULATED conversation_id alongside
// status=ERROR (and exiting 0). Testing only the first shape made the parser look
// like an error filter it never was.
//
// So the behavior that matters, asserted below: parseAntigravityConversationID
// reports whatever id is on the object, ERROR or not. It returns ("",false) for
// the argv-failure shape purely because the FIELD is empty, not because the status
// is ERROR. Rejecting a failed run is parseAntigravityStatus's job, and the runner
// checks that BEFORE it records anything (see the healthy gate in CmdRunner.Run).
func TestParseAntigravityConversationID_IsNotAnErrorFilter(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		wantID string
		wantOK bool
	}{
		{
			// Argv-validation failure (bad --model): agy exits 1 before a
			// conversation exists, so the field really is empty.
			name:   "argv validation failure: empty id",
			out:    `{"conversation_id":"","status":"ERROR","response":"","error":"invalid model selection"}`,
			wantID: "", wantOK: false,
		},
		{
			// Mid-run failure (live-captured): a conversation was created and used,
			// then something failed. agy exits 0 and the id IS populated. The parser
			// must return it — this function is not the place failed runs get
			// rejected.
			name:   "mid-run failure: populated id, ERROR status",
			out:    `{"conversation_id":"mid-run-conv-id","status":"ERROR","response":"partial",` + `"error":"tool call rejected","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`,
			wantID: "mid-run-conv-id", wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := parseAntigravityConversationID(tc.out)
			if id != tc.wantID || ok != tc.wantOK {
				t.Fatalf("parseAntigravityConversationID = (%q,%v), want (%q,%v)", id, ok, tc.wantID, tc.wantOK)
			}
			// Both shapes are ERROR, and THAT is the signal the caller gates on.
			st, stOK := parseAntigravityStatus(tc.out)
			if !stOK || st != "ERROR" {
				t.Fatalf("parseAntigravityStatus = (%q,%v), want (ERROR,true) — "+
					"the status parser, not the id parser, is what rejects a failed run", st, stOK)
			}
		})
	}
}

func TestParseAntigravityConversationID_NoiseTolerant(t *testing.T) {
	out := "some log chatter\n" + `{"conversation_id":"noisy-id","status":"SUCCESS"}` + "\ntrailing junk\n"
	id, ok := parseAntigravityConversationID(out)
	if !ok || id != "noisy-id" {
		t.Fatalf("parseAntigravityConversationID(noisy) = (%q,%v), want (noisy-id,true)", id, ok)
	}
}

// --- agyResumeArgsIfAny ---

func TestAgyResumeArgsIfAny_NoRecord_Unchanged(t *testing.T) {
	dir := t.TempDir()
	base := []string{"-p", "{briefing}", "--model", "m1"}
	lc := LaunchContext{Seat: "a1", Task: "t1", RepoDir: dir}
	got, resumed := agyResumeArgsIfAny(lc, base)
	if resumed {
		t.Fatal("no stored record: resumed should be false")
	}
	if argsHave(got, "--conversation") {
		t.Fatalf("args = %v, must not contain --conversation with no stored record", got)
	}
	if len(got) != len(base) {
		t.Fatalf("args = %v, want base unchanged (%v)", got, base)
	}
}

func TestAgyResumeArgsIfAny_WithRecord_AppendsFlag(t *testing.T) {
	dir := t.TempDir()
	if err := RecordSession(dir, "a1", "t1", "antigravity", "conv-123"); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	base := []string{"-p", "{briefing}", "--model", "m1"}
	lc := LaunchContext{Seat: "a1", Task: "t1", RepoDir: dir}
	got, resumed := agyResumeArgsIfAny(lc, base)
	if !resumed {
		t.Fatal("stored record present: resumed should be true")
	}
	want := []string{"-p", "{briefing}", "--model", "m1", "--conversation", "conv-123"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestAgyResumeArgsIfAny_NoTask_Unchanged(t *testing.T) {
	dir := t.TempDir()
	if err := RecordSession(dir, "a1", "t1", "antigravity", "conv-123"); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	base := []string{"-p", "{briefing}"}
	// Task empty — LaunchContext with no task never resumes (mirrors codex's guard).
	lc := LaunchContext{Seat: "a1", Task: "", RepoDir: dir}
	got, resumed := agyResumeArgsIfAny(lc, base)
	if resumed || argsHave(got, "--conversation") {
		t.Fatalf("empty task must never resume, got args=%v resumed=%v", got, resumed)
	}
}

// --- End-to-end through CmdRunner.Run ---

// agyExecOut returns an execFn that behaves like a real agy invocation: it
// writes out to the captured stdout and returns ret, while also recording the
// args it was called with into calls (for inspecting --conversation presence).
func agyExecOut(calls *[][]string, out string, ret error) execFn {
	return func(_ context.Context, _ string, args []string, _ string, _ []string, capture io.Writer) error {
		*calls = append(*calls, append([]string(nil), args...))
		if capture != nil {
			_, _ = io.WriteString(capture, out)
		}
		return ret
	}
}

// First stint for a (seat,task): argv must NOT contain --conversation (cold
// start, no record yet); after a successful run, the store holds a record
// keyed to that (seat,task) with the conversation_id agy reported.
func TestCmdRunner_Antigravity_FirstStint_ColdStartThenRecords(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-resume", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(calls))
	}
	if argsHave(calls[0], "--conversation") {
		t.Fatalf("first stint args = %v, must NOT contain --conversation", calls[0])
	}
	got, ok := LookupSession(dir, "agy1", "t-resume")
	if !ok || got != "9940825b-1234" {
		t.Fatalf("LookupSession after first stint = (%q,%v), want (9940825b-1234,true)", got, ok)
	}
}

// Second stint for the SAME (seat,task) resumes: argv carries
// `--conversation <id-from-first-run>`.
func TestCmdRunner_Antigravity_SecondStint_ResumesWithConversationID(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-resume", Briefing: "go", RepoDir: dir}

	var calls [][]string
	r1 := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	if err := r1.Run(context.Background(), lc); err != nil {
		t.Fatalf("run 1: %v", err)
	}

	secondOut := `{"conversation_id":"9940825b-1234","status":"SUCCESS","response":"more",` +
		`"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`
	r2 := CmdRunner{Exec: agyExecOut(&calls, secondOut, nil)}
	if err := r2.Run(context.Background(), lc); err != nil {
		t.Fatalf("run 2: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(calls))
	}
	if argsHave(calls[0], "--conversation") {
		t.Fatalf("run 1 args = %v, must NOT contain --conversation", calls[0])
	}
	if !argsHave(calls[1], "--conversation") || !argsHave(calls[1], "9940825b-1234") {
		t.Fatalf("run 2 args = %v, want --conversation 9940825b-1234", calls[1])
	}
}

// A different task for the same seat never picks up the first task's
// conversation id — cold start every time until IT records its own.
func TestCmdRunner_Antigravity_DifferentTask_NeverReuses(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	var calls [][]string
	r1 := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	lcTaskA := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-a", Briefing: "go", RepoDir: dir}
	if err := r1.Run(context.Background(), lcTaskA); err != nil {
		t.Fatalf("run task A: %v", err)
	}

	r2 := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	lcTaskB := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-b", Briefing: "go", RepoDir: dir}
	if err := r2.Run(context.Background(), lcTaskB); err != nil {
		t.Fatalf("run task B: %v", err)
	}

	if argsHave(calls[1], "--conversation") {
		t.Fatalf("a different task must cold-start, got args=%v", calls[1])
	}
	if _, ok := LookupSession(dir, "agy1", "t-b"); !ok {
		t.Fatal("task B should have recorded its own session after success")
	}
}

// A different seat working the SAME task never picks up the first seat's
// conversation id either — the store is keyed by (seat,task), not task alone.
func TestCmdRunner_Antigravity_DifferentSeat_NeverReuses(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	var calls [][]string
	r1 := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	lcSeatA := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-shared", Briefing: "go", RepoDir: dir}
	if err := r1.Run(context.Background(), lcSeatA); err != nil {
		t.Fatalf("run seat A: %v", err)
	}

	r2 := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	lcSeatB := LaunchContext{Seat: "agy2", Kind: "antigravity", Task: "t-shared", Briefing: "go", RepoDir: dir}
	if err := r2.Run(context.Background(), lcSeatB); err != nil {
		t.Fatalf("run seat B: %v", err)
	}

	if argsHave(calls[1], "--conversation") {
		t.Fatalf("a different seat on the same task must cold-start, got args=%v", calls[1])
	}
}

// TestCmdRunner_Antigravity_RejectedResumeFallsBackColdWithoutFailing models
// the REAL, live-verified rejection shape (2026-08-22, see
// parseAntigravityConversationID's doc comment): a stale/unknown
// --conversation id does NOT fail the agy process. agy prints a
// `warning: conversation "<id>" not found` line to STDERR (never captured on
// this stdout-only path) and transparently mints a FRESH conversation, still
// exiting 0/SUCCESS with that fresh id in its JSON result.
//
// This fake models exactly that: it's handed --conversation <stale-id> but its
// SUCCESS output reports a DIFFERENT (fresh) conversation_id, exactly as the
// real binary did in the live reproduction. The stint must succeed (not fail)
// and the store must end up holding the FRESH id, not the stale one it started
// with — "clears the record and falls back to cold start" without any
// dedicated rejection-detection code path.
func TestCmdRunner_Antigravity_RejectedResumeFallsBackColdWithoutFailing(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	if err := RecordSession(dir, "agy1", "t-stale", "antigravity", "stale-conv-id"); err != nil {
		t.Fatalf("seed stale record: %v", err)
	}

	freshOut := `{"conversation_id":"fresh-conv-id","status":"SUCCESS","response":"hi",` +
		`"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`
	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, freshOut, nil)}
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-stale", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run must not fail on a rejected resume (agy self-heals): %v", err)
	}

	if !argsHave(calls[0], "--conversation") || !argsHave(calls[0], "stale-conv-id") {
		t.Fatalf("the stint should have attempted resume with the stale id, args=%v", calls[0])
	}
	got, ok := LookupSession(dir, "agy1", "t-stale")
	if !ok || got != "fresh-conv-id" {
		t.Fatalf("LookupSession after rejected resume = (%q,%v), want (fresh-conv-id,true) — "+
			"stale id must be overwritten by the fresh one agy actually used", got, ok)
	}
}

// TestCmdRunner_Antigravity_ResumeHardFailure_ClearsRecord covers the
// DEFENSIVE symmetry-with-codex branch (resumed && err!=nil → ClearSession) —
// NOT reproduced against the real agy binary (every --conversation rejection
// observed live fell back silently with exit 0; see the test above). This
// guards a hypothetical future agy hard-failure mode: the record must not
// wedge every subsequent retry on a now-dead id. Mirrors
// TestCmdRunner_Codex_ResumeRetry's shape for the analogous codex mechanic.
func TestCmdRunner_Antigravity_ResumeHardFailure_ClearsRecord(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	if err := RecordSession(dir, "agy1", "t-hardfail", "antigravity", "dead-conv-id"); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, "", errors.New("agy process hard-failed"))}
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-hardfail", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err == nil {
		t.Fatal("a genuine exec failure should still propagate (only the store self-heals)")
	}
	if !argsHave(calls[0], "--conversation") || !argsHave(calls[0], "dead-conv-id") {
		t.Fatalf("the stint should have attempted resume, args=%v", calls[0])
	}
	if _, ok := LookupSession(dir, "agy1", "t-hardfail"); ok {
		t.Fatal("a resumed run that hard-fails must clear the stale record")
	}

	// The next retry cold-starts (record was cleared) and can succeed.
	r2 := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	if err := r2.Run(context.Background(), lc); err != nil {
		t.Fatalf("retry after cleared record should cold-start and succeed: %v", err)
	}
	if argsHave(calls[1], "--conversation") {
		t.Fatalf("retry after cleared record must cold-start, got args=%v", calls[1])
	}
}

// --- headWriter (the capture window agy's leading fields are read from) ---

// headWriter keeps the FIRST max bytes. agy emits one object whose leading fields
// are conversation_id and status, so a tail window loses both once the response
// outgrows it. These cases pin the properties the runner depends on: the head is
// kept (not the tail), the cap is respected, and Write always reports the FULL
// length with a nil error — a short write here would abort the io.MultiWriter
// splice on the child's stdout and stall the agent.
func TestHeadWriter_KeepsFirstBytesUpToMax(t *testing.T) {
	t.Run("under the cap keeps everything", func(t *testing.T) {
		w := &headWriter{max: 16}
		n, err := w.Write([]byte("abcdef"))
		if n != 6 || err != nil {
			t.Fatalf("Write = (%d,%v), want (6,nil)", n, err)
		}
		if got := w.String(); got != "abcdef" {
			t.Fatalf("String = %q, want %q", got, "abcdef")
		}
	})

	t.Run("one write larger than max truncates to the head", func(t *testing.T) {
		w := &headWriter{max: 4}
		n, err := w.Write([]byte("abcdefghij"))
		// Must claim the whole write, or MultiWriter reports ErrShortWrite and the
		// child's stdout pipe breaks.
		if n != 10 || err != nil {
			t.Fatalf("Write = (%d,%v), want (10,nil) — a short write would stall the child", n, err)
		}
		if got := w.String(); got != "abcd" {
			t.Fatalf("String = %q, want %q (FIRST bytes, not last)", got, "abcd")
		}
	})

	t.Run("many small writes stop appending at max", func(t *testing.T) {
		w := &headWriter{max: 5}
		for i := 0; i < 100; i++ {
			if n, err := w.Write([]byte("xy")); n != 2 || err != nil {
				t.Fatalf("Write %d = (%d,%v), want (2,nil)", i, n, err)
			}
		}
		if got := w.String(); got != "xyxyx" {
			t.Fatalf("String = %q, want %q", got, "xyxyx")
		}
		if len(w.buf) > w.max {
			t.Fatalf("buffer grew past max: len=%d max=%d", len(w.buf), w.max)
		}
	})

	t.Run("head keeps leading fields a smaller tail window loses", func(t *testing.T) {
		// The reason the head window exists: agy's result object leads with
		// conversation_id and status, so a tail window smaller than the object keeps
		// the usage block and drops both. Written as ONE complete object (agy emits
		// exactly one) that fits inside the head but not the tail.
		obj := `{"conversation_id":"lead-id","status":"SUCCESS","response":"` +
			strings.Repeat("z", 512) + `","usage":{"total_tokens":7}}`
		head := &headWriter{max: headCaptureCap}
		tail := &tailWriter{max: 64}
		for _, w := range []io.Writer{head, tail} {
			_, _ = io.WriteString(w, obj)
		}
		if id, ok := parseAntigravityConversationID(head.String()); !ok || id != "lead-id" {
			t.Fatalf("head capture = (%q,%v), want (lead-id,true)", id, ok)
		}
		if st, ok := parseAntigravityStatus(head.String()); !ok || st != "SUCCESS" {
			t.Fatalf("status from head capture = (%q,%v), want (SUCCESS,true)", st, ok)
		}
		if _, ok := parseAntigravityConversationID(tail.String()); ok {
			t.Fatal("sanity: the tail window should have lost the leading fields — " +
				"if it hasn't, this test no longer proves the head window is needed")
		}
	})
}

// TestHeadCapture_TruncatedObject_BothFieldsSurvive covers the case the head
// window exists for: an agy result object larger than headCaptureCap (8 KiB),
// where the captured fragment is no longer valid JSON.
//
// Both leading fields must still be recoverable. status was always fine (it had a
// scanStringField fallback); conversation_id was NOT — it had only the
// json.Unmarshal fast path and a per-line scan requiring valid JSON, so it
// returned ("",false) while the id sat verbatim in the fragment. That made the
// head window useless for exactly the case it was introduced for: for a result
// over 8 KiB, resume would silently never engage (the 1 MiB tail already covers
// everything smaller). Found by the independent test audit, fixed in
// agy_tokens.go the same day.
func TestHeadCapture_TruncatedObject_BothFieldsSurvive(t *testing.T) {
	// A result object cut off mid-`response` by the head window.
	frag := `{"conversation_id":"lead-id","status":"SUCCESS","response":"` + strings.Repeat("z", 64)

	if st, ok := parseAntigravityStatus(frag); !ok || st != "SUCCESS" {
		t.Fatalf("status from a truncated head = (%q,%v), want (SUCCESS,true) — "+
			"the scan fallback must survive truncation", st, ok)
	}
	if id, ok := parseAntigravityConversationID(frag); !ok || id != "lead-id" {
		t.Fatalf("conversation_id from a truncated head = (%q,%v), want (lead-id,true) — "+
			"without this the head window buys nothing: resume never engages for a "+
			">8 KiB agy result", id, ok)
	}
}

// --- Cross-kind session contamination (regression) ---

// TestCmdRunner_Antigravity_NeverInheritsCodexThreadID is the regression test for
// the exposure LookupSessionKind exists to close: the session store is SHARED by
// codex-cli, the ACP path and agy, keyed by (seat,task). A seat re-kinded
// mid-feature (dynamic `join --kind`, `--seat-kind`, a fallback-role switch)
// leaves a codex-cli record under the very (seat,task) an agy stint then looks up.
// Kind-blind, agy would be handed a codex thread_id as its --conversation.
//
// Mutation-verified: reverting agyResumeArgsIfAny to the kind-blind LookupSession
// makes this test fail (argv gains --conversation codex-thread-abc).
func TestCmdRunner_Antigravity_NeverInheritsCodexThreadID(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	// The seat previously worked this task as codex-cli.
	if err := RecordSession(dir, "seat1", "t-rekind", "codex-cli", "codex-thread-abc"); err != nil {
		t.Fatalf("seed codex record: %v", err)
	}

	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	lc := LaunchContext{Seat: "seat1", Kind: "antigravity", Task: "t-rekind", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run: %v", err)
	}
	if argsHave(calls[0], "--conversation") {
		t.Fatalf("agy must cold-start over a codex-cli record for the same (seat,task), got args=%v", calls[0])
	}
	if argsHave(calls[0], "codex-thread-abc") {
		t.Fatalf("agy inherited codex's thread id as its conversation id: args=%v", calls[0])
	}
	// NOTE (current behavior, pinned not endorsed): RecordSession keys by
	// (seat,task) ALONE, so agy's own successful record now REPLACES the codex row
	// rather than coexisting with it. The read side (LookupSessionKind) and the
	// delete side (RemoveSession) are kind-checked; the write side is not.
	if id, ok := LookupSessionKind(dir, "seat1", "t-rekind", "antigravity"); !ok || id != "9940825b-1234" {
		t.Fatalf("agy should have recorded its own id, got (%q,%v)", id, ok)
	}
}

// The mirror: an agy FAILURE must not delete another kind's record for the same
// (seat,task). Seeds the store directly with two rows (RecordSession upserts by
// (seat,task) and so cannot express the two-kind state the store's readers must
// tolerate) and drives a resumed agy stint to a hard failure.
//
// Mutation-verified: swapping the runner's kind-checked RemoveSession for the
// kind-blind ClearSession makes this fail — the codex row disappears too.
func TestCmdRunner_Antigravity_Failure_LeavesOtherKindsRecordIntact(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	seed := []SessionRecord{
		{Seat: "seat1", Task: "t-shared", Kind: "antigravity", SessionID: "agy-conv", UpdatedAt: "2026-08-22T00:00:00Z"},
		{Seat: "seat1", Task: "t-shared", Kind: "codex-cli", SessionID: "codex-thread", UpdatedAt: "2026-08-22T00:00:00Z"},
	}
	if err := writeSessions(sessionsPath(dir), seed); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, "", errors.New("agy process hard-failed"))}
	lc := LaunchContext{Seat: "seat1", Kind: "antigravity", Task: "t-shared", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err == nil {
		t.Fatal("a genuine exec failure should propagate")
	}
	if !argsHave(calls[0], "agy-conv") {
		t.Fatalf("the stint should have resumed off the antigravity row, args=%v", calls[0])
	}
	if _, ok := LookupSessionKind(dir, "seat1", "t-shared", "antigravity"); ok {
		t.Fatal("the failed agy resume must clear ITS OWN record")
	}
	if id, ok := LookupSessionKind(dir, "seat1", "t-shared", "codex-cli"); !ok || id != "codex-thread" {
		t.Fatalf("codex-cli record for the same (seat,task) = (%q,%v), want (codex-thread,true) — "+
			"an agy failure must not delete another kind's session", id, ok)
	}
}

// --- Health gate: no session record for an unhealthy run ---

// agy can exit 0 while reporting {"status":"ERROR"} (live-verified: a mid-run tool
// rejection). Re-entering a conversation that ended in an error state is not
// something we have evidence works, so an unhealthy run must NOT leave a resume
// record — even though its conversation_id is populated and parseable.
//
// Mutation-verified: deleting the `healthy` gate from CmdRunner.Run (recording on
// any err==nil) makes this fail.
func TestCmdRunner_Antigravity_UnhealthyStatus_RecordsNoSession(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	out := `{"conversation_id":"abc","status":"ERROR","response":"partial",` +
		`"error":"tool call rejected","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`

	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, out, nil)} // exit 0 despite status=ERROR
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-unhealthy", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("status=ERROR must NOT be converted into a stint failure: %v", err)
	}
	// Precondition: the id really is there to be recorded, so a passing assertion
	// below means the health gate rejected it — not that parsing failed.
	if id, ok := parseAntigravityConversationID(out); !ok || id != "abc" {
		t.Fatalf("fixture sanity: conversation_id = (%q,%v), want (abc,true)", id, ok)
	}
	if id, ok := LookupSessionKind(dir, "agy1", "t-unhealthy", "antigravity"); ok {
		t.Fatalf("an unhealthy (status!=SUCCESS) run must not record a resume id, got %q", id)
	}
	recs, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("session store should stay empty after an unhealthy run, got %+v", recs)
	}
	// Tokens are still recorded — they were really spent.
	if n := tokens.Load(dir).Get("t-unhealthy"); n != 15 {
		t.Fatalf("tokens for an unhealthy run = %d, want 15 (spend is real regardless of status)", n)
	}
}

// The resumed-but-unhealthy case: the conversation being resumed is the thing that
// errored, so the EXISTING record must be REMOVED, not merely left un-refreshed —
// otherwise every subsequent retry re-enters the same bad state.
func TestCmdRunner_Antigravity_ResumedThenUnhealthy_RemovesRecord(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	if err := RecordSession(dir, "agy1", "t-sick", "antigravity", "sick-conv"); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	out := `{"conversation_id":"sick-conv","status":"ERROR","response":"partial",` +
		`"error":"tool call rejected","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`

	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, out, nil)}
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-sick", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !argsHave(calls[0], "--conversation") || !argsHave(calls[0], "sick-conv") {
		t.Fatalf("the stint should have resumed, args=%v", calls[0])
	}
	if id, ok := LookupSessionKind(dir, "agy1", "t-sick", "antigravity"); ok {
		t.Fatalf("a resumed run that ends unhealthy must drop the record so the next "+
			"attempt cold-starts, got %q", id)
	}

	// The next attempt does cold-start, and a healthy result records again.
	r2 := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	if err := r2.Run(context.Background(), lc); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if argsHave(calls[1], "--conversation") {
		t.Fatalf("retry after an unhealthy run must cold-start, args=%v", calls[1])
	}
	if id, ok := LookupSessionKind(dir, "agy1", "t-sick", "antigravity"); !ok || id != "9940825b-1234" {
		t.Fatalf("a healthy retry should record again, got (%q,%v)", id, ok)
	}
}

// An agy build that omits `status` entirely must not be treated as unhealthy —
// the gate keys off an explicitly non-SUCCESS status, not off the field's absence.
// (Pins the `ok &&` in the runner's health check: dropping it would make every
// status-less result unhealthy and silently disable resume.)
func TestCmdRunner_Antigravity_AbsentStatus_TreatedAsHealthy(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	out := `{"conversation_id":"no-status-conv","response":"done","usage":{"total_tokens":15}}`

	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, out, nil)}
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-nostatus", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if id, ok := LookupSessionKind(dir, "agy1", "t-nostatus", "antigravity"); !ok || id != "no-status-conv" {
		t.Fatalf("a result with no status field must still record, got (%q,%v)", id, ok)
	}
}

// A stint with no Task (non-orchestrated launch) never touches the session
// store at all — no lookup, no record, no panic.
func TestCmdRunner_Antigravity_NoTask_NoSessionActivity(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	var calls [][]string
	r := CmdRunner{Exec: agyExecOut(&calls, realAgySample, nil)}
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "", Briefing: "go", RepoDir: dir}
	if err := r.Run(context.Background(), lc); err != nil {
		t.Fatalf("run: %v", err)
	}
	if argsHave(calls[0], "--conversation") {
		t.Fatalf("no task: must not resume, args=%v", calls[0])
	}
	recs, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("no task: session store should stay empty, got %+v", recs)
	}
}
