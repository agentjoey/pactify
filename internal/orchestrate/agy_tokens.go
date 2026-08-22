package orchestrate

import (
	"encoding/json"
	"strconv"
	"strings"
)

// agyUsageShape matches agy's (`antigravity --output-format json`) headless
// result object:
//
//	{"conversation_id":"...","status":"SUCCESS","response":"...",
//	 "duration_seconds":4.72,"num_turns":1,
//	 "usage":{"input_tokens":20300,"output_tokens":31,"thinking_tokens":27,
//	          "cache_read_tokens":0,"total_tokens":20331}}
//
// (real sample, verified). Both usage.total_tokens (dm-agy-tokens) and
// conversation_id (dm-agy-resume, added 2026-08-22) live on this SAME object,
// so one struct backs both parsers rather than duplicating the JSON shape —
// see parseAntigravityTokens for why total_tokens must not be recomputed from
// the other usage fields, and parseAntigravityConversationID for the resume
// use of ConversationID.
type agyUsageShape struct {
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"`
	Error          string `json:"error"`
	Usage          struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// scanStringField finds `"key":"value"` in a possibly-truncated JSON blob and
// returns value. Used only as a fallback after a real json.Unmarshal has failed:
// agy emits ONE object, so a response larger than the capture window leaves a
// fragment that no JSON parser can accept, yet the individual scalar fields are
// still intact and readable. Deliberately naive — it does not handle escaped
// quotes, which none of the fields it reads (uuid, status enum) can contain.
//
// Returns ("",false) when the key is absent, when the fragment ends mid-value,
// AND when the value is present but EMPTY. That last case is not pedantry: agy
// emits `"conversation_id":""` on an argv-validation failure, and every caller
// here is asking "did I find a usable value", not "was the key present" — an
// earlier version returned ("",true) for it and turned an empty id into a
// recorded resume session.
func scanStringField(s, key string) (string, bool) {
	needle := `"` + key + `":"`
	i := strings.Index(s, needle)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(needle):]
	j := strings.IndexByte(rest, '"')
	if j <= 0 {
		return "", false // truncated mid-value (j<0), or present-but-empty (j==0)
	}
	return rest[:j], true
}

