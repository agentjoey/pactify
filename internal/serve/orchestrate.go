package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/event"
	"github.com/agentjoey/pactify/internal/finish"
	"github.com/agentjoey/pactify/internal/gitx"
	"github.com/agentjoey/pactify/internal/pact"
	"github.com/agentjoey/pactify/internal/schedule"
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

// SetPlannerRunner overrides how plan generate launches the planner (tests stub
// it). Default shells out to `pactify plan` and waits.
func (s *Server) SetPlannerRunner(fn func(dir string, args, env []string) error) {
	s.runPlanner = fn
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
	mux.HandleFunc("GET /api/projects/{id}/fallback-proposal", s.handleFallbackProposal)
	mux.HandleFunc("POST /api/projects/{id}/fallback-proposal/approve", s.handleFallbackApprove)
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
	Feature        string            `json:"feature"`
	MaxConcurrency int               `json:"max_concurrency"`
	SeatKinds      map[string]string `json:"seat_kinds"`
}

func seatKindsFromInit(dir string) map[string]string {
	evs, err := event.ReadAll(logPath(dir))
	if err != nil {
		return nil
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EventType != "init" {
			continue
		}
		seats, ok := evs[i].Payload["seats"].([]any)
		if !ok {
			break
		}
		out := map[string]string{}
		for _, s := range seats {
			m, _ := s.(map[string]any)
			id, _ := m["id"].(string)
			kind, _ := m["kind"].(string)
			if id != "" && kind != "" {
				out[id] = kind
			}
		}
		if len(out) > 0 {
			return out
		}
		break
	}
	return nil
}

// orchRunning tracks project dirs with a driver spawned by this process that
// has not exited yet. The status.json check alone cannot serialize rapid POSTs:
// the child takes seconds before its first status write (and on a first run the
// file doesn't exist at all), so two quick clicks would both pass the file check
// and spawn two drivers against the same ledger/branches. Keyed by dir (not a
// Server field) so any two spawners into the same project exclude each other.
var orchRunning = struct {
	sync.Mutex
	dirs map[string]bool
}{dirs: map[string]bool{}}

// orchMarkRunning claims dir for a new driver; false means one is already live.
func orchMarkRunning(dir string) bool {
	orchRunning.Lock()
	defer orchRunning.Unlock()
	if orchRunning.dirs[dir] {
		return false
	}
	orchRunning.dirs[dir] = true
	return true
}

func orchClearRunning(dir string) {
	orchRunning.Lock()
	delete(orchRunning.dirs, dir)
	orchRunning.Unlock()
}

