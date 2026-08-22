package orchestrate

import (
	"context"
	"testing"

	"github.com/agentjoey/pactify/internal/tokens"
)

// realAgySample is the VERIFIED real agy `--output-format json` result object
// (dm-agy-tokens spec). In this sample total_tokens (20331) equals
// input_tokens+output_tokens (20300+31) — thinking_tokens (27) is NOT included
// either way (20300+31+27=20358 is not the total). Note this sample alone does
// not distinguish "read total_tokens as-is" from "sum input+output"; see
// TestParseAntigravityTokens_TotalDivergesFromSum for that.
const realAgySample = `{"conversation_id":"9940825b-1234","status":"SUCCESS","response":"done",` +
	`"duration_seconds":4.72,"num_turns":1,` +
	`"usage":{"input_tokens":20300,"output_tokens":31,"thinking_tokens":27,` +
	`"cache_read_tokens":0,"total_tokens":20331}}`

// divergentAgySample is realAgySample's SHAPE (same fields, same order — a full
// result object, not a usage-only fragment) with a usage block whose total_tokens
// cannot be produced from input+output by any combination: 100+50=150, but
// total_tokens is 777.
//
// Every test that claims to prove "antigravity reads total_tokens as-is" must use
// THIS fixture, not realAgySample. In the real sample total happens to equal
// input+output, so the generic tokens.Parse path (which sums them) returns the
// same number — an audit (2026-08-22) found both TestParseTokenUsage_DispatchesByKind
// and the CmdRunner recording test stayed GREEN with the `kind == "antigravity"`
// dispatch deleted from parseTokenUsage outright. With this fixture the two paths
// disagree (777 vs 150), so deleting the dispatch fails them immediately.
const divergentAgySample = `{"conversation_id":"9940825b-1234","status":"SUCCESS","response":"done",` +
	`"duration_seconds":4.72,"num_turns":1,` +
	`"usage":{"input_tokens":100,"output_tokens":50,"thinking_tokens":10,` +
	`"cache_read_tokens":5,"total_tokens":777}}`

func TestParseAntigravityTokens_RealSample(t *testing.T) {
	n, ok := parseAntigravityTokens(realAgySample)
	if !ok || n != 20331 {
		t.Fatalf("parseAntigravityTokens(real sample) = (%d,%v), want (20331,true)", n, ok)
	}
}

// A second real-world figure cited in the task spec (input 53,743 / output 2,248 /
// thinking 971 / total 55,991) — same caveat as realAgySample: total here equals
// input+output; it does not by itself prove total_tokens is read as-is rather than
// recomputed. See TestParseAntigravityTokens_TotalDivergesFromSum for that proof.
func TestParseAntigravityTokens_SecondRealFigure(t *testing.T) {
	out := `{"usage":{"input_tokens":53743,"output_tokens":2248,"thinking_tokens":971,` +
		`"cache_read_tokens":0,"total_tokens":55991}}`
	n, ok := parseAntigravityTokens(out)
	if !ok || n != 55991 {
		t.Fatalf("parseAntigravityTokens(second sample) = (%d,%v), want (55991,true)", n, ok)
	}
}

// TestParseAntigravityTokens_TotalDivergesFromSum is the one case that actually
// distinguishes "read usage.total_tokens as-is" from "recompute
// input_tokens+output_tokens" (both real samples above happen to make the two
// equal, so neither proves this on its own — flagged by independent review,
// 2026-08-22). Uses a synthetic total_tokens the two other fields cannot produce
// by any combination cited in the spec, so a regression to summing would be
// caught immediately: 100+50=150, but total_tokens is deliberately set to 777.
func TestParseAntigravityTokens_TotalDivergesFromSum(t *testing.T) {
	n, ok := parseAntigravityTokens(divergentAgySample)
	if !ok || n != 777 {
		t.Fatalf("parseAntigravityTokens(diverging sample) = (%d,%v), want (777,true) — "+
			"a wrong implementation that sums input+output would instead return 150", n, ok)
	}
}

func TestParseAntigravityTokens_PrettyPrinted(t *testing.T) {
	out := "{\n  \"status\": \"SUCCESS\",\n  \"usage\": {\n    \"total_tokens\": 999\n  }\n}\n"
	n, ok := parseAntigravityTokens(out)
	if !ok || n != 999 {
		t.Fatalf("parseAntigravityTokens(pretty) = (%d,%v), want (999,true)", n, ok)
	}
}

func TestParseAntigravityTokens_MalformedJSON(t *testing.T) {
	n, ok := parseAntigravityTokens(`{"usage":{"total_tokens":`) // truncated
	if ok || n != 0 {
		t.Fatalf("parseAntigravityTokens(malformed) = (%d,%v), want (0,false)", n, ok)
	}
}

