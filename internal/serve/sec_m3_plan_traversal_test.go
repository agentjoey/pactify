package serve

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentjoey/pactify/internal/registry"
)

// Security regression — review finding M3 (medium, path traversal).
//
// handlePlanReview takes {feature} straight from the path and does
// filepath.Join(p.Path, ".pact", "plan-"+feature+".json") + os.ReadFile with no
// validation (unlike startPlanGenerate, which enforces planner.ValidSlug). A
// %2F-encoded traversal in the segment (decoded into PathValue after routing)
// climbs out of .pact/ to read an arbitrary <path>.json — a file existence/type
// oracle and content leak, reachable via DNS-rebind. The handler must reject a
// feature that is not a valid kebab slug.
//
// RED until handlePlanReview validates {feature} with planner.ValidSlug.
func TestSEC_M3_PlanReviewRejectsInvalidFeature(t *testing.T) {
	dir := t.TempDir()
	srv := New([]registry.Project{{Name: "p", Path: dir}})
	h := srv.Handler()

	for _, bad := range []string{
		"..%2f..%2f..%2fsecret", // encoded traversal
		"..%2fescape",
		"plan.%2e%2e",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/projects/p/plan/"+bad, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("M3: non-slug feature %q returned %d, want 400 (unvalidated path read)", bad, rr.Code)
		}
	}

	// A legit slug must still work — a missing manifest is a normal present:false 200.
	req := httptest.NewRequest(http.MethodGet, "/api/projects/p/plan/add-2fa", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("legit feature rejected: %d %s", rr.Code, rr.Body.String())
	}
}
