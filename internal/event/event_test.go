package event

import "testing"

func TestRoleForDerivesFromVerb(t *testing.T) {
	cases := map[string]string{
		"init": "orchestrator", "assign": "orchestrator", "merge": "orchestrator",
		"cancel": "orchestrator", "withdraw": "orchestrator", "rebaseline": "orchestrator",
		"config_gate": "orchestrator", "config_critic": "orchestrator", "start": "orchestrator",
		"escalate": "orchestrator",
		"join": "worker", "checkpoint": "worker",
		"accept": "reviewer", "changes_requested": "reviewer",
	}
	for et, want := range cases {
		if got := RoleFor(et); got != want {
			t.Errorf("RoleFor(%q) = %q, want %q", et, got, want)
		}
	}
}

func TestNewEventIDIsNonEmptyAndUnique(t *testing.T) {
	a, b := NewEventID(), NewEventID()
	if len(a) == 0 || a == b {
		t.Fatalf("event ids not unique/non-empty: %q %q", a, b)
	}
}

func TestNowUTCFormat(t *testing.T) {
	ts := NowUTC()
	if len(ts) != 20 || ts[len(ts)-1] != 'Z' || ts[4] != '-' {
		t.Fatalf("NowUTC() = %q, not ISO-8601 UTC", ts)
	}
}