func TestParseAntigravityTokens_MissingUsage(t *testing.T) {
	n, ok := parseAntigravityTokens(`{"conversation_id":"x","status":"SUCCESS","response":"done"}`)
	if ok || n != 0 {
		t.Fatalf("parseAntigravityTokens(missing usage) = (%d,%v), want (0,false)", n, ok)
	}
}

func TestParseAntigravityTokens_EmptyOutput(t *testing.T) {
	n, ok := parseAntigravityTokens("")
	if ok || n != 0 {
		t.Fatalf("parseAntigravityTokens(empty) = (%d,%v), want (0,false)", n, ok)
	}
	n, ok = parseAntigravityTokens("   \n  ")
	if ok || n != 0 {
		t.Fatalf("parseAntigravityTokens(whitespace) = (%d,%v), want (0,false)", n, ok)
	}
}

func TestParseAntigravityTokens_NotJSONAtAll(t *testing.T) {
	n, ok := parseAntigravityTokens("agy: hello, how can I help you today?")
	if ok || n != 0 {
		t.Fatalf("parseAntigravityTokens(non-json chatter) = (%d,%v), want (0,false)", n, ok)
	}
}

func TestParseAntigravityTokens_ZeroTotal(t *testing.T) {
	// total_tokens present but zero (or absent, defaulting to zero) must not be
	// treated as a successful zero-token parse — it is indistinguishable from
	// "field missing" and must fall through to the caller's no-op path.
	n, ok := parseAntigravityTokens(`{"usage":{"total_tokens":0}}`)
	if ok || n != 0 {
		t.Fatalf("parseAntigravityTokens(zero total) = (%d,%v), want (0,false)", n, ok)
	}
}

// --- parseAntigravityTokens: truncation fallback (tailWriter overflow) ---

// agy emits ONE result object with usage at its tail. When a stint's response
// exceeds tokenCaptureCap the tail window keeps the END of that object — the
// opening `{` is gone, so no JSON parser can accept the blob and the per-line
// scan skips it (the fragment does not start with `{`). scanIntField is the last
// resort: total_tokens is still intact and readable, and MUST be recovered, or a
// long agy stint silently records zero tokens.
func TestParseAntigravityTokens_TruncatedHeadStillFindsTotal(t *testing.T) {
	// Simulates the tail of the object after the opening brace (and the whole
	// response field) was cut away by the capture window.
	out := `is answer.","duration_seconds":9.1,"num_turns":3,` +
		`"usage":{"input_tokens":100,"output_tokens":50,"thinking_tokens":10,` +
		`"cache_read_tokens":5,"total_tokens":4242}}`
	n, ok := parseAntigravityTokens(out)
	if !ok || n != 4242 {
		t.Fatalf("parseAntigravityTokens(truncated head) = (%d,%v), want (4242,true) — "+
			"the bare-field fallback must recover total_tokens from a tail-only fragment", n, ok)
	}
}

// The same truncation, but the cut lands INSIDE the number: there is no trailing
// delimiter proving the digits ended, so the value could be any prefix of the
// real one (4 of 4242). Recording a wrong-by-orders-of-magnitude tally is worse
// than recording none, so this must decline: (0,false).
func TestParseAntigravityTokens_TruncatedMidNumberDeclines(t *testing.T) {
	out := `"cache_read_tokens":5,"total_tokens":4`
	n, ok := parseAntigravityTokens(out)
	if ok || n != 0 {
		t.Fatalf("parseAntigravityTokens(truncated mid-number) = (%d,%v), want (0,false) — "+
			"a digit run with no closing delimiter is not a trustworthy total", n, ok)
	}
}

// --- parseAntigravityStatus ---

