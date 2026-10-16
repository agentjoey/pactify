// Package subpanel serves the standalone, read-only harness job panel.
package subpanel

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

//go:embed dist
var assets embed.FS

type Server struct {
	store       sub.Store
	token       string
	launchToken string
	tokenMu     sync.Mutex
	exchanged   bool
}

type Detail struct {
	Job         sub.JobSummary `json:"job"`
	Brief       string         `json:"brief"`
	Result      *sub.Result    `json:"result,omitempty"`
	Markdown    string         `json:"markdown"`
	ResultError string         `json:"resultError,omitempty"`
}

func New(store sub.Store) (*Server, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(secret[:])
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	return &Server{store: store, token: token, launchToken: hex.EncodeToString(secret[:])}, nil
}

// Listen deliberately has no host option: even a specified port stays on IPv4 loopback.
func Listen(port int) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid panel port %d", port)
	}
	return net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}
func (s *Server) URL(ln net.Listener) string {
	return "http://" + ln.Addr().String() + "/?token=" + s.launchToken
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				_ = server.Close()
			}
		case <-stopped:
		}
	}()
	err := server.Serve(ln)
	close(stopped)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) matches(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}
func (s *Server) authenticated(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	return strings.HasPrefix(auth, "Bearer ") && s.matches(strings.TrimPrefix(auth, "Bearer "))
}

// exchange consumes the launch secret atomically. The independent session secret
// is delivered only in this response, never in cookies or redirect URLs.
func (s *Server) exchange(token string) bool {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.exchanged || subtle.ConstantTimeCompare([]byte(token), []byte(s.launchToken)) != 1 {
		return false
	}
	s.exchanged = true
	return true
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	api := http.StripPrefix("/api", ReadOnlyHandler(s.store))
	root, _ := fs.Sub(assets, "dist")
	mux.Handle("GET /", http.FileServer(http.FS(root)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" {
			http.Error(w, "loopback host required", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/" && r.URL.Query().Has("token") {
			if !s.exchange(r.URL.Query().Get("token")) {
				http.Error(w, "unauthorized", 401)
				return
			}
			page, err := fs.ReadFile(root, "index.html")
			if err != nil {
				apiError(w, err)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, strings.Replace(string(page), "</head>", `<meta name="pactify-session" content="`+s.token+`"></head>`, 1))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !s.authenticated(r) {
				http.Error(w, "unauthorized", 401)
				return
			}
			api.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// ReadOnlyHandler serves /router, /cost, /jobs and its detail, logs and diff subresources.
// The host owns authentication and must mount this behind its security boundary.
// Reads use atomic snapshots and never recover workers or write job files.
func ReadOnlyHandler(store sub.Store) http.Handler {
	s := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cost", s.cost)
	mux.HandleFunc("GET /router", s.router)
	mux.HandleFunc("GET /harnesses", s.harnesses)
	mux.HandleFunc("GET /jobs", s.jobs)
	mux.HandleFunc("GET /jobs/{id}", s.detail)
	mux.HandleFunc("GET /jobs/{id}/logs", s.logs)
	mux.HandleFunc("GET /jobs/{id}/diff", s.diff)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Go's GET patterns also accept HEAD; these APIs accept GET exclusively.
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "read-only: GET required", http.StatusMethodNotAllowed)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
func apiError(w http.ResponseWriter, err error) {
	code := 500
	if os.IsNotExist(err) {
		code = 404
	}
	http.Error(w, err.Error(), code)
}

// Load follows state's atomic rename convention. Unlike status CLI's Recover,
// panel queries never create lock files, publish results or change job state.
func (s *Server) states() ([]sub.State, error) {
	rows, err := s.store.States()
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	return rows, nil
}
func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	states, err := s.states()
	if err != nil {
		apiError(w, err)
		return
	}
	rows := []sub.JobSummary{}
	repo := r.URL.Query().Get("repo")
	target := ""
	if repo != "" {
		target = sub.NormalizeRepo(repo)
	}
	now := time.Now()
	index := s.store.NewSummaryIndex(states)
	for _, st := range states {
		if repo != "" {
			root := sub.JobRepo(st)
			if root == "" || sub.NormalizeRepo(root) != target {
				continue
			}
		}
		rows = append(rows, index.Summarize(st, now))
		if r.URL.Query().Get("all") != "1" && len(rows) == 10 {
			break
		}
	}
	jsonResponse(w, rows)
}
func (s *Server) load(r *http.Request) (sub.State, error) { return s.store.Load(r.PathValue("id")) }
func (s *Server) result(id string) (*sub.Result, error) {
	file, err := os.Open(s.store.ResultPath(id))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const limit = 4 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("result exceeds 4 MiB")
	}
	var result sub.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	st, err := s.load(r)
	if err != nil {
		apiError(w, err)
		return
	}
	states, err := s.states()
	if err != nil {
		apiError(w, err)
		return
	}
	d := Detail{Job: s.store.NewSummaryIndex(states).Summarize(st, time.Now()), Brief: st.Request.Brief}
	d.Result, err = s.result(st.ID)
	if err == nil {
		d.Markdown = d.Result.Markdown()
	} else if !os.IsNotExist(err) || sub.Terminal(st.Status) {
		d.ResultError = "Result unavailable: " + err.Error()
	}
	jsonResponse(w, d)
}

type eventWriter struct{ w http.ResponseWriter }

func (e eventWriter) Write(data []byte) (int, error) {
	// Logs emits one formatted line per write. JSON encoding keeps all text inside
	// one SSE data frame, including arbitrary Unicode and control characters.
	encoded, _ := json.Marshal(strings.TrimSuffix(string(data), "\n"))
	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", encoded); err != nil {
		return 0, err
	}
	if err := http.NewResponseController(e.w).Flush(); err != nil {
		return 0, err
	}
	return len(data), nil
}
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	st, err := s.load(r)
	if err != nil {
		apiError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	_ = http.NewResponseController(w).Flush()
	err = s.store.Logs(r.Context(), st.ID, eventWriter{w}, sub.LogOptions{Tail: 100, Follow: true, ReadOnly: true})
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		data, _ := json.Marshal(err.Error())
		_, _ = fmt.Fprintf(w, "event: log-error\ndata: %s\n\n", data)
	}
	_, _ = fmt.Fprint(w, "event: done\ndata: {}\n\n")
	_ = http.NewResponseController(w).Flush()
}