func (s *Server) orchestrateRunning(dir string) bool {
	b, err := os.ReadFile(orchestrateStatusPath(dir))
	if err != nil {
		return false
	}
	var st struct {
		Done      bool   `json:"done"`
		Escalated bool   `json:"escalated"`
		UpdatedAt string `json:"updated_at"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	if st.Done || st.Escalated {
		return false
	}
	// Stale guard: a crashed/abandoned run leaves done=false forever. Treat it as
	// running ONLY if its status updated recently (RFC3339). An old or unparseable
	// timestamp means a dead run that must not block new ones.
	t, err := time.Parse(time.RFC3339, st.UpdatedAt)
	if err != nil {
		return false
	}
	return time.Since(t) < 10*time.Minute
}

// inferKindFromName derives an agent kind from a seat name by stripping a
// trailing role suffix (-worker/-reviewer/-orchestrator) and matching the
// resulting base against the known kind list. It returns the kind if it is an
// exact match or the unique kind that has the base as a prefix; returns "" when
// there is no match or the match is ambiguous.
func inferKindFromName(seat string, known []string) string {
	base := seat
	for _, sfx := range []string{"-worker", "-reviewer", "-orchestrator"} {
		if strings.HasSuffix(seat, sfx) {
			base = strings.TrimSuffix(seat, sfx)
			break
		}
	}
	// exact match first
	for _, k := range known {
		if k == base {
			return k
		}
	}
	// unique prefix match
	var match string
	for _, k := range known {
		if strings.HasPrefix(k, base) {
			if match != "" {
				return "" // ambiguous
			}
			match = k
		}
	}
	return match
}

// resolveSeatKinds builds the seat→kind map for an orchestrate run: start from
// init-event kinds, then fill any gaps from the current projection roster's Kind
// (add-seat kinds), then from a name heuristic against known agent kinds
// (seat "opencode-worker" → "opencode", "gemini-worker" → "gemini-cli").
func (s *Server) resolveSeatKinds(dir string) map[string]string {
	km := seatKindsFromInit(dir)
	if km == nil {
		km = map[string]string{}
	}

	// Fill gaps from projection roster (picks up add-seat events with a kind).
	if st, err := pact.At(dir).StateProjection(); err == nil {
		for _, a := range st.Agents {
			if _, already := km[a.ID]; !already && a.Kind != "" {
				km[a.ID] = a.Kind
			}
		}
		// Name heuristic for seats still without a kind.
		known := agent.Kinds()
		for _, a := range st.Agents {
			if _, already := km[a.ID]; already {
				continue
			}
			if inferred := inferKindFromName(a.ID, known); inferred != "" {
				km[a.ID] = inferred
			}
		}
	}

	return km
}

func (s *Server) defaultExecOrchestrate(dir string, args, env []string) error {
	bin, err := exec.LookPath("pactify")
	if err != nil {
		return fmt.Errorf("pactify not in PATH: %w", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	logf := driverLogFile(dir)
	if logf != nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	if err := cmd.Start(); err != nil {
		if logf != nil {
			logf.Close()
		}
		return err
	}
	// Reap the child: serve is a long-lived daemon, so an un-Waited driver
	// lingers as a zombie. On failure leave a trace — a driver that dies before
	// its first status.json write otherwise reports nothing anywhere.
	go func() {
		werr := cmd.Wait()
		if logf != nil {
			logf.Close()
		}
		orchClearRunning(dir)
		if werr != nil {
			writeDriverError(dir, werr)
		}
	}()
	return nil
}

// driverLogFile opens .pact/orchestrate/driver.log (truncated per run) for the
// child's stdout/stderr; nil means the output is discarded as before.
func driverLogFile(dir string) *os.File {
	p := filepath.Join(dir, ".pact", "orchestrate", "driver.log")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil
	}
	f, err := os.Create(p)
	if err != nil {
		return nil
	}
	return f
}

func writeDriverError(dir string, err error) {
	p := filepath.Join(dir, ".pact", "orchestrate", "driver-error.log")
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	msg := time.Now().UTC().Format(time.RFC3339) + " orchestrate driver exited: " + err.Error() + " (output in driver.log)\n"
	_ = os.WriteFile(p, []byte(msg), 0o644)
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

	var req orchestrateRunReq
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	if code, err := s.spawnOrchestrate(dir, req.Feature, req.SeatKinds, req.MaxConcurrency, resume); err != nil {
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status_url": "/api/projects/" + id + "/orchestrate/status",
	})
}

// spawnOrchestrate launches the orchestrate driver subprocess for dir — the
// shared core of the dashboard Run button (HTTP) and the remote orchestrate rpc.
// Returns an HTTP-ish status code with the error for the HTTP caller's mapping.
func (s *Server) spawnOrchestrate(dir, feature string, seatKinds map[string]string, maxConcurrency int, resume bool) (int, error) {
	return s.spawnOrchestrateWith(dir, feature, seatKinds, maxConcurrency, resume, false)
}

// spawnOrchestrateApprove resumes the paused run adopting the pending fallback
// proposal (the dashboard approval path; CLI equivalent:
// `pactify orchestrate --resume --approve-fallback`).
func (s *Server) spawnOrchestrateApprove(dir string) (int, error) {
	return s.spawnOrchestrateWith(dir, "", nil, 0, true, true)
}

func (s *Server) spawnOrchestrateWith(dir, feature string, seatKinds map[string]string, maxConcurrency int, resume, approveFallback bool) (int, error) {
	// Claim the in-memory marker BEFORE the file check: it is the only guard
	// with no window between check and spawn (see orchRunning).
	if !orchMarkRunning(dir) {
		return http.StatusConflict, fmt.Errorf("orchestrate is already running")
	}
	if s.orchestrateRunning(dir) {
		orchClearRunning(dir)
		return http.StatusConflict, fmt.Errorf("orchestrate is already running")
	}

	km := s.resolveSeatKinds(dir)
	for seat, kind := range seatKinds {
		km[seat] = kind
	}

	args := []string{"orchestrate", "--as", s.seat}
	for seat, kind := range km {
		args = append(args, "--seat-kind", seat+"="+kind)
	}
	if feature != "" {
		args = append(args, "--feature", feature)
	}
	if maxConcurrency > 0 {
		args = append(args, "--max-concurrency", fmt.Sprintf("%d", maxConcurrency))
	}
	if resume {
		args = append(args, "--resume")
	}
	if approveFallback {
		args = append(args, "--approve-fallback")
	}
	env := []string{"PACT_AGENT_ID=" + s.seat}

	execFn := s.execOrchestrate
	injected := execFn != nil
	if !injected {
		execFn = s.defaultExecOrchestrate
	}
	if err := execFn(dir, args, env); err != nil {
		orchClearRunning(dir)
		return http.StatusInternalServerError, err
	}
	// The default exec clears the marker from its reaper when the child exits;
	// an injected fn (tests) has no child to reap, so clear once it returns.
	if injected {
		orchClearRunning(dir)
	}
	return 0, nil
}

// scheduleSpawn adapts spawnOrchestrate to schedule.SpawnFunc: it resolves a
// project registry name to its on-disk dir and launches a serial orchestrate run
// (nil seat-kinds → resolved from the ledger; feature "" → driver picks). A
// conflict (an orchestrate already running for that project) maps to
// alreadyRunning=true rather than an error, so the scheduler logs a skip.
func (s *Server) scheduleSpawn(project, feature string) (bool, error) {
	_, dir, ok := s.project(project)
	if !ok {
		return false, fmt.Errorf("unknown project %q", project)
	}
	code, err := s.spawnOrchestrate(dir, feature, nil, 0, false)
	if err != nil {
		if code == http.StatusConflict {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// StartScheduler launches the minute-granularity schedule runner in the calling
// goroutine (cmd_serve runs it as `go srv.StartScheduler(ctx)`). It reads
// ~/.pactify/schedules.json each tick and spawns orchestrate for due enabled
// schedules; the existing conflict guard skips any project already running.
func (s *Server) StartScheduler(ctx context.Context) {
	r := &schedule.Runner{
		Path:  schedule.DefaultPath(),
		Spawn: s.scheduleSpawn,
		Log: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "pactify serve: "+format+"\n", args...)
		},
	}
	r.Start(ctx)
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
		req.Branch = shipBaseBranch(dir)
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

// shipBaseBranch resolves the project's integration base for a ship with no
// explicit branch: ledger rebaseline overrides init base_branch (the same
// precedence internal/pact applies), then origin/HEAD, then "main" as last
// resort. The dashboard never sends a branch, so a hardcoded "main" default
// shipped/PR'd against the wrong base on projects whose base differs.
func shipBaseBranch(dir string) string {
	if evs, err := event.ReadAll(logPath(dir)); err == nil {
		base := ""
		for _, e := range evs {
			switch e.EventType {
			case "init":
				if base == "" {
					if b, ok := e.Payload["base_branch"].(string); ok {
						base = b
					}
				}
			case "rebaseline":
				if b, ok := e.Payload["base_branch"].(string); ok && b != "" {
					base = b
				}
			}
		}
		if base != "" {
			return base
		}
	}
	if def := gitx.DefaultBranch(dir); def != "" {
		return def
	}
	return "main"
}

// defaultGitDiff returns a REDACTED diff summary — `git diff --stat`: changed
// file names + per-file insertion/deletion counts, but NO file content. The full
// patch would leak uncommitted secrets (.env, tokens) and all source over an
// unauthenticated read endpoint (review finding M1). The stat summary is enough
// for the dashboard's "what changed" view; viewing actual content must go through
// an authenticated channel once the read surface gets per-seat auth (deferred to
// the H1/C4 identity coordination window).
func defaultGitDiff(dir string, staged bool) (string, error) {
	args := []string{"-C", dir, "diff", "--stat"}
	if staged {
		args = append(args, "--staged")
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}

// handleOrchestrateDiff returns a redacted diff summary (file names + line counts,
// no content — see defaultGitDiff). It stays ungated (no actingProject check) for
// now; a real per-seat read-surface credential is deferred to H1/C4. The prior
// "full git diff, safe for any observer" was the M1 leak — the redaction is what
// makes a read-only summary tolerable until that auth lands.
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
