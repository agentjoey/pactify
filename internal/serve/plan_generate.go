package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/planner"
	"github.com/agentjoey/pactify/internal/projection"
)

// PlanGenStatusDTO is the single-in-flight plan-generation status for a project.
// state ∈ idle | running | done | error.
type PlanGenStatusDTO struct {
	State   string `json:"state"`
	Feature string `json:"feature,omitempty"`
	Error   string `json:"error,omitempty"`
}

func planGenStatusPath(dir string) string {
	return filepath.Join(dir, ".pact", "plan-gen", "status.json")
}

func readPlanGenStatus(dir string) PlanGenStatusDTO {
	b, err := os.ReadFile(planGenStatusPath(dir))
	if err != nil {
		return PlanGenStatusDTO{State: "idle"}
	}
	var dto PlanGenStatusDTO
	if json.Unmarshal(b, &dto) != nil || dto.State == "" {
		return PlanGenStatusDTO{State: "idle"}
	}
	return dto
}

func writePlanGenStatus(dir string, dto PlanGenStatusDTO) error {
	p := planGenStatusPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(dto)
	return os.WriteFile(p, b, 0o644)
}

// orchestratorKind returns the agent kind of the first seat holding the
// "orchestrator" role, or "" when none/kindless.
func orchestratorKind(st projection.State) string {
	for _, a := range st.Agents {
		for _, r := range a.Roles {
			if r == "orchestrator" {
				return a.Kind
			}
		}
	}
	return ""
}

// manifestParses reports whether .pact/plan-<feature>.json exists and parses.
func manifestParses(dir, feature string) error {
	b, err := os.ReadFile(filepath.Join(dir, ".pact", "plan-"+feature+".json"))
	if err != nil {
		return err
	}
	_, err = planner.Parse(b)
	return err
}

// defaultRunPlanner shells out to `pactify plan` and WAITS (caller runs it in a
// goroutine). `pactify plan` builds the prompt, launches the planner agent, and
// writes .pact/plan-<feature>.json.
func (s *Server) defaultRunPlanner(dir string, args, env []string) error {
	bin, err := exec.LookPath("pactify")
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return wrapRunErr(err, out)
	}
	return nil
}

// wrapRunErr appends the tail of a failed subprocess's combined output to its
// error, so the planner's real failure (e.g. `exec: "claude": not found`)
// surfaces in plan-gen/status instead of a bare "exit status 1".
func wrapRunErr(err error, out []byte) error {
	tail := strings.TrimSpace(string(out))
	const max = 600
	if len(tail) > max {
		tail = "…" + tail[len(tail)-max:]
	}
	if tail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, tail)
}

func stateProjection(dir string) (projection.State, error) {
	return pact.At(dir).StateProjection()
}

func (s *Server) handlePlanGenStatus(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	writeJSON(w, http.StatusOK, readPlanGenStatus(dir))
}

type planGenerateReq struct {
	Goal        string `json:"goal"`
	Feature     string `json:"feature"`
	PlannerKind string `json:"planner_kind"`
}

func (s *Server) handlePlanGenerate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, dir, ok := s.project(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	if _, err := s.actingProject(dir); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if readPlanGenStatus(dir).State == "running" {
		writeErr(w, http.StatusConflict, "a plan is already generating")
		return
	}
	var req planGenerateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// The HTTP dashboard drives generate → review → apply as separate steps.
	if code, err := s.startPlanGenerate(id, dir, req.Goal, req.Feature, req.PlannerKind, false); err != nil {
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status_url": "/api/projects/" + id + "/plan/generate/status",
		"feature":    req.Feature,
	})
}

// startPlanGenerate validates + launches the planner subprocess (async), shared
// by the dashboard's HTTP endpoint and the remote plan.generate rpc. Returns an
// HTTP status code with the error for the HTTP caller; the goroutine records
// done/error in plan-gen status. When applyAfter is set, a successfully
// generated + valid manifest is applied in the same goroutine — the one-shot
// path for hosted/relay callers that can't drive an interactive review loop over
// the relay (the resulting assigns surface on the board via the event stream).
func (s *Server) startPlanGenerate(id, dir, goal, feature, plannerKind string, applyAfter bool) (int, error) {
	if readPlanGenStatus(dir).State == "running" {
		return http.StatusConflict, fmt.Errorf("a plan is already generating")
	}
	if strings.TrimSpace(goal) == "" {
		return http.StatusBadRequest, fmt.Errorf("goal must not be empty")
	}
	if !planner.ValidSlug(feature) {
		return http.StatusBadRequest, fmt.Errorf("feature must be a kebab-case slug")
	}
	kind := plannerKind
	if kind == "" {
		if st, err := stateProjection(dir); err == nil {
			kind = orchestratorKind(st)
		}
	}
	if kind == "" {
		kind = "claude-code"
	}
	if err := writePlanGenStatus(dir, PlanGenStatusDTO{State: "running", Feature: feature}); err != nil {
		return http.StatusInternalServerError, err
	}
	runFn := s.runPlanner
	if runFn == nil {
		runFn = s.defaultRunPlanner
	}
	args := []string{"plan", goal, "--feature", feature, "--planner-kind", kind}
	env := []string{"PACT_AGENT_ID=" + s.seat}
	go func() {
		err := runFn(dir, args, env)
		done := PlanGenStatusDTO{Feature: feature, State: "done"}
		if err != nil {
			done.State = "error"
			done.Error = err.Error()
		} else if perr := manifestParses(dir, feature); perr != nil {
			done.State = "error"
			done.Error = "planner produced no valid manifest: " + perr.Error()
		} else if applyAfter {
			if _, _, aerr := s.applyPlanManifest(id, dir, feature); aerr != nil {
				done.State = "error"
				done.Error = "generated but failed to apply: " + aerr.Error()
			}
		}
		_ = writePlanGenStatus(dir, done)
	}()
	return 0, nil
}
