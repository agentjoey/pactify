package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/registry"
	"github.com/fsnotify/fsnotify"
)

// sseHeartbeat is the interval between SSE keep-alive comment frames. It must sit
// comfortably under the idle timeout of any reverse proxy in front of serve
// (Cloudflare Tunnel cuts idle streams around 100s), so a quiet project's live
// connection is not dropped.
const sseHeartbeat = 25 * time.Second

// Server holds the watched projects and the SSE hub.
type Server struct {
	// pmu guards the project registry maps + watcher bookkeeping (projects,
	// order, offsets, watchPaths). The HTTP handlers read these concurrently
	// with runtime AddProject/RemoveProject and the fsnotify watchLoop, so all
	// access goes through pmu (RWMutex: handlers RLock, mutations Lock). It was
	// lock-free when projects were fixed at New(); runtime registry dynamics
	// (O2) make the maps mutable, hence the lock.
	pmu        sync.RWMutex
	projects   map[string]registry.Project // by name (id)
	order      []string                    // stable display order
	offsets    map[string]int64            // id -> bytes consumed from log.jsonl
	watchPaths map[string]string           // id -> watched log.jsonl path

	hub     *hub
	watcher *fsnotify.Watcher

	seat string // acting seat for author writes ("" = none configured)

	// mu serializes author writes per project: authoring verbs append to the
	// same log.jsonl, so concurrent assigns must not interleave. muGuard
	// guards lazy creation of the per-project mutexes.
	muGuard sync.Mutex
	mu      map[string]*sync.Mutex

	relay *relay

	execOrchestrate func(dir string, args, env []string) error
	finishRunner    func(dir, name string, args ...string) (string, error)
	gitDiffFn       func(dir string, staged bool) (string, error)
}

// SetSeat configures the acting seat used for author (write) endpoints.
func (s *Server) SetSeat(seat string) { s.seat = seat }

// SetRelay configures the optional best-effort relay for log events.
func (s *Server) SetRelay(url, token string) { s.relay = newRelay(url, token) }

// projectMu returns the lazily-created mutex serializing author writes for id.
func (s *Server) projectMu(id string) *sync.Mutex {
	s.muGuard.Lock()
	defer s.muGuard.Unlock()
	if s.mu == nil {
		s.mu = map[string]*sync.Mutex{}
	}
	m, ok := s.mu[id]
	if !ok {
		m = &sync.Mutex{}
		s.mu[id] = m
	}
	return m
}

// New builds a Server over the given projects.
func New(projects []registry.Project) *Server {
	s := &Server{
		projects:   map[string]registry.Project{},
		offsets:    map[string]int64{},
		watchPaths: map[string]string{},
		hub:        newHub(),
	}
	for _, p := range projects {
		if _, ok := s.projects[p.Name]; ok {
			continue
		}
		s.projects[p.Name] = p
		s.order = append(s.order, p.Name)
	}
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("GET /api/projects/{id}/state", s.handleState)
	mux.HandleFunc("GET /api/projects/{id}/events", s.handleEvents)
	s.registerRegistryRoutes(mux)
	s.registerAuthorRoutes(mux)
	s.registerWiringRoutes(mux)
	s.registerAgentRoutes(mux)
	s.registerSessionRoutes(mux)
	s.registerAuditRoutes(mux)
	s.registerManifestRoutes(mux)
	s.registerStatsRoutes(mux)
	s.registerSeatsRoutes(mux)
	s.registerTimelineRoutes(mux)
	s.registerOrchestrateRoutes(mux)
	s.registerSetupRoutes(mux)
	s.registerRecipeRoutes(mux)
	s.registerPlanRoutes(mux)
	mux.Handle("/", dashboardHandler())
	return mux
}

func (s *Server) handleProjects(w http.ResponseWriter, _ *http.Request) {
	type item struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Path          string `json:"path"`
		Project       string `json:"project"`
		FeatureCount  int    `json:"feature_count"`
		AwaitingCount int    `json:"awaiting_count"`
	}
	s.pmu.RLock()
	projs := make([]registry.Project, 0, len(s.order))
	for _, id := range s.order {
		projs = append(projs, s.projects[id])
	}
	s.pmu.RUnlock()
	out := []item{}
	for _, p := range projs {
		dto, _ := ProjectState(p.Path)
		out = append(out, item{ID: p.Name, Name: p.Name, Path: p.Path, Project: dto.Project, FeatureCount: len(dto.Features), AwaitingCount: dto.AwaitingCount})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.pmu.RLock()
	p, ok := s.projects[r.PathValue("id")]
	s.pmu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown project")
		return
	}
	// at param: ABSENT → full state (unchanged); present+valid → prefix fold of
	// the first N events (clamped); present+malformed/negative → 400.
	at, present, valid := parseAt(r)
	if present && !valid {
		writeErr(w, http.StatusBadRequest, "at must be a non-negative integer")
		return
	}
	if !present {
		at = -1 // all events
	}
	dto, err := ProjectStateAt(p.Path, at)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.pmu.RLock()
	_, known := s.projects[id]
	s.pmu.RUnlock()
	if !known {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// Subscribe BEFORE flushing headers so events appended the instant the client
	// sees the response are buffered on the channel, not dropped (avoids a race).
	ch := s.hub.subscribe(id)
	defer s.hub.unsubscribe(id, ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Tell reverse proxies (nginx and friends) not to buffer this response.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Emit an initial frame and flush it immediately. A proxy that buffers the
	// response until the first body byte (Cloudflare Tunnel does this) would
	// otherwise hold the 200 back until the first real event — so EventSource never
	// fires onopen and the dashboard shows "offline" on a quiet project. The retry
	// directive also sets the client's reconnect backoff.
	_, _ = io.WriteString(w, "retry: 3000\n: ok\n\n")
	fl.Flush()

	// Heartbeat keeps the stream non-idle so a proxy doesn't drop it (and re-flushes
	// periodically). Comment frames (": ...") are ignored by EventSource.
	hb := time.NewTicker(sseHeartbeat)
	defer hb.Stop()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write([]byte("event: pact\ndata: " + line + "\n\n"))
			fl.Flush()
		case <-hb.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
