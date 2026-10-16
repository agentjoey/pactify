// Package sub provides persistence and result collection for harness jobs.
package sub

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type Request struct {
	CompletionFollowups *int   `json:"completionFollowups,omitempty"`
	StartedFrom         string `json:"startedFrom,omitempty"`
	Retries             *int   `json:"retries,omitempty"` // nil uses the default; zero disables retries.
	WithUserConfig      bool   `json:"withUserConfig"`
	Harness             string `json:"harness"`
	Model               string `json:"model,omitempty"`
	Effort              string `json:"effort,omitempty"`
	Access              string `json:"access"`
	Cwd                 string `json:"cwd"`
	Resume              string `json:"resume,omitempty"`
	Brief               string `json:"brief"`
}

type State struct {
	ReadOnlyBaseline     *ReadOnlySnapshot `json:"readOnlyBaseline,omitempty"`
	Routing              *Routing          `json:"routing,omitempty"`
	Baseline             *Snapshot         `json:"baseline,omitempty"`
	RouteWarnings        []string          `json:"routeWarnings,omitempty"`
	FallbackFrom         string            `json:"fallbackFrom,omitempty"`
	FallbackTo           string            `json:"fallbackTo,omitempty"`
	RetryAttempt         int               `json:"retryAttempt,omitempty"`
	RetryLimit           int               `json:"retryLimit,omitempty"`
	Retries              []Retry           `json:"retries,omitempty"`
	Descendants          []procx.Identity  `json:"descendants,omitempty"`
	ProcessMarker        string            `json:"processMarker,omitempty"`
	ProcessTrackingError string            `json:"processTrackingError,omitempty"`
	TempDirOwned         bool              `json:"tempDirOwned,omitempty"`
	Repo                 string            `json:"repo,omitempty"`
	StartedAt            time.Time         `json:"startedAt,omitzero"`
	ResumeOf             string            `json:"resumeOf,omitempty"`
	HarnessHomes         map[string]string `json:"harnessHomes,omitempty"`
	HarnessHome          string            `json:"harnessHome,omitempty"`
	ConfigMode           string            `json:"configMode,omitempty"`
	ID                   string            `json:"id"`
	Status               Status            `json:"status"`
	Request              Request           `json:"request"`
	PID                  int               `json:"pid,omitempty"`
	Worker               *procx.Identity   `json:"worker,omitempty"`
	Processes            []procx.Identity  `json:"processes,omitempty"`
	CancelRequested      bool              `json:"cancelRequested,omitempty"`
	CreatedAt            time.Time         `json:"createdAt"`
	UpdatedAt            time.Time         `json:"updatedAt"`
	FinishedAt           time.Time         `json:"finishedAt,omitempty"`
	Error                string            `json:"error,omitempty"`
	Worktree             *Worktree         `json:"worktree,omitempty"`
}

type Store struct{ Root string }

func DefaultStore() (Store, error) {
	home := os.Getenv("PACTIFY_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Store{}, err
		}
		home = filepath.Join(userHome, ".pactify")
	}
	return Store{Root: filepath.Join(home, "jobs")}, nil
}

// NewJobID panics if rnd cannot supply three bytes: its signature cannot
// report entropy failure, and a predictable fallback would risk collisions.
func NewJobID(now time.Time, rnd io.Reader) string {
	var suffix [3]byte
	if _, err := io.ReadFull(rnd, suffix[:]); err != nil {
		panic(fmt.Errorf("new job id: %w", err))
	}
	return now.Format("20060102-150405") + "-" + hex.EncodeToString(suffix[:])
}

func validJobID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00") {
		return fmt.Errorf("invalid job id %q", id)
	}
	return nil
}

func (s Store) Dir(id string) string        { return filepath.Join(s.Root, id) }
func (s Store) LogPath(id string) string    { return filepath.Join(s.Dir(id), "log.jsonl") }
func (s Store) ResultPath(id string) string { return filepath.Join(s.Dir(id), "result.json") }

func (s Store) Create(id string, req Request) (State, error) {
	if err := validJobID(id); err != nil {
		return State{}, err
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return State{}, err
	}
	if err := os.Mkdir(s.Dir(id), 0o700); err != nil {
		return State{}, err
	}
	now := time.Now().UTC()
	st := State{ID: id, Status: StatusQueued, Request: req, CreatedAt: now, UpdatedAt: now}
	if req.Cwd != "" {
		st.Repo, _ = repositoryRoot(req.Cwd)
	}
	h, _ := Lookup(req.Harness)
	st.ConfigMode = h.configMode(req.WithUserConfig)
	if err := s.Save(st); err != nil {
		_ = os.Remove(s.Dir(id))
		return State{}, err
	}
	return st, nil
}

func (s Store) Load(id string) (State, error) {
	if err := validJobID(id); err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(filepath.Join(s.Dir(id), "state.json"))
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("job %s state: %w", id, err)
	}
	if st.ID != id {
		return State{}, fmt.Errorf("job %s state has mismatched id %q", id, st.ID)
	}
	return st, nil
}

// Save atomically replaces state.json; the caller owns state timestamps.
// Runtime lifecycle mutations must hold the per-job lock via WithState.
func (s Store) Save(st State) error {
	if err := validJobID(st.ID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir(st.ID), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.Dir(st.ID), "state.json"))
}

func (s Store) LastSessionFor(harness, cwd string) (string, error) {
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var latest State
	var sessionID string
	found := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, err := s.Load(entry.Name())
		if err != nil {
			continue
		}
		if st.Request.Harness != harness || st.Request.Cwd != cwd || st.Status != StatusSucceeded || (found && !st.FinishedAt.After(latest.FinishedAt)) {
			continue
		}
		data, err := os.ReadFile(s.ResultPath(st.ID))
		if err != nil {
			continue
		}
		var result struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			continue
		}
		latest, sessionID, found = st, result.SessionID, true
	}
	return sessionID, nil
}

// lastCLIVersionFor is best-effort history across repositories and terminal statuses.
func (s Store) lastCLIVersionFor(harness string) string {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return ""
	}
	var latest time.Time
	var version string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, err := s.Load(entry.Name())
		if err != nil || st.Request.Harness != harness {
			continue
		}
		switch st.Status {
		case StatusSucceeded, StatusFailed, StatusCanceled:
		default:
			continue
		}
		if version != "" && !st.FinishedAt.After(latest) {
			continue
		}
		data, err := os.ReadFile(s.ResultPath(st.ID))
		if err != nil {
			continue
		}
		var result Result
		if err := json.Unmarshal(data, &result); err != nil || result.CLIVersion == "" {
			continue
		}
		latest, version = st.FinishedAt, result.CLIVersion
	}
	return version
}
