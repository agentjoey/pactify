package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
)

const (
	// CursorKind is the harness kind used for vendor environment isolation.
	CursorKind       = "cursor-agent"
	cursorAuthMethod = "cursor_login"

	cursorProbeTimeout = 10 * time.Second
	cursorAskMode      = "ask"
	cursorAgentMode    = "agent"
)

// cursorAuthTimeout bounds authenticate, which an agent without a login never answers.
var cursorAuthTimeout = 20 * time.Second

// Cursor releases are dated builds such as 2026.11.17-f532c45.
var cursorVersionPattern = regexp.MustCompile(`^\d{4}\.\d{1,2}\.\d{1,2}-[0-9a-f]{6,}$`)

// ErrReadOnlyUnenforceable means the session cannot be switched into Cursor's
// read-only ask mode, so a read-only job must not start.
var ErrReadOnlyUnenforceable = errors.New("cursor did not offer its read-only ask mode; read-only access cannot be enforced")

type cursorBinary struct{ path, version string }

var cursorBinaries = struct {
	sync.Mutex
	byPath map[string]cursorBinary
}{byPath: map[string]cursorBinary{}}

// ResolveCursorBinary finds Cursor's CLI. The legacy name cursor-agent is tried
// first; the newer name agent is accepted only after its help text identifies
// Cursor, because other CLIs use that name too. Probes run with a throwaway
// CURSOR_CONFIG_DIR so discovery never writes into the user's configuration.
func ResolveCursorBinary(ctx context.Context) (path, version string, err error) {
	key := os.Getenv("PATH")
	cursorBinaries.Lock()
	cached, ok := cursorBinaries.byPath[key]
	cursorBinaries.Unlock()
	if ok {
		return cached.path, cached.version, nil
	}
	var notes []string
	for _, name := range []string{"cursor-agent", "agent"} {
		candidate, lookErr := exec.LookPath(name)
		if lookErr != nil {
			continue
		}
		version, probeErr := probeCursorBinary(ctx, candidate, name == "agent")
		if probeErr != nil {
			notes = append(notes, fmt.Sprintf("%s at %s is not Cursor's CLI: %v", name, candidate, probeErr))
			continue
		}
		cursorBinaries.Lock()
		cursorBinaries.byPath[key] = cursorBinary{candidate, version}
		cursorBinaries.Unlock()
		return candidate, version, nil
	}
	if len(notes) > 0 {
		return "", "", errors.New(strings.Join(notes, "; "))
	}
	return "", "", errors.New("neither cursor-agent nor agent is on PATH")
}

// CursorVersion reports the installed Cursor CLI version.
func CursorVersion(ctx context.Context) (string, error) {
	_, v, err := ResolveCursorBinary(ctx)
	return v, err
}

func probeCursorBinary(ctx context.Context, path string, needHelp bool) (string, error) {
	dir, err := os.MkdirTemp("", "pactify-cursor-probe-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	run := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, cursorProbeTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Env = append(os.Environ(), "CURSOR_CONFIG_DIR="+dir, "NO_COLOR=1")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return "", err
		}
		return out.String(), nil
	}
	out, err := run("--version")
	if err != nil {
		return "", err
	}
	version, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	version = strings.TrimSpace(version)
	if !cursorVersionPattern.MatchString(version) {
		return "", fmt.Errorf("unexpected --version output %q", version)
	}
	if needHelp {
		help, err := run("--help")
		if err != nil {
			return "", err
		}
		if !strings.Contains(help, "Start the Cursor Agent") {
			return "", errors.New("--help does not identify Cursor Agent")
		}
	}
	return version, nil
}

type cursorBackend struct{}

// NewCursorBackend drives Cursor's `agent acp` server through the shared ACP client.
func NewCursorBackend() Backend { return cursorBackend{} }

