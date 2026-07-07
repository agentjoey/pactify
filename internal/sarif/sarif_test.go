package sarif

import (
	"encoding/json"
	"testing"
)

func TestBuildMinimalLog(t *testing.T) {
	log := Build("pactify", "v0.1.0", []Finding{
		{RuleID: "pact.audit.exec", Level: "warning", Message: "go build", Seat: "dev", Task: "t1", Project: "demo", TS: "2026-06-16T01:00:00Z"},
	})

	if log.Version != "2.1.0" {
		t.Fatalf("version = %q, want 2.1.0", log.Version)
	}
	if log.Schema != "https://json.schemastore.org/sarif-2.1.0.json" {
		t.Fatalf("schema = %q", log.Schema)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	driver := log.Runs[0].Tool.Driver
	if driver.Name != "pactify" {
		t.Fatalf("driver.name = %q, want pactify", driver.Name)
	}
	if len(driver.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(driver.Rules))
	}
	if driver.Rules[0].ID != "pact.audit.exec" {
		t.Fatalf("rule id = %q", driver.Rules[0].ID)
	}
	if len(log.Runs[0].Results) != 1 {
		t.Fatalf("results = %d, want 1", len(log.Runs[0].Results))
	}
	res := log.Runs[0].Results[0]
	if res.Level != "warning" {
		t.Fatalf("result level = %q, want warning", res.Level)
	}
	if res.Properties.Seat != "dev" {
		t.Fatalf("properties.seat = %q", res.Properties.Seat)
	}
}

func TestBuildDeduplicatesRules(t *testing.T) {
	log := Build("pactify", "", []Finding{
		{RuleID: "pact.audit.exec", Level: "warning", Message: "a"},
		{RuleID: "pact.audit.exec", Level: "warning", Message: "b"},
		{RuleID: "pact.audit.read", Level: "note", Message: "c"},
	})

	if len(log.Runs[0].Tool.Driver.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(log.Runs[0].Tool.Driver.Rules))
	}
	if len(log.Runs[0].Results) != 3 {
		t.Fatalf("results = %d, want 3", len(log.Runs[0].Results))
	}
}

func TestBuildNormalizesInvalidLevel(t *testing.T) {
	log := Build("pactify", "", []Finding{
		{RuleID: "r", Level: "", Message: "empty"},
		{RuleID: "r", Level: "critical", Message: "invalid"},
	})

	for i, res := range log.Runs[0].Results {
		if res.Level != "note" {
			t.Fatalf("result[%d].level = %q, want note", i, res.Level)
		}
	}
}

func TestBuildMarshalContainsSchemaAndRuns(t *testing.T) {
	log := Build("pactify", "v0.1.0", []Finding{
		{RuleID: "pact.audit.write", Level: "note", Message: "x"},
	})
	buf, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["$schema"]; !ok {
		t.Fatalf("missing $schema key")
	}
	if _, ok := m["runs"]; !ok {
		t.Fatalf("missing runs key")
	}
}
