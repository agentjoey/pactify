package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/finish"
)

// OrchestrateStatusDTO wraps the orchestrate runtime snapshot. Present is false
// when the orchestrate driver has never run for this project (no status file).
type OrchestrateStatusDTO struct {
	Present bool            `json:"present"`
	Status  json.RawMessage `json:"status,omitempty"`
}

func orchestrateStatusPath(dir string) string {
	return filepath.Join(dir, ".pact", "orchestrate", "status.json")
}

func (s *Server) SetExecOrchestrate(fn func(dir string, args, env []string) error) {
	s.execOrchestrate = fn
}

func (s *Server) SetFinishRunner(fn func(dir, name string, args ...string) (string, error)) {
	s.finishRunner = fn
}

func (s *Server) SetGitDiff(fn func(dir string, staged bool) (string, error)) {
	s.gitDiffFn = fn
}

func (s *Server) registerOrchestrateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/orchestrate/status", s.handleOrchestrateStatus)
	mux.HandleFunc("GET /api/projects/{id}/orchestrate/parallel", s.handleOrchestrateParallel)
	mux.HandleFunc("POST /api/projects/{id}/orchestrate/run", s.handleOrchestrateRun)
	mux.HandleFunc("POST /api/projects/{id}/orchestrate/resume", s.handleOrchestrateResume)
	mux.HandleFunc("POST /api/projects/{id}/orchestrate/ship", s.handleOrchestrateShip)
	mux.HandleFunc("GET /api/projects/{id}/orchestrate/diff", s.handleOrchestrateDiff)
}

// ParallelStatusDTO aggregates the per-feature status files a parallel
// orchestrate run writes (one JSON per concurrent feature). Present is false when
// no parallel run has happened (no parallel dir). Features is sorted by id for a
// stable dashboard order.
type ParallelStatusDTO struct {
	Present  bool              `json:"present"`
	Features []json.RawMessage `json:"features"`
}

// handleOrchestrateParallel reads .pact/orchestrate/parallel/*.json and returns
// the per-feature statuses as a list, so the dashboard can show several features
// advancing at once (the single status.json only holds the serial run).
func (s *Server) handleOrchestrateParallel(w http.ResponseWriter, r *http.Request) {
	s.pmu.RLock()
	p, ok := s.projects[r.PathValue("id")]
	s.pmu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}

	dir := filepath.Join(p.Path, ".pact", "orchestrate", "parallel")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, ParallelStatusDTO{Present: false})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // stable order by feature id

	dto := ParallelStatusDTO{Present: true, Features: make([]json.RawMessage, 0, len(names))}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil || !json.Valid(b) {
			continue // skip a transient half-written file
		}
		dto.Features = append(dto.Features, b)
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleOrchestrateStatus(w http.ResponseWriter, r *http.Request) {
	s.pmu.RLock()
	p, ok := s.projects[r.PathValue("id")]
	s.pmu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}

	path := orchestrateStatusPath(p.Path)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, OrchestrateStatusDTO{Present: false})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !json.Valid(b) {
		writeErr(w, http.StatusInternalServerError, "invalid status file")
		return
	}

	writeJSON(w, http.StatusOK, OrchestrateStatusDTO{
		Present: true,
		Status:  b,
	})
}

type orchestrateRunReq struct {
	Feature        string `json:"feature"`
	MaxConcurrency int    `json:"max_concurrency"`
}

func (s *Server) orchestrateRunning(dir string) bool {
	path := orchestrateStatusPath(dir)
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var st struct {
		Done      bool `json:"done"`
		Escalated bool `json:"escalated"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	return !st.Done && !st.Escalated
}

func (s *Server) defaultExecOrchestrate(dir string, args, env []string) error {
	bin, err := exec.LookPath("pactify")
	if err != nil {
		return fmt.Errorf("pactify not in PATH: %w", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	return cmd.Start()
}

func (s *Server) handleOrchestrateRunOrResume(w http.ResponseWriter, r *http.Request, resume bool) {
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

	if s.orchestrateRunning(dir) {
		writeErr(w, http.StatusConflict, "orchestrate is already running")
		return
	}

	var req orchestrateRunReq
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	args := []string{"orchestrate", "--as", s.seat}
	if req.Feature != "" {
		args = append(args, "--feature", req.Feature)
	}
	if req.MaxConcurrency > 0 {
		args = append(args, "--max-concurrency", fmt.Sprintf("%d", req.MaxConcurrency))
	}
	if resume {
		args = append(args, "--resume")
	}
	env := []string{"PACT_AGENT_ID=" + s.seat}

	execFn := s.execOrchestrate
	if execFn == nil {
		execFn = s.defaultExecOrchestrate
	}
	if err := execFn(dir, args, env); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status_url": "/api/projects/" + id + "/orchestrate/status",
	})
}

func (s *Server) handleOrchestrateRun(w http.ResponseWriter, r *http.Request) {
	s.handleOrchestrateRunOrResume(w, r, false)
}

func (s *Server) handleOrchestrateResume(w http.ResponseWriter, r *http.Request) {
	s.handleOrchestrateRunOrResume(w, r, true)
}

type shipReq struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	PR     bool   `json:"pr"`
	Head   string `json:"head"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func (s *Server) handleOrchestrateShip(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	if _, err := s.actingProject(dir); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	var req shipReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	runner := s.finishRunner
	if runner == nil {
		runner = func(d, name string, args ...string) (string, error) {
			cmd := exec.Command(name, args...)
			cmd.Dir = d
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
	}
	f := finish.Finisher{
		Run:   runner,
		HasGH: func() bool { _, err := exec.LookPath("gh"); return err == nil },
	}

	if req.Remote == "" {
		req.Remote = "origin"
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	if req.PR {
		out, err := f.OpenPR(dir, req.Branch, req.Head, req.Title, req.Body)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		prURL := strings.TrimSpace(out)
		writeJSON(w, http.StatusOK, map[string]any{"pushed": true, "pr_url": prURL})
		return
	}

	if _, err := f.Push(dir, req.Remote, req.Branch); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pushed": true})
}

func defaultGitDiff(dir string, staged bool) (string, error) {
	args := []string{"-C", dir, "diff"}
	if staged {
		args = append(args, "--staged")
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}

func (s *Server) handleOrchestrateDiff(w http.ResponseWriter, r *http.Request) {
	_, dir, ok := s.project(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}

	staged := r.URL.Query().Has("staged")

	diffFn := s.gitDiffFn
	if diffFn == nil {
		diffFn = defaultGitDiff
	}

	out, err := diffFn(dir, staged)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": out})
}
