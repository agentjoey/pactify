package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentjoey/pactify/internal/audit"
)

func TestHandleAuditList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	_ = audit.Append(audit.Record{Project: "demo", TS: "2026-06-16T01:00:00Z", Seat: "dev", Task: "t1", Tool: "bash", Risk: "exec"})
	_ = audit.Append(audit.Record{Project: "demo", TS: "2026-06-16T02:00:00Z", Seat: "rev", Task: "t1", Tool: "fs.read", Risk: "read"})

	s := &Server{}
	// filter seat=dev → 1 record
	r := httptest.NewRequest("GET", "/api/projects/demo/audit?seat=dev", nil)
	r.SetPathValue("id", "demo")
	w := httptest.NewRecorder()
	s.handleAudit(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var recs []audit.Record
	json.NewDecoder(w.Body).Decode(&recs)
	if len(recs) != 1 || recs[0].Tool != "bash" {
		t.Fatalf("resp = %+v", recs)
	}
}

func TestHandleAuditEmptyIsArray(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	s := &Server{}
	r := httptest.NewRequest("GET", "/api/projects/none/audit", nil)
	r.SetPathValue("id", "none")
	w := httptest.NewRecorder()
	s.handleAudit(w, r)
	if w.Body.String() != "[]\n" && w.Body.String() != "[]" {
		t.Fatalf("empty audit should be [], got %q", w.Body.String())
	}
}