// agy's exit code is not a sufficient success signal (a mid-run tool rejection
// exits 0 with status=ERROR), so the runner gates its resume record on this
// parser. It reads the HEAD window because status is a leading field.
func TestParseAntigravityStatus(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"success", realAgySample, "SUCCESS", true},
		{
			"error, mid-run failure (exit 0, populated conversation_id)",
			`{"conversation_id":"c-9","status":"ERROR","response":"partial","error":"tool call rejected"}`,
			"ERROR", true,
		},
		{
			"error, argv validation failure (exit 1, empty conversation_id)",
			`{"conversation_id":"","status":"ERROR","response":"","error":"invalid model selection"}`,
			"ERROR", true,
		},
		// No status field at all: the caller must NOT infer failure — an absent
		// status leaves the run treated as healthy (ok=false).
		{"absent status", `{"conversation_id":"c-1","response":"done"}`, "", false},
		{"empty input", "", "", false},
		{"whitespace only", "   \n\t ", "", false},
		{"not json at all", "agy: hello there", "", false},
		// HEAD-truncated: the object is cut off mid-`response`, so json.Unmarshal
		// fails outright. status precedes the cut, so the scan fallback must still
		// find it — otherwise an unhealthy long-output run is booked as healthy.
		{
			"head-truncated mid-response",
			`{"conversation_id":"c-2","status":"ERROR","response":"aaaaaaaaaaaaaaaaaaaa`,
			"ERROR", true,
		},
		{
			"head-truncated mid-response, success",
			`{"conversation_id":"c-3","status":"SUCCESS","response":"the quick brown fox jum`,
			"SUCCESS", true,
		},
		// Cut inside the status value itself: nothing trustworthy to report.
		{"truncated mid-status", `{"conversation_id":"c-4","status":"SUCC`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseAntigravityStatus(tc.in)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("parseAntigravityStatus(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// --- parseAntigravityError ---

// Operator-facing message only (never a control signal): it returns agy's error
// string when present, and a self-describing sentinel when it isn't — so an
// escalation message can never read as an empty/absent reason.
func TestParseAntigravityError(t *testing.T) {
	const sentinel = "(no error message in captured output)"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"error present",
			`{"conversation_id":"c-9","status":"ERROR","response":"","error":"tool call rejected"}`,
			"tool call rejected",
		},
		{
			"error present in a truncated fragment",
			`"status":"ERROR","error":"quota exhausted","respon`,
			"quota exhausted",
		},
		{"error absent on a SUCCESS result", realAgySample, sentinel},
		// An explicitly EMPTY error field yields the sentinel, not "". This was
		// briefly the opposite — scanStringField treated present-but-empty as a
		// hit — which mattered far more for conversation_id (agy emits
		// `"conversation_id":""` on an argv-validation failure, and an empty id was
		// being recorded as a resumable session) than it ever did here. Tightening
		// scanStringField to reject empty values fixed both; this case pins the
		// benign half of that fix.
		{"error field present but empty", `{"status":"ERROR","error":""}`, sentinel},
		{"no json at all", "agy exploded", sentinel},
		{"empty input", "", sentinel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAntigravityError(tc.in); got != tc.want {
				t.Fatalf("parseAntigravityError(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// parseTokenUsage must route antigravity through parseAntigravityTokens (taking
// total_tokens as-is) while leaving every other kind on the existing generic
// tokens.Parse path (input+output sum) — no cross-kind interference.
//
// Uses divergentAgySample deliberately: with realAgySample the two paths agree
// (total == input+output), so this test passed even with the antigravity dispatch
// deleted from parseTokenUsage. The assertions below now pin the dispatch itself —
// the SAME blob must yield 777 for antigravity and 150 for a generic kind.
func TestParseTokenUsage_DispatchesByKind(t *testing.T) {
	n, ok := parseTokenUsage("antigravity", divergentAgySample)
	if !ok || n != 777 {
		t.Fatalf("parseTokenUsage(antigravity) = (%d,%v), want (777,true) — "+
			"150 means the antigravity dispatch is gone and tokens.Parse summed input+output", n, ok)
	}
	// Same blob, generic kind: the untouched tokens.Parse path sums input+output
	// (150) and never reads total_tokens. Proves the dispatch is real, not a
	// coincidence of the fixture.
	if generic, gok := parseTokenUsage("claude-code", divergentAgySample); !gok || generic != 150 {
		t.Fatalf("parseTokenUsage(claude-code, same blob) = (%d,%v), want (150,true)", generic, gok)
	}

	// Same-shaped usage object, but for claude-code the generic tokens.Parse path
	// applies (input+output, ignoring a total_tokens field it doesn't look at
	// first) — verifies antigravity's dispatch didn't leak into other kinds.
	claudeOut := `{"usage":{"input_tokens":100,"output_tokens":50}}`
	n, ok = parseTokenUsage("claude-code", claudeOut)
	want, wantOK := tokens.Parse("claude-code", claudeOut)
	if ok != wantOK || n != want {
		t.Fatalf("parseTokenUsage(claude-code) = (%d,%v), want (%d,%v) [unchanged tokens.Parse behavior]", n, ok, want, wantOK)
	}
	if n != 150 {
		t.Fatalf("sanity: claude-code parse = %d, want 150", n)
	}
}

// End-to-end through CmdRunner.Run: an agy stint's stdout records EXACTLY
// usage.total_tokens, not any input+output (or +thinking) recomputation, into the
// repo's token store — the acceptance criterion from the task spec.
//
// The "verbatim" subtest uses divergentAgySample: on realAgySample alone this test
// passed with the antigravity dispatch deleted from parseTokenUsage (total there
// coincidentally equals input+output). The "real shape" subtest keeps the verified
// real object in the end-to-end path so field ordering/extra keys stay covered.
func TestCmdRunner_RecordsTokens_Antigravity_TotalTokensVerbatim(t *testing.T) {
	cases := []struct {
		name string
		task string
		out  string
		want int
	}{
		// 100+50=150 would be the generic-sum answer; only reading total_tokens
		// as-is yields 777.
		{"verbatim total, diverges from sum", "t-agy-diverge", divergentAgySample, 777},
		{"real verified sample", "t-agy-real", realAgySample, 20331},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			dir := t.TempDir()
			r := CmdRunner{Exec: emitExec(tc.out, nil)}
			err := r.Run(context.Background(), LaunchContext{
				Seat: "agy1", Kind: "antigravity", Task: tc.task, Briefing: "go", RepoDir: dir,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := tokens.Load(dir)
			if n := got.Get(tc.task); n != tc.want {
				t.Fatalf("recorded tokens for %s = %d, want exactly %d (usage.total_tokens verbatim)", tc.task, n, tc.want)
			}
			if runs := got.Tasks[tc.task].Runs; runs != 1 {
				t.Fatalf("runs for %s = %d, want 1", tc.task, runs)
			}
		})
	}
}

// Malformed JSON on stdout must not error or block the stint, and must record
// nothing (not a spurious zero entry).
func TestCmdRunner_RecordsTokens_Antigravity_MalformedJSON(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	r := CmdRunner{Exec: emitExec(`{"usage":{"total_tokens":`, nil)}
	err := r.Run(context.Background(), LaunchContext{
		Seat: "agy1", Kind: "antigravity", Task: "t-bad", Briefing: "go", RepoDir: dir,
	})
	if err != nil {
		t.Fatalf("Run should not fail on malformed token JSON: %v", err)
	}
	if n := tokens.Load(dir).Get("t-bad"); n != 0 {
		t.Fatalf("tokens for t-bad = %d, want 0 (malformed usage json)", n)
	}
}

// Missing usage field: run succeeds, records nothing, never panics.
func TestCmdRunner_RecordsTokens_Antigravity_MissingUsage(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	r := CmdRunner{Exec: emitExec(`{"conversation_id":"x","status":"SUCCESS","response":"done"}`, nil)}
	err := r.Run(context.Background(), LaunchContext{
		Seat: "agy1", Kind: "antigravity", Task: "t-nousage", Briefing: "go", RepoDir: dir,
	})
	if err != nil {
		t.Fatalf("Run should not fail on missing usage field: %v", err)
	}
	if n := tokens.Load(dir).Get("t-nousage"); n != 0 {
		t.Fatalf("tokens for t-nousage = %d, want 0 (no usage field)", n)
	}
}

// Empty stdout: run succeeds, records nothing, never panics.
func TestCmdRunner_RecordsTokens_Antigravity_EmptyOutput(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	r := CmdRunner{Exec: emitExec("", nil)}
	err := r.Run(context.Background(), LaunchContext{
		Seat: "agy1", Kind: "antigravity", Task: "t-empty", Briefing: "go", RepoDir: dir,
	})
	if err != nil {
		t.Fatalf("Run should not fail on empty stdout: %v", err)
	}
	if n := tokens.Load(dir).Get("t-empty"); n != 0 {
		t.Fatalf("tokens for t-empty = %d, want 0 (empty output)", n)
	}
	if len(tokens.Load(dir).Tasks) != 0 {
		t.Fatalf("token store should be empty, got %d entries", len(tokens.Load(dir).Tasks))
	}
}

// Two antigravity stints on the same task accumulate, matching the existing
// claude-code accumulation behavior (TestCmdRunner_RecordsTokens_AccumulatesAcrossRuns).
func TestCmdRunner_RecordsTokens_Antigravity_AccumulatesAcrossRuns(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()
	lc := LaunchContext{Seat: "agy1", Kind: "antigravity", Task: "t-acc", Briefing: "go", RepoDir: dir}

	r1 := CmdRunner{Exec: emitExec(`{"usage":{"total_tokens":100}}`, nil)}
	if err := r1.Run(context.Background(), lc); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	r2 := CmdRunner{Exec: emitExec(`{"usage":{"total_tokens":250}}`, nil)}
	if err := r2.Run(context.Background(), lc); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if got := tokens.Load(dir).Get("t-acc"); got != 350 {
		t.Fatalf("accumulated tokens for t-acc = %d, want 350", got)
	}
	if runs := tokens.Load(dir).Tasks["t-acc"].Runs; runs != 2 {
		t.Fatalf("runs for t-acc = %d, want 2", runs)
	}
}
