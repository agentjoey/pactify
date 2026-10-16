package cockpit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKimiP0AllAgents(t *testing.T) {
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, "{\"type\":\"llm.request\",\"model\":\"main-model\"}\n{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":10,\"output\":2}}\n")
	dir, _ := kimiSessionDir(home, "session_abc")
	os.MkdirAll(filepath.Join(dir, "agents", "agent-0"), 0700)
	mustWriteKimi(t, filepath.Join(dir, "agents", "agent-0", "wire.jsonl"), "{\"type\":\"llm.request\",\"model\":\"child-model\"}\n{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":20,\"output\":3}}\n")
	r, err := ReadKimiRecord(home, "session_abc")
	if err != nil || r.InputTokens != 30 || r.OutputTokens != 5 || len(r.Models) != 1 || r.Models[0] != "main-model" {
		t.Fatalf("record=%+v err=%v", r, err)
	}
}

func TestKimiP0FailureCodes(t *testing.T) {
	for _, tc := range []struct{ code, message, class string }{
		{"provider.rate_limit", "subscription quota exhausted; resets at 2033-05-18T03:33:20Z", "quota"},
		{"provider.rate_limit", "too many requests, retry shortly", "transient"},
		{"provider.connection_error", "connection failed", "transient"},
		{"provider.overloaded", "busy", "transient"},
		{"provider.auth_error", "invalid credentials", "auth"},
		{"provider.filtered", "content filtered", "refusal"},
		{"context.overflow", "context exceeded", "other"},
		{"provider.api_error", "HTTP 503", "transient"},
		{"provider.timeout", "timed out", "transient"},
	} {
		t.Run(tc.code+tc.class, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"type": "turn.ended", "reason": "failed", "error": map[string]string{"code": tc.code, "message": tc.message}})
			home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, string(raw)+"\n")
			record, err := ReadKimiRecordSince(home, "session_abc", 0)
			if err != nil || record.Error == nil || record.Reason != "failed" {
				t.Fatalf("%+v %v", record, err)
			}
			got := ClassifyFailure("kimi", "opaque", raw, time.Now())
			if got.Class != tc.class {
				t.Fatalf("%+v want %s", got, tc.class)
			}
			if tc.class == "quota" && got.ResetAt == nil {
				t.Fatal("missing reset")
			}
		})
	}
}

func TestKimiP0OffsetsAndLatestOutcome(t *testing.T) {
	main := "{\"type\":\"llm.request\",\"model\":\"m\"}\n{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":10,\"output\":2}}\n{\"type\":\"turn.ended\",\"reason\":\"failed\",\"error\":{\"code\":\"provider.connection_error\",\"message\":\"lost\"}}\n"
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, main)
	dir, _ := kimiSessionDir(home, "session_abc")
	child := filepath.Join(dir, "agents", "agent-0", "wire.jsonl")
	os.MkdirAll(filepath.Dir(child), 0700)
	mustWriteKimi(t, child, main)
	offsets, err := KimiWireOffsets(home, "session_abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(offsets) != 2 {
		t.Fatal(offsets)
	}
	appendFile := func(path, s string) {
		t.Helper()
		f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if _, e = f.WriteString(s); e != nil {
			t.Fatal(e)
		}
	}
	appendFile(filepath.Join(dir, "agents", "main", "wire.jsonl"), "{\"type\":\"llm.request\",\"model\":\"resumed\"}\n{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":3,\"output\":1}}\n{\"type\":\"turn.ended\",\"reason\":\"completed\"}\n")
	appendFile(child, "{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":4,\"output\":2}}\n")
	newChild := filepath.Join(dir, "agents", "agent-1", "wire.jsonl")
	os.MkdirAll(filepath.Dir(newChild), 0700)
	mustWriteKimi(t, newChild, "{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":5,\"output\":3}}\n")
	got, err := ReadKimiRecordOffsets(home, "session_abc", offsets)
	if err != nil || got.InputTokens != 12 || got.OutputTokens != 6 || got.Error != nil || len(got.Models) != 1 || got.Models[0] != "resumed" || got.EndedTurns != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	// The scalar API remains usable for old callers with a main-only baseline.
	legacy, err := ReadKimiRecordSince(home, "session_abc", offsets["main"])
	if err != nil || legacy.Models[0] != "resumed" {
		t.Fatal(legacy, err)
	}
	all, _ := ReadKimiRecord(home, "session_abc")
	if all.Error != nil {
		t.Fatal("old failures survived successful retry", all)
	}
}

func TestKimiP0RecordedConnectionFailure(t *testing.T) {
	data, err := os.ReadFile("testdata/kimi/connection-error.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, string(data))
	r, err := ReadKimiRecordSince(home, "session_abc", 0)
	if err != nil || r.Reason != "failed" || r.Error == nil || r.Error.Code != "provider.connection_error" || r.Error.Message != "Connection error." {
		t.Fatal(r, err)
	}
}

func TestKimiP0TypedCodePrecedesMessage(t *testing.T) {
	for _, tc := range []struct{ code, message, class string }{
		{"provider.auth_error", "quota request authentication failed", "auth"},
		{"provider.filtered", "quota content filtered", "refusal"},
		{"provider.connection_error", "authentication endpoint unreachable", "transient"},
	} {
		raw, _ := json.Marshal(map[string]any{"error": KimiWireError{Code: tc.code, Message: tc.message}})
		if got := ClassifyFailure("kimi", "", raw, time.Now()); got.Class != tc.class {
			t.Errorf("%s: %s want %s", tc.code, got.Class, tc.class)
		}
	}
}

func TestKimiP0StructuredHTTPFailure(t *testing.T) {
	raw := json.RawMessage(`{"type":"turn.ended","reason":"failed","error":{"code":"provider.api_error","message":"upstream error","details":{"statusCode":503}}}`)
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, string(raw)+"\n")
	record, err := ReadKimiRecord(home, "session_abc")
	if err != nil {
		t.Fatal(err)
	}
	// The worker carries the parsed payload to classification, rather than rereading raw JSON.
	roundTrip, _ := json.Marshal(map[string]any{"error": record.Error})
	if got := ClassifyFailure("kimi", "", roundTrip, time.Now()); got.Class != "transient" {
		t.Fatal(got)
	}
}