func (b cursorBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	client, _, err := openCursor(ctx, opts)
	if err != nil {
		return nil, err
	}
	res, err := client.NewSessionDetailed(ctx, opts.RepoDir)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cursor new session: %w", err)
	}
	info, err := applyCursorOpts(ctx, client, res.SessionID, res, opts)
	if err != nil {
		_ = client.Close()
		return nil, &StartError{ID: string(res.SessionID), Err: fmt.Errorf("cursor configure: %w", err)}
	}
	sess := newACPSession(client, res.SessionID)
	sess.info = info
	return sess, nil
}

func (b cursorBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	return b.ResumeWith(ctx, threadID, StartOpts{})
}

func (b cursorBackend) ResumeWith(ctx context.Context, threadID string, opts StartOpts) (Session, error) {
	client, _, err := openCursor(ctx, opts)
	if err != nil {
		return nil, err
	}
	sid := acp.SessionID(threadID)
	if err := client.LoadSession(ctx, sid); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cursor load session: %w", err)
	}
	info, err := applyCursorResumeOpts(ctx, client, sid, opts)
	if err != nil {
		_ = client.Close()
		return nil, &StartError{ID: threadID, Err: fmt.Errorf("cursor configure resumed session: %w", err)}
	}
	sess := newACPSession(client, sid)
	sess.info = info
	return sess, nil
}

// openCursor spawns `agent acp`, performs the handshake and authenticates.
// Authentication may never be answered when no login exists, so it is bounded;
// session/new then fails with a typed -32000 error that classifies as auth.
func openCursor(ctx context.Context, opts StartOpts) (*acp.Client, acp.InitializeResult, error) {
	path, _, err := ResolveCursorBinary(ctx)
	if err != nil {
		return nil, acp.InitializeResult{}, fmt.Errorf("cursor CLI unavailable: %w", err)
	}
	var observe func(string, []byte)
	if opts.Transcript != nil {
		observe = opts.Transcript.Observe
	}
	client, err := acp.SpawnObserved(ctx, path, []string{"acp"}, opts.Env, opts.RepoDir, observe, opts.Stderr)
	if err != nil {
		return nil, acp.InitializeResult{}, fmt.Errorf("cursor acp spawn: %w", err)
	}
	init, err := client.Initialize(ctx)
	if err != nil {
		_ = client.Close()
		return nil, init, fmt.Errorf("cursor acp initialize: %w", err)
	}
	if slices.ContainsFunc(init.AuthMethods, func(m acp.AuthMethod) bool { return m.ID == cursorAuthMethod }) {
		authCtx, cancel := context.WithTimeout(ctx, cursorAuthTimeout)
		err = client.Authenticate(authCtx, cursorAuthMethod)
		cancel()
		if err != nil && ctx.Err() == nil && !errors.Is(err, context.DeadlineExceeded) {
			_ = client.Close()
			return nil, init, fmt.Errorf("cursor authenticate: %w", err)
		}
	}
	return client, init, nil
}

// applyCursorOpts applies explicit model overrides and always selects the
// access mode: ask (Cursor's read-only mode) for read-only jobs, agent otherwise.
// Effort is part of Cursor's parameterized model id, not a separate setting.
func applyCursorOpts(ctx context.Context, c acpConfigurer, sid acp.SessionID, res acp.NewSessionResult, opts StartOpts) (SessionInfo, error) {
	if opts.Effort != "" {
		return SessionInfo{}, fmt.Errorf("%w: cursor has no separate effort setting; put it in the model id, for example model[effort=high]", ErrEffortUnavailable)
	}
	info, err := applyACPModelEffort("cursor", ctx, c, sid, res, opts)
	if err != nil {
		return info, err
	}
	if opts.Access == AccessReadOnly {
		if !hasMode(res.Modes, cursorAskMode) {
			return info, ErrReadOnlyUnenforceable
		}
		if err := c.SetMode(ctx, sid, cursorAskMode); err != nil {
			return info, fmt.Errorf("set %s mode: %w", cursorAskMode, err)
		}
		info.Access = cursorAskMode
		return info, nil
	}
	if res.CurrentModeID != cursorAgentMode && hasMode(res.Modes, cursorAgentMode) {
		if err := c.SetMode(ctx, sid, cursorAgentMode); err != nil {
			return info, fmt.Errorf("set %s mode: %w", cursorAgentMode, err)
		}
		info.Access = cursorAgentMode
	}
	return info, nil
}

