package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/doctor"
)

func TestCheckJSONTags(t *testing.T) {
	checks := []doctor.Check{
		{Name: "good check", OK: true, Detail: "all fine"},
		{Name: "bad check", OK: false, Detail: "something wrong"},
	}
	b, err := json.Marshal(checks)
	if err != nil {
		t.Fatalf("marshal checks: %v", err)
	}
	s := string(b)
	for _, key := range []string{"name", "ok", "detail"} {
		if !strings.Contains(s, `"`+key+`"`) {
			t.Errorf("JSON missing key %q: %s", key, s)
		}
	}
	if !strings.Contains(s, `"ok":true`) {
		t.Errorf("expected OK=true serialized as \"ok\":true, got %s", s)
	}
	if !strings.Contains(s, `"ok":false`) {
		t.Errorf("expected OK=false serialized as \"ok\":false, got %s", s)
	}
}

func TestDoctorJSONOutput(t *testing.T) {
	cmd := newDoctorCmd()
	cmd.SetArgs([]string{"--json"})
	cmd.SilenceUsage = true
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	// The command may return an error depending on the test environment,
	// but stdout should still contain a valid JSON array.
	_ = cmd.Execute()

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		t.Fatal("doctor --json produced no stdout output")
	}
	var checks []doctor.Check
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		t.Fatalf("doctor --json stdout is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(checks) == 0 {
		t.Fatal("doctor --json returned empty checks array")
	}
	for _, ck := range checks {
		if ck.Name == "" {
			t.Errorf("decoded check missing Name: %+v", ck)
		}
	}
}
