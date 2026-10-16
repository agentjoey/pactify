package sub

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

var doctorCheckNames = []string{"Installed", "Write access", "Model", "Read-only access", "Read-only hook", "Network", "Sandbox", "Failure classification", "Session hiding", "Managed home"}

// Complete the same contract on preflight failures as on successful probes.
func alignDoctorChecks(r DoctorReport) DoctorReport {
	checks := map[string]DoctorCheck{}
	for _, c := range r.Checks {
		checks[c.Name] = c
	}
	if _, ok := checks["Read-only hook"]; !ok {
		checks["Read-only hook"] = DoctorCheck{"Read-only hook", "SKIP", "not applicable: only agy installs a read-only hook"}
	}
	sandbox := DoctorCheck{"Sandbox", "WARN", "not sandboxed: writes are not confined by the OS"}
	if r.Harness == "codex" {
		sandbox.Status = "PASS"
		sandbox.Reason = "sandboxed: native read-only / workspace-write OS sandbox"
	}
	checks[sandbox.Name] = sandbox
	checks["Failure classification"] = doctorFailureClassification(r.Harness)
	if _, ok := checks["Managed home"]; !ok {
		if r.Harness == "codex" {
			checks["Managed home"] = DoctorCheck{"Managed home", "PASS", "native per-process config overrides disable optional integrations; no separate HOME required"}
		} else {
			checks["Managed home"] = DoctorCheck{"Managed home", "SKIP", "not applicable: no managed home configured"}
		}
	}
	r.Checks = nil
	for _, name := range doctorCheckNames {
		c, ok := checks[name]
		if !ok {
			c = DoctorCheck{name, "SKIP", "preflight failed; not run"}
		}
		r.Checks = append(r.Checks, c)
	}
	return r
}

// Inject an offline protocol failure directly at the classification boundary.
// This tests the same adapter used by retries/fallback, without launching a CLI,
// opening credentials, sending a prompt, or making a billable network request.
func doctorFailureClassification(name string) DoctorCheck {
	message, raw, want := "", json.RawMessage(nil), "transient"
	switch name {
	case "codex":
		raw = json.RawMessage(`{"error":{"codexErrorInfo":"internalServerError"}}`)
	case "opencode":
		raw = json.RawMessage(`{"name":"APIError","data":{"statusCode":503,"isRetryable":true}}`)
	case "kimi":
		raw = json.RawMessage(`{"error":{"code":"provider.connection_error","message":"Connection error."}}`)
	case "agy":
		message = "UNAVAILABLE (code 503)"
	default:
		return DoctorCheck{"Failure classification", "SKIP", "not applicable: test harness has no vendor classifier"}
	}
	got := cockpit.ClassifyFailure(name, message, raw, time.Now()).Class
	status := "PASS"
	if got != want {
		status = "FAIL"
	}
	return DoctorCheck{"Failure classification", status, fmt.Sprintf("offline protocol error injection: expected=%s, actual=%s; no CLI or network call", want, got)}
}