// ACP session/load reports neither modes nor settings, so the access mode is
// set unconditionally. A read-only resume fails closed; a write resume is best
// effort because a loaded session is already in agent mode by default.
func applyCursorResumeOpts(ctx context.Context, c acpConfigurer, sid acp.SessionID, opts StartOpts) (SessionInfo, error) {
	if opts.Effort != "" {
		return SessionInfo{}, fmt.Errorf("%w: cursor has no separate effort setting", ErrEffortUnavailable)
	}
	info := SessionInfo{}
	if opts.Model != "" {
		updated, err := c.SetConfigOption(ctx, sid, "model", opts.Model)
		if err != nil {
			return info, fmt.Errorf("set model %q: %w; available values not reported by session/load", opts.Model, err)
		}
		option, ok := acp.FindConfig(updated, "model")
		if !ok || option.CurrentValue != opts.Model {
			return info, fmt.Errorf("%w: asked for %q, reported %q", ErrModelUnavailable, opts.Model, option.CurrentValue)
		}
		info.Model = option.CurrentValue
	}
	if opts.Access == AccessReadOnly {
		if err := c.SetMode(ctx, sid, cursorAskMode); err != nil {
			return info, fmt.Errorf("set %s mode: %w", cursorAskMode, err)
		}
		info.Access = cursorAskMode
		return info, nil
	}
	if err := c.SetMode(ctx, sid, cursorAgentMode); err == nil {
		info.Access = cursorAgentMode
	}
	return info, nil
}

// CursorModelDetails lists the models Cursor offers over ACP without sending a
// prompt. The session it creates is empty and lives in opts' private config dir.
func CursorModelDetails(ctx context.Context, opts StartOpts) (ModelDiscovery, error) {
	out := ModelDiscovery{Items: []string{}, Efforts: []string{}}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	o := opts
	o.Stderr = io.Discard
	client, _, err := openCursor(ctx, o)
	if err != nil {
		return out, err
	}
	defer client.Close()
	res, err := client.NewSessionDetailed(ctx, opts.RepoDir)
	if err != nil {
		return out, fmt.Errorf("cursor new session: %w", err)
	}
	if m, ok := acp.FindConfig(res.ConfigOptions, "model"); ok {
		out.DefaultModel = m.CurrentValue
		for _, v := range m.Options {
			if v.Value != "" && !slices.Contains(out.Items, v.Value) {
				out.Items = append(out.Items, v.Value)
			}
		}
	}
	if len(out.Items) == 0 {
		return out, errors.New("cursor: ACP advertised no model options")
	}
	return out, nil
}

// CursorModels lists the advertised model ids.
func CursorModels(ctx context.Context, opts StartOpts) ([]string, error) {
	d, err := CursorModelDetails(ctx, opts)
	return d.Items, err
}

var cursorFailureRules = []failureRule{
	{"authentication required", "auth"},
	{"not logged in", "auth"},
	{"agent login", "auth"},
	{"resource_exhausted", "quota"},
	{"usage limit", "quota"},
	{"quota", "quota"},
	{"requested model is not offered by this agent", "model"},
	{"connection error", "transient"},
	{"unavailable", "transient"},
	{"timed out", "transient"},
	{"econnreset", "transient"},
}

// cursorFailure classifies by typed ACP evidence first. Cursor reports quota
// exhaustion only as text, so the table below is a documented best effort.
func cursorFailure(message string, raw json.RawMessage) Failure {
	if f := acpFailure("", raw, nil); f.Class != "other" {
		return f
	}
	return acpFailure(message, raw, cursorFailureRules)
}