// scanIntField is scanStringField for an unquoted integer, taking the LAST
// occurrence (usage sits at the object's tail, and a nested object could in
// principle repeat the key earlier).
func scanIntField(s, key string) (int, bool) {
	needle := `"` + key + `":`
	i := strings.LastIndex(s, needle)
	if i < 0 {
		return 0, false
	}
	rest := s[i+len(needle):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 || end == len(rest) {
		return 0, false // no digits, or truncated with no delimiter to prove the number ended
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseAntigravityStatus reports agy's result status ("SUCCESS" / "ERROR") from a
// captured stdout HEAD. status is a leading field of agy's single result object,
// which is why the caller passes the head window rather than the tail.
//
// This exists because agy's exit code is NOT a sufficient success signal:
// verified live 2026-08-22, a mid-run tool rejection exits 0 while reporting
// {"status":"ERROR",...}, whereas an argv-validation failure (--model/--effort
// conflict) exits 1 with the same status. Without this check a genuinely failed
// stint is booked as a success.
func parseAntigravityStatus(head string) (string, bool) {
	head = strings.TrimSpace(head)
	if head == "" {
		return "", false
	}
	var u agyUsageShape
	if json.Unmarshal([]byte(head), &u) == nil && u.Status != "" {
		return u.Status, true
	}
	return scanStringField(head, "status")
}

// parseAntigravityError returns agy's error message for a non-SUCCESS result, or
// "" when absent. Read from the TAIL (the error field trails the response), and
// purely for the operator-facing message — never a control signal.
func parseAntigravityError(output string) string {
	var u agyUsageShape
	if json.Unmarshal([]byte(strings.TrimSpace(output)), &u) == nil && u.Error != "" {
		return u.Error
	}
	if v, ok := scanStringField(output, "error"); ok {
		return v
	}
	return "(no error message in captured output)"
}

// parseAntigravityTokens extracts usage.total_tokens from an agy stint's captured
// stdout. It deliberately reads total_tokens AS-IS rather than summing
// input_tokens+output_tokens: in the samples verified so far, the two happen to
// be equal (thinking_tokens/cache_read_tokens are not folded into total_tokens
// there), but agy's usage shape is not a documented/versioned contract pactify
// controls — recomputing from a subset of fields risks silently diverging from
// the real total the moment agy's shape changes (e.g. a future field billed
// separately). Reading total_tokens verbatim is the only choice that can't drift
// out of sync with whatever agy actually charges. See dm-agy-tokens and
// TestParseAntigravityTokens_TotalDivergesFromSum for a case that isolates this
// behavior from a plain input+output sum.
//
// agy emits a single JSON result object (verified, not JSONL like codex/opencode's
// stream-json), but the parse is tolerant of both a compact one-line object and a
// pretty-printed multi-line one, and of leading/trailing noise around it — the
// same tolerance tokens.Parse affords the other kinds. Malformed JSON, an empty
// blob, or a missing/non-positive usage.total_tokens all return (0, false):
// best-effort telemetry, never an error, never a failed stint.
func parseAntigravityTokens(output string) (int, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return 0, false
	}
	// Fast path: the whole (possibly pretty-printed) blob is the single result
	// object — this is agy's actual real-world shape.
	var u agyUsageShape
	if json.Unmarshal([]byte(output), &u) == nil && u.Usage.TotalTokens > 0 {
		return u.Usage.TotalTokens, true
	}
	// Fallback: tolerate extra noise around the JSON (e.g. a stray log line) by
	// scanning individual lines for one that parses, taking the LAST usage-bearing
	// line — mirrors tokens.Parse's JSONL tolerance for the other kinds.
	best, ok := 0, false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var lu agyUsageShape
		if json.Unmarshal([]byte(line), &lu) != nil {
			continue
		}
		if lu.Usage.TotalTokens > 0 {
			best, ok = lu.Usage.TotalTokens, true
		}
	}
	if ok {
		return best, true
	}
	// Last resort: the capture window truncated the object so no parser can accept
	// it. total_tokens sits at the object's tail, so it survives in a tail window
	// even when the opening brace is gone — read it as a bare field.
	if n, found := scanIntField(output, "total_tokens"); found && n > 0 {
		return n, true
	}
	return 0, false
}

// parseAntigravityConversationID extracts conversation_id from an agy stint's
// captured stdout (agy-resume, dm-agy-resume). Mirrors parseAntigravityTokens's
// tolerance exactly (single-object fast path, then a noise-tolerant line scan
// taking the LAST id-bearing line) since both read the same result object —
// see agyUsageShape's doc comment. Live-verified 2026-08-22: agy ALWAYS
// populates conversation_id on a SUCCESS result, whether the run cold-started,
// genuinely resumed, or was handed a stale/unknown --conversation id (agy
// prints a `warning: conversation "<id>" not found` line to STDERR — invisible
// to this stdout-only capture — and transparently mints a fresh conversation,
// still exiting 0/SUCCESS). Re-recording whatever id THIS stint actually
// reports is therefore sufficient to self-heal a silently-rejected resume: the
// stale id in the store gets overwritten by the fresh one agy chose, with no
// special-case rejection detection needed.
//
// CORRECTION (re-verified 2026-08-22): an earlier version of this comment claimed
// "an ERROR-status result carries conversation_id:\"\"". That holds ONLY for an
// argv-validation failure (bad --model), which fails before any conversation
// exists. A mid-run failure carries a POPULATED conversation_id alongside
// status=ERROR — observed directly in a live e2e stint. So this function must
// not be read as an ERROR filter; it isn't one. Rejecting failed stints is
// parseAntigravityStatus's job, and the caller checks it BEFORE recording.
//
// Malformed JSON or an empty blob return ("", false): best-effort telemetry,
// never an error, never a failed stint.
func parseAntigravityConversationID(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", false
	}
	var u agyUsageShape
	if json.Unmarshal([]byte(output), &u) == nil && u.ConversationID != "" {
		return u.ConversationID, true
	}
	best, ok := "", false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var lu agyUsageShape
		if json.Unmarshal([]byte(line), &lu) != nil {
			continue
		}
		if lu.ConversationID != "" {
			best, ok = lu.ConversationID, true
		}
	}
	if ok {
		return best, true
	}
	// Last resort, mirroring parseAntigravityTokens: once the result object
	// outgrows the capture window no parser can accept the fragment, but
	// conversation_id is a leading field so it survives intact in a HEAD window.
	// Without this the head capture would be pointless for exactly the case it
	// was added for — a >8 KiB result, where resume would otherwise never engage.
	return scanStringField(output, "conversation_id")
}
