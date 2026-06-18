package serve

import (
	"encoding/json"
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
	return cmd.Run()
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
	if strings.TrimSpace(req.Goal) == "" {
		writeErr(w, http.StatusBadRequest, "goal must not be empty")
		return
	}
	if !planner.ValidSlug(req.Feature) {
		writeErr(w, http.StatusBadRequest, "feature must be a kebab-case slug")
		return
	}
	kind := req.PlannerKind
	if kind == "" {
		if st, err := stateProjection(dir); err == nil {
			kind = orchestratorKind(st)
		}
	}
	if kind == "" {
		kind = "claude-code"
	}
	if err := writePlanGenStatus(dir, PlanGenStatusDTO{State: "running", Feature: req.Feature}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	runFn := s.runPlanner
	if runFn == nil {
		runFn = s.defaultRunPlanner
	}
	args := []string{"plan", req.Goal, "--feature", req.Feature, "--planner-kind", kind}
	env := []string{"PACT_AGENT_ID=" + s.seat}
	feature := req.Feature
	go func() {
		err := runFn(dir, args, env)
		done := PlanGenStatusDTO{Feature: feature, State: "done"}
		if err != nil {
			done.State = "error"
			done.Error = err.Error()
		} else if perr := manifestParses(dir, feature); perr != nil {
			done.State = "error"
			done.Error = "planner produced no valid manifest: " + perr.Error()
		}
		_ = writePlanGenStatus(dir, done)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status_url": "/api/projects/" + id + "/plan/generate/status",
		"feature":    feature,
	})
}
