package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// NewCodexBackend returns a Backend that spawns a local `codex app-server`
// subprocess and speaks JSON-RPC 2.0 over stdio (newline-delimited JSON).
func NewCodexBackend() Backend {
	return &codexBackend{}
}

type codexBackend struct{}

// Start spawns codex app-server, performs initialize/initialized, and starts a
// new thread rooted at opts.RepoDir.
func (b *codexBackend) Start(ctx context.Context, opts StartOpts) (Session, error) {
	return newCodexSession(ctx, opts, "")
}

// Resume spawns codex app-server and resumes an existing thread.
func (b *codexBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	return b.ResumeWith(ctx, threadID, StartOpts{})
}

// ResumeWith resumes a thread with explicit delegation options.
func (b *codexBackend) ResumeWith(ctx context.Context, threadID string, opts StartOpts) (Session, error) {
	return newCodexSession(ctx, opts, threadID)
}

// codexSession owns one codex app-server process and its JSON-RPC connection.
type codexSession struct {
	client *codexClient

	threadID string
	info     SessionInfo
	effort   string
	turnMu   sync.Mutex // Hidden jobs serialize prompt startup with interruption.
	hidden   bool       // archive the thread on Close (StartOpts.Hidden)

	events    chan Event
	approvals chan ApprovalRequest

	done chan struct{}
	// Serialize channel closure against all dispatch callbacks.
	dispatchMu sync.RWMutex
	closed     bool

	closeOnce sync.Once
	closeErr  error
}

// newCodexAppServer performs only process startup and the initialize handshake.
// Thread creation/resume belongs to newCodexSession, never catalog discovery.
func newCodexAppServer(ctx context.Context, opts StartOpts) (*codexSession, error) {
	s := &codexSession{
		hidden:    opts.Hidden,
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
		done:      make(chan struct{}),
	}

	args := []string{"app-server"}
	if opts.MinimalConfig {
		// Process-local overrides retain CODEX_HOME, authentication, provider,
		// sandbox settings and AGENTS.md discovery. Disable plugins before init.
		for _, override := range []string{
			"features.plugins=false", "features.apps=false",
			"features.skip_host_skill_discovery=true", "skills.include_instructions=false",
			"features.memories=false", "features.memory_tool=false",
			"memories.use_memories=false", "memories.generate_memories=false",
		} {
			args = append(args, "-c", override)
		}
	}
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = opts.RepoDir
	cmd.Env = append(filteredEnviron(), opts.Env...)
	cmd.Stderr = opts.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	group := &processGroup{}
	cmd.Cancel = group.kill

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}
	if err := group.start(ctx, cmd); err != nil {
		return nil, fmt.Errorf("codex: start app-server: %w", err)
	}

	s.client = newCodexClient(stdin, stdout, func() error {
		_ = stdin.Close()
		_ = group.kill()
		return nil
	}, s.dispatchEvent, s.dispatchApproval)

	go func() {
		_ = cmd.Wait()
		s.client.rpc.terminate(errors.New("codex: app-server process exited"))
	}()

	if _, err := s.client.initialize(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("codex: initialize: %w", err)
	}

	if err := s.client.initialized(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("codex: initialized: %w", err)
	}

	return s, nil
}

func newCodexSession(ctx context.Context, opts StartOpts, resumeThreadID string) (*codexSession, error) {
	s, err := newCodexAppServer(ctx, opts)
	if err != nil {
		return nil, err
	}
	s.threadID = resumeThreadID
	if resumeThreadID != "" {
		info, id, err := s.client.threadResumeOpts(ctx, resumeThreadID, opts)
		if id != "" {
			s.threadID = id
		}
		if err != nil {
			err = errors.Join(err, s.Close())
			return nil, fmt.Errorf("codex: thread/resume: %w", err)
		}
		if id == "" {
			id = resumeThreadID
		}
		s.threadID = id
		s.info = info
		s.client.setMainThread(id)
	} else {
		info, id, err := s.client.threadStartOpts(ctx, opts)
		s.threadID = id
		if err != nil {
			err = errors.Join(err, s.Close())
			return nil, fmt.Errorf("codex: thread/start: %w", err)
		}
		s.threadID = id
		s.info = info
		s.client.setMainThread(id)
	}
	s.effort = opts.Effort
	s.hidden = opts.Hidden

	return s, nil
}

func (s *codexSession) Prompt(ctx context.Context, msg UserMessage) error {
	if s.hidden {
		s.turnMu.Lock()
		defer s.turnMu.Unlock()
		// Once sent, retain the start response even if cancellation arrives first:
		// Interrupt needs its turn ID. Close bounds a server that never responds.
		return s.client.turnStartEffortContexts(ctx, context.WithoutCancel(ctx), s.threadID, msg.Text, s.effort)
	}
	return s.client.turnStartEffort(ctx, s.threadID, msg.Text, s.effort)
}

func (s *codexSession) Interrupt(ctx context.Context) error {
	if s.hidden {
		s.turnMu.Lock()
		defer s.turnMu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.client.turnInterrupt(ctx, s.threadID)
}

func (s *codexSession) Events() <-chan Event { return s.events }

func (s *codexSession) Approvals() <-chan ApprovalRequest { return s.approvals }

func (s *codexSession) ThreadID() string { return s.threadID }

func (s *codexSession) Info() SessionInfo { return s.info }

// ErrArchive marks a failure to archive a Hidden session's thread on Close: the
// session ended normally but stays visible in the user's thread list.
var ErrArchive = errors.New("archive thread")

// codexArchiveTimeout bounds the archive call in Close, so a wedged app-server
// cannot hold up teardown.
var codexArchiveTimeout = 5 * time.Second

func (s *codexSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.client != nil {
			var archiveErr error
			if s.hidden && s.threadID != "" {
				ctx, cancel := context.WithTimeout(context.Background(), codexArchiveTimeout)
				_, err := s.client.rpc.call(ctx, "thread/archive", map[string]any{"threadId": s.threadID})
				cancel()
				if err != nil {
					archiveErr = fmt.Errorf("codex: %w %s: %w", ErrArchive, s.threadID, err)
				}
			}
			s.closeErr = errors.Join(archiveErr, s.client.Close())
		}
		s.dispatchMu.Lock()
		s.closed = true
		close(s.events)
		close(s.approvals)
		s.dispatchMu.Unlock()
	})
	return s.closeErr
}

func (s *codexSession) dispatchEvent(e Event) {
	s.dispatchMu.RLock()
	defer s.dispatchMu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.events <- e:
	case <-s.done:
	}
}

func (s *codexSession) dispatchApproval(a ApprovalRequest) {
	s.dispatchMu.RLock()
	defer s.dispatchMu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.approvals <- a:
	default:
	}
}

// newCodexSessionWithClient wires a codexSession around an already-constructed
// client. It is used by pipe-backed tests so they can drive the JSON-RPC layer
// without spawning a real subprocess.
func newCodexSessionWithClient(c *codexClient, threadID string) *codexSession {
	if c != nil && threadID != "" {
		c.setMainThread(threadID)
	}
	return &codexSession{
		client:    c,
		threadID:  threadID,
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
		done:      make(chan struct{}),
	}
}

// codexClient is the thin, codex-specific wrapper around the shared rpcConn.
type codexClient struct {
	rpc *rpcConn

	dispatchEvent    func(Event)
	dispatchApproval func(ApprovalRequest)

	mu sync.Mutex
	// turnID is the id of the turn currently in flight. TurnInterruptParams
	// requires it, and it is only knowable from the wire: it is recorded from
	// the (synchronous) turn/start response and from the turn/started
	// notification, and cleared when the turn completes.
	turnID string
	// lastErr is the last error message already surfaced for turnID. A failed
	// turn is reported twice by the protocol — once as the `error`
	// notification and again as turn/completed{status:failed} carrying the
	// same TurnError — and the cockpit stream must show it once.
	lastErr string
	// itemNames maps a live fileChange item's id to its rendered label.
	// FileChangeRequestApprovalParams carries only itemId — the paths under
	// review live on the item announced earlier by item/started — so without
	// this correlation the approval card has no title at all
	// ([CODEX-APPROVAL-NAME]). Entries are dropped when the item completes.
	itemNames map[string]string
	// mainThread is the session's own thread. codex can run sub-agents in child
	// threads on the same connection; their turn ends and replies are not the
	// session's (see handleChildNotification).
	mainThread string
}

// maxTrackedItems bounds itemNames so a long thread whose items never complete
// cannot grow it without limit. Approvals arrive while their item is in flight,
// so a reset only ever costs a stale label, never a wrong one.
const maxTrackedItems = 256

// noteItem records a live item's label for later approval correlation.
func (c *codexClient) noteItem(id, name string) {
	if id == "" || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.itemNames == nil || len(c.itemNames) >= maxTrackedItems {
		c.itemNames = map[string]string{}
	}
	c.itemNames[id] = name
}

// forgetItem drops a completed item's label.
func (c *codexClient) forgetItem(id string) {
	c.mu.Lock()
	delete(c.itemNames, id)
	c.mu.Unlock()
}

// itemName returns the recorded label for id, if the item is still tracked.
func (c *codexClient) itemName(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.itemNames[id]
}

// setTurnID records the in-flight turn. Starting a DIFFERENT turn clears the
// reported-error memo; clearing the id (turn over) deliberately does not, so
// that turn/completed{status:failed} can still recognise the message the
// `error` notification just reported for that same turn.
func (c *codexClient) setTurnID(id string) {
	c.mu.Lock()
	if id != "" && id != c.turnID {
		c.lastErr = ""
	}
	c.turnID = id
	c.mu.Unlock()
}

func (c *codexClient) setMainThread(id string) {
	c.mu.Lock()
	c.mainThread = id
	c.mu.Unlock()
}

// childThread reports whether a notification belongs to a thread other than the
// session's own (a codex sub-agent).
func (c *codexClient) childThread(params json.RawMessage) bool {
	c.mu.Lock()
	main := c.mainThread
	c.mu.Unlock()
	tid := jsonStringAt(params, "threadId")
	return main != "" && tid != "" && tid != main
}

func (c *codexClient) currentTurnID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turnID
}

// noteErr records msg as reported for the current turn and returns false if it
// had already been reported.
func (c *codexClient) noteErr(msg string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if msg != "" && msg == c.lastErr {
		return false
	}
	c.lastErr = msg
	return true
}

func newCodexClient(w io.WriteCloser, r io.Reader, closeFn func() error,
	onEvent func(Event), onApproval func(ApprovalRequest)) *codexClient {
	c := &codexClient{
		dispatchEvent:    onEvent,
		dispatchApproval: onApproval,
	}
	c.rpc = newRPCConn(w, r, closeFn, c.handleServerRequest, c.handleNotification)
	return c
}

func (c *codexClient) initialize(ctx context.Context) (json.RawMessage, error) {
	params := map[string]any{
		"clientInfo": map[string]any{
			"name":    "pactify",
			"version": "0",
			"title":   "pactify",
		},
	}
	return c.rpc.call(ctx, "initialize", params)
}

func (c *codexClient) initialized(ctx context.Context) error {
	return c.rpc.notify("initialized", nil)
}

func sandboxMode(a Access) string {
	switch a {
	case AccessReadOnly:
		return "read-only"
	case AccessWrite:
		return "workspace-write"
	}
	return ""
}

func codexStartParams(opts StartOpts) map[string]any {
	p := map[string]any{"cwd": opts.RepoDir}
	if opts.Model != "" {
		p["model"] = opts.Model
	}
	if m := sandboxMode(opts.Access); m != "" {
		p["sandbox"] = m
		p["approvalPolicy"] = "never"
	}
	if opts.Access == AccessWrite {
		// Write mode may use the network (Owner decision 2026-10-15): delegated work
		// must be able to run a project's full test suite, which often binds local
		// ports, and fetch dependencies. Read-only keeps codex's default (no network).
		sandbox := map[string]any{"network_access": true}
		if len(opts.WritableRoots) > 0 {
			sandbox["writable_roots"] = opts.WritableRoots
		}
		p["config"] = map[string]any{"sandbox_workspace_write": sandbox}
	}
	return p
}

type codexThreadResult struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model           string `json:"model"`
	ModelProvider   string `json:"modelProvider"`
	ReasoningEffort string `json:"reasoningEffort"`
	Sandbox         struct {
		Type string `json:"type"`
	} `json:"sandbox"`
}

func (r codexThreadResult) info() SessionInfo {
	return SessionInfo{Model: r.Model, Provider: r.ModelProvider, Effort: r.ReasoningEffort, Access: r.Sandbox.Type}
}

func (c *codexClient) threadStartOpts(ctx context.Context, opts StartOpts) (SessionInfo, string, error) {
	params, err := c.delegationParams(ctx, opts)
	if err != nil {
		return SessionInfo{}, "", err
	}
	return c.threadOpts(ctx, "thread/start", params, opts.Model)
}

// Read effective configuration before merging cache roots or disabling MCP.
// An empty MCP table merges with the user's table in codex 0.162.1; disable each
// effective name instead. Never write config or log the returned values.
func (c *codexClient) delegationParams(ctx context.Context, opts StartOpts) (map[string]any, error) {
	params := codexStartParams(opts)
	addRoots := opts.Access == AccessWrite && len(opts.WritableRoots) > 0
	if !opts.MinimalConfig && !addRoots {
		return params, nil
	}
	raw, err := c.rpc.call(ctx, "config/read", map[string]any{"cwd": opts.RepoDir, "includeLayers": false})
	if err != nil {
		return nil, fmt.Errorf("codex: delegation configuration: %w", err)
	}
	var res struct {
		Config struct {
			Servers        map[string]json.RawMessage `json:"mcp_servers"`
			WorkspaceWrite struct {
				WritableRoots []string `json:"writable_roots"`
			} `json:"sandbox_workspace_write"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("codex: parse delegation configuration: %w", err)
	}
	config, _ := params["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
	}
	if opts.MinimalConfig {
		servers := make(map[string]any, len(res.Config.Servers))
		for name := range res.Config.Servers {
			servers[name] = map[string]any{"enabled": false}
		}
		// Nested keys preserve literal server names containing dots or quotes.
		config["mcp_servers"] = servers
	}
	if addRoots {
		roots := append([]string(nil), res.Config.WorkspaceWrite.WritableRoots...)
		for _, path := range opts.WritableRoots {
			if !slices.Contains(roots, path) {
				roots = append(roots, path)
			}
		}
		config["sandbox_workspace_write"].(map[string]any)["writable_roots"] = roots
	}
	params["config"] = config
	return params, nil
}

// threadOpts verifies the server's actual model before exposing a usable session.
func (c *codexClient) threadOpts(ctx context.Context, method string, params map[string]any, model string) (SessionInfo, string, error) {
	raw, err := c.rpc.call(ctx, method, params)
	if err != nil {
		if model != "" {
			err = c.modelRequestError(ctx, model, err)
		}
		return SessionInfo{}, "", err
	}
	var res codexThreadResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return SessionInfo{}, "", fmt.Errorf("parse %s result: %w", method, err)
	}
	if model != "" && res.Model != model {
		return res.info(), res.Thread.ID, c.modelRequestError(ctx, model, fmt.Errorf("server reported model %q", res.Model))
	}
	return res.info(), res.Thread.ID, nil
}

// Only explicit model failures query the catalog; default serve traffic is unchanged.
func (c *codexClient) modelRequestError(ctx context.Context, requested string, cause error) error {
	catalog, err := c.modelDetails(ctx)
	if err != nil {
		return fmt.Errorf("codex: requested model %q: %w; could not list available models: %v", requested, cause, err)
	}
	return fmt.Errorf("codex: requested model %q: %w; available models: %s", requested, cause, modelChoices("codex", requested, catalog.Items))
}

func (c *codexClient) threadStart(ctx context.Context, cwd string) (string, error) {
	_, id, err := c.threadStartOpts(ctx, StartOpts{RepoDir: cwd})
	return id, err
}

// threadResume is a request (not a notification), so server failures propagate.
func (c *codexClient) threadResumeOpts(ctx context.Context, threadID string, opts StartOpts) (SessionInfo, string, error) {
	params, err := c.delegationParams(ctx, opts)
	if err != nil {
		return SessionInfo{}, "", err
	}
	if opts.Hidden {
		// codex refuses to resume an archived thread; a hidden job archived it on
		// Close. A thread that is not archived makes this fail, which is fine:
		// thread/resume reports any real problem.
		_, _ = c.rpc.call(ctx, "thread/unarchive", map[string]any{"threadId": threadID})
	}
	// Preserve the original resume frame when no cwd override was provided.
	if opts.RepoDir == "" {
		delete(params, "cwd")
	}
	params["threadId"] = threadID
	return c.threadOpts(ctx, "thread/resume", params, opts.Model)
}

func (c *codexClient) threadResume(ctx context.Context, threadID string) (string, error) {
	_, id, err := c.threadResumeOpts(ctx, threadID, StartOpts{})
	return id, err
}

func (c *codexClient) turnStart(ctx context.Context, threadID, text string) error {
	return c.turnStartEffort(ctx, threadID, text, "")
}

func (c *codexClient) turnStartEffort(ctx context.Context, threadID, text, effort string) error {
	return c.turnStartEffortContexts(ctx, ctx, threadID, text, effort)
}
func (c *codexClient) turnStartEffortContexts(sendCtx, responseCtx context.Context, threadID, text, effort string) error {
	params := map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": text}},
	}
	if effort != "" {
		params["effort"] = effort
	}
	raw, err := c.rpc.callContexts(sendCtx, responseCtx, "turn/start", params)
	if err != nil {
		return err
	}
	// TurnStartResponse carries the Turn. Recording the id here — rather than
	// waiting for the turn/started notification — means an interrupt issued the
	// instant after Prompt returns already has a turn to name.
	var res struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &res) == nil && res.Turn.ID != "" {
		c.setTurnID(res.Turn.ID)
	}
	return nil
}

// turnInterrupt cancels the in-flight turn.
//
// turn/interrupt is a ClientRequest and TurnInterruptParams requires BOTH
// threadId and turnId; the old id-less, turnId-less notification could not have
// interrupted anything. With no turn id there is no turn running (the id is
// recorded from the turn/start response before Prompt returns), so interrupting
// is a no-op rather than a request the server would reject.
func (c *codexClient) turnInterrupt(ctx context.Context, threadID string) error {
	turnID := c.currentTurnID()
	if turnID == "" {
		return nil
	}
	_, err := c.rpc.call(ctx, "turn/interrupt", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})
	return err
}

func (c *codexClient) Close() error {
	return c.rpc.Close()
}

func (c *codexClient) handleServerRequest(id json.RawMessage, method string, params json.RawMessage) {
	switch method {
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/permissions/requestApproval":
		c.handleApprovalRequest(id, method, params)
	}
}

func (c *codexClient) handleApprovalRequest(id json.RawMessage, method string, params json.RawMessage) {
	kind := approvalKindFromMethod(method)

	toolName := c.approvalToolName(kind, params)

	var once sync.Once
	var innerErr error
	req := ApprovalRequest{
		Kind:     kind,
		ToolName: toolName,
		RawInput: params,
		Respond: func(d Decision) error {
			triggered := false
			once.Do(func() {
				triggered = true
				innerErr = c.replyApproval(id, kind, params, d)
			})
			if !triggered {
				return errors.New("approval already responded")
			}
			return innerErr
		},
	}

	if c.dispatchApproval != nil {
		c.dispatchApproval(req)
	}
}

// replyApproval answers a server-initiated approval request.
//
// The three approval kinds do NOT share a response shape:
//   - CommandExecutionRequestApprovalResponse = {decision}
//   - FileChangeRequestApprovalResponse       = {decision}
//   - PermissionsRequestApprovalResponse      = {permissions, scope,
//     strictAutoReview} — it has no `decision` field at all, so answering a
//     permissions prompt with {"decision": …} is structurally invalid.
func (c *codexClient) replyApproval(id json.RawMessage, kind string, params json.RawMessage, d Decision) error {
	if kind == "permission" {
		return c.replyPermissionsApproval(id, params, d)
	}
	decision := decisionString(d)
	if decision == "" {
		return fmt.Errorf("codex: unsupported decision %q", d)
	}
	return c.rpc.reply(id, map[string]any{"decision": decision})
}

// replyPermissionsApproval answers item/permissions/requestApproval with a
// GrantedPermissionProfile. Granting means handing back the very profile that
// was requested (RequestPermissionProfile and GrantedPermissionProfile are the
// same {fileSystem, network} shape); denying means granting an empty profile.
// PermissionGrantScope distinguishes a one-turn grant from a session-wide one.
func (c *codexClient) replyPermissionsApproval(id json.RawMessage, params json.RawMessage, d Decision) error {
	scope := "turn"
	grant := json.RawMessage("{}")

	switch d {
	case DecisionAllow:
	case DecisionAllowForSession:
		scope = "session"
	case DecisionDeny:
		// Grant nothing; scope stays "turn" so the refusal is not cached.
		return c.rpc.reply(id, map[string]any{"permissions": grant, "scope": scope})
	default:
		return fmt.Errorf("codex: unsupported decision %q", d)
	}

	var p struct {
		Permissions json.RawMessage `json:"permissions"`
	}
	if json.Unmarshal(params, &p) == nil && len(p.Permissions) > 0 {
		grant = p.Permissions
	}
	return c.rpc.reply(id, map[string]any{"permissions": grant, "scope": scope})
}

// approvalToolName renders the approval card's title. Each approval variant
// keeps its identity somewhere different, and only the command one has a
// `command` field — reading command/tool/name for all three left fileChange and
// permissions cards blank, so the user could only guess from RawInput what they
// were approving ([CODEX-APPROVAL-NAME]).
func (c *codexClient) approvalToolName(kind string, params json.RawMessage) string {
	var p struct {
		Command     string                     `json:"command"`
		Tool        string                     `json:"tool"`
		Name        string                     `json:"name"`
		ItemID      string                     `json:"itemId"`
		Reason      string                     `json:"reason"`
		Permissions map[string]json.RawMessage `json:"permissions"`
	}
	_ = json.Unmarshal(params, &p)

	switch kind {
	case "file_change":
		// The paths live on the fileChange item announced by item/started.
		if name := c.itemName(p.ItemID); name != "" {
			return name
		}
		if p.Reason != "" {
			return "file change: " + p.Reason
		}
		return "file change"
	case "permission":
		// RequestPermissionProfile — which classes of access are being asked for.
		if classes := permissionClasses(p.Permissions); len(classes) > 0 {
			name := "permissions: " + strings.Join(classes, ", ")
			if p.Reason != "" {
				name += " (" + p.Reason + ")"
			}
			return name
		}
		if p.Reason != "" {
			return "permissions: " + p.Reason
		}
		return "permissions"
	}

	// commandExecution: `command` is the real field; tool/name are kept as
	// tolerant fallbacks for shapes this client has not seen.
	for _, s := range []string{p.Command, p.Tool, p.Name} {
		if s != "" {
			return s
		}
	}
	if p.Reason != "" {
		return p.Reason
	}
	return ""
}

// permissionClasses lists the non-null keys of a RequestPermissionProfile
// (fileSystem / network), sorted so the label is stable.
func permissionClasses(perms map[string]json.RawMessage) []string {
	var out []string
	for k, v := range perms {
		if len(v) == 0 || string(v) == "null" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func approvalKindFromMethod(method string) string {
	switch {
	case strings.HasSuffix(method, "commandExecution/requestApproval"):
		return "command"
	case strings.HasSuffix(method, "fileChange/requestApproval"):
		return "file_change"
	case strings.HasSuffix(method, "permissions/requestApproval"):
		return "permission"
	}
	return ""
}

func decisionString(d Decision) string {
	switch d {
	case DecisionAllow:
		return "accept"
	case DecisionAllowForSession:
		return "acceptForSession"
	case DecisionDeny:
		return "decline"
	}
	return ""
}

// handleChildNotification handles a sub-agent thread's notification. Its work
// still counts (tool calls, token usage), but it never ends the session's turn,
// never supplies the final reply and never takes over the in-flight turn id; its
// replies and turn ends reach the event log as system events.
func (c *codexClient) handleChildNotification(method string, params, raw json.RawMessage) {
	switch method {
	case "item/started":
		if item, ok := decodeCodexItem(params); ok && item.isTool() {
			c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: item.displayName()}, Raw: raw})
		}
	case "item/completed":
		if item, ok := decodeCodexItem(params); ok {
			switch {
			case item.Type == "agentMessage":
				c.dispatchEvent(Event{Kind: EventSystem, State: "subagent_message", Text: item.Text, Raw: raw})
			case item.isTool():
				c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: item.displayName()}, Raw: raw})
			}
		}
	case "thread/tokenUsage/updated":
		if u := parseUsageFromParams(params); u != nil {
			c.dispatchEvent(Event{Kind: EventUsage, Usage: u, Raw: raw})
		}
	case "turn/completed":
		c.dispatchEvent(Event{Kind: EventSystem, State: "subagent_turn_completed", Raw: raw})
	}
}

func (c *codexClient) handleNotification(method string, params json.RawMessage) {
	if c.dispatchEvent == nil {
		return
	}

	raw := cloneRaw(params)
	if c.childThread(params) {
		c.handleChildNotification(method, params, raw)
		return
	}

	switch method {
	case "item/agentMessage/delta":
		// AgentMessageDeltaNotification.delta is a plain string, not an object.
		c.dispatchEvent(Event{Kind: EventMessage, Text: jsonStringAt(params, "delta"), Final: false, Raw: raw})

	case "item/completed":
		if item, ok := decodeCodexItem(params); ok {
			switch {
			case item.Type == "agentMessage":
				c.dispatchEvent(Event{Kind: EventMessage, Final: true, Text: item.Text, Raw: raw})
			case item.isTool():
				c.forgetItem(item.ID)
				c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "end", Name: item.displayName()}, Raw: raw})
			}
		}

	case "item/commandExecution/outputDelta":
		// CommandExecutionOutputDeltaNotification.delta is a plain string too.
		c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "output", Text: jsonStringAt(params, "delta")}, Raw: raw})

	case "item/started":
		if item, ok := decodeCodexItem(params); ok && item.isTool() {
			// Remember the label: a fileChange approval that arrives later
			// identifies its subject by this item's id and nothing else.
			c.noteItem(item.ID, item.displayName())
			c.dispatchEvent(Event{Kind: EventTool, Tool: &ToolEvent{Phase: "start", Name: item.displayName()}, Raw: raw})
		}

	case "account/rateLimits/updated":
		c.dispatchEvent(Event{Kind: EventSystem, State: "rate_limits", Raw: raw})

	case "thread/tokenUsage/updated":
		if u := parseUsageFromParams(params); u != nil {
			c.dispatchEvent(Event{Kind: EventUsage, Usage: u, Raw: raw})
		}

	case "turn/started":
		c.setTurnID(jsonStringAt(params, "turn", "id"))
		c.dispatchEvent(Event{Kind: EventState, State: "turn_started", Raw: raw})

	case "turn/completed":
		// turn/completed is the ONLY terminal turn notification: there is no
		// turn/failed method in this protocol. Failure arrives here as
		// turn.status == "failed" with turn.error populated.
		c.setTurnID("")
		if jsonStringAt(params, "turn", "status") == "failed" {
			summary := jsonStringAt(params, "turn", "error", "message")
			if summary == "" {
				summary = "codex: turn failed"
			}
			c.dispatchEvent(Event{Kind: EventState, State: StateTurnFailed, Raw: raw})
			// The `error` notification usually reported this already.
			if c.noteErr(summary) {
				c.dispatchEvent(Event{Kind: EventError, Err: summary, Raw: raw})
			}
			return
		}
		// "completed" and "interrupted" are both terminal and non-error.
		c.dispatchEvent(Event{Kind: EventState, State: StateTurnCompleted, Raw: raw})

	case "error":
		// ErrorNotification.error is a TurnError, whose only required field is
		// `message`.
		summary := jsonStringAt(params, "error", "message")
		if summary == "" {
			summary = jsonStringAt(params, "message")
		}
		var notice struct {
			WillRetry bool `json:"willRetry"`
		}
		_ = json.Unmarshal(raw, &notice)
		if notice.WillRetry {
			c.dispatchEvent(Event{Kind: EventSystem, State: "codex_retry", Text: summary, Raw: raw})
			return
		}
		if c.noteErr(summary) {
			c.dispatchEvent(Event{Kind: EventError, Err: summary, Raw: raw})
		}

	case "turn/diff/updated":
		c.dispatchEvent(Event{Kind: EventDiff, Raw: raw})

	case "turn/plan/updated":
		c.dispatchEvent(Event{Kind: EventPlan, Raw: raw})
	}
}

func cloneRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(r))
	copy(out, r)
	return out
}

// jsonStringAt walks keys through a decoded JSON object and returns the first
// string it reaches.
//
// Note the "first string it reaches" part: traversal STOPS as soon as a hop
// yields a string, so trailing keys are ignored. jsonStringAt(p, "delta",
// "text") therefore returns p.delta whenever delta is a plain string — which
// is why codex.go reading the (non-existent) delta.text still produced correct
// streaming text, and why the wrong path went unnoticed for so long. Callers
// must pass the real path anyway: a lenient lookup that happens to work is not
// a mapping anyone can reason about.
func jsonStringAt(params json.RawMessage, keys ...string) string {
	if len(params) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(params, &v); err != nil {
		return ""
	}
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		child, ok := m[k]
		if !ok {
			return ""
		}
		switch x := child.(type) {
		case string:
			return x
		case map[string]any:
			v = x
		default:
			return ""
		}
	}
	return ""
}

// codexThreadItem is the subset of v2/ThreadItem the cockpit surfaces. Only
// message/tool variants contribute fields here; each field
// exists on exactly one variant, so decoding them together is unambiguous.
type codexThreadItem struct {
	Type string `json:"type"`
	Text string `json:"text"` // agentMessage
	// ID is on every ThreadItem variant; it is the key a later
	// requestApproval uses to point back at this item.
	ID      string `json:"id"`
	Command string `json:"command"` // commandExecution
	Server  string `json:"server"`  // mcpToolCall
	Tool    string `json:"tool"`    // mcpToolCall
	Changes []struct {
		Path string `json:"path"`
	} `json:"changes"` // fileChange
}

// decodeCodexItem pulls the ThreadItem out of an item/started or item/completed
// notification.
func decodeCodexItem(params json.RawMessage) (codexThreadItem, bool) {
	var p struct {
		Item *codexThreadItem `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Item == nil {
		return codexThreadItem{}, false
	}
	return *p.Item, true
}

// isTool reports whether this item belongs on the cockpit's tool timeline.
// The MCP variant tag is "mcpToolCall" — "mcpTool" is not a ThreadItem type and
// never matched anything.
func (it codexThreadItem) isTool() bool {
	switch it.Type {
	case "commandExecution", "fileChange", "mcpToolCall":
		return true
	}
	return false
}

// displayName renders the one-line label shown for a tool item. Each variant
// keeps its name somewhere different: commandExecution has `command`,
// mcpToolCall has `server`/`tool`, and fileChange has neither — its only
// identifying data is the `changes` array of {path, kind, diff}.
func (it codexThreadItem) displayName() string {
	switch it.Type {
	case "commandExecution":
		return it.Command
	case "mcpToolCall":
		switch {
		case it.Server != "" && it.Tool != "":
			return it.Server + "/" + it.Tool
		case it.Tool != "":
			return it.Tool
		default:
			return it.Server
		}
	case "fileChange":
		if len(it.Changes) == 0 {
			return ""
		}
		name := it.Changes[0].Path
		if rest := len(it.Changes) - 1; rest > 0 {
			name = fmt.Sprintf("%s (+%d more)", name, rest)
		}
		return name
	}
	return ""
}

// parseUsageFromParams reads a ThreadTokenUsageUpdatedNotification.
//
// The counters are nested: tokenUsage is a ThreadTokenUsage
// {last, total, modelContextWindow} whose last/total are TokenUsageBreakdown
// {cachedInputTokens, inputTokens, outputTokens, reasoningOutputTokens,
// totalTokens}. Nothing is flat at the top level.
//
// `last` is the right bucket, not `total`: every consumer of EventUsage
// ACCUMULATES (internal/serve/cockpit_remote.go sums Usage across events, and
// the acp/claude backends emit per-update deltas), while `total` is the
// thread's running sum. Summing `total` would double-count — in a captured
// two-request turn codex reported total=15264 then total=30572, whose sum
// (45836) exceeds the true 30572, while the `last` values 15264 + 15308 add up
// to exactly 30572.
//
// The protocol carries no cost in this notification, so CostUSD stays 0 rather
// than being invented.
func parseUsageFromParams(params json.RawMessage) *Usage {
	if len(params) == 0 {
		return nil
	}
	var p struct {
		TokenUsage *struct {
			Last *struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
				TotalTokens  int `json:"totalTokens"`
			} `json:"last"`
		} `json:"tokenUsage"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.TokenUsage == nil || p.TokenUsage.Last == nil {
		return nil
	}
	last := p.TokenUsage.Last
	return &Usage{
		InputTokens:  last.InputTokens,
		OutputTokens: last.OutputTokens,
		TotalTokens:  last.TotalTokens,
	}
}

var codexFailureRules = []failureRule{
	{"task canceled", "canceled"},
	{"usage limit", "quota"}, {"rate limit", "quota"},
	{"unauthorized", "auth"},
	{"requested model is not offered by this agent", "model"},
	{"model is not supported when using codex with a chatgpt account", "model"},
}

type codexFailureWindow struct {
	UsedPercent float64 `json:"usedPercent"`
	ResetsAt    int64   `json:"resetsAt"`
	LegacyReset int64   `json:"resets_at"`
}

func codexFailure(message string, raw json.RawMessage, _ time.Time) Failure {
	var p struct {
		RateLimits struct {
			Primary, Secondary *codexFailureWindow
		} `json:"rateLimits"`
		Error struct {
			Info json.RawMessage `json:"codexErrorInfo"`
		} `json:"error"`
		Turn struct {
			Error struct {
				Info json.RawMessage `json:"codexErrorInfo"`
			} `json:"error"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &p) == nil {
		f := Failure{}
		unknownReset := false
		for _, w := range []*codexFailureWindow{p.RateLimits.Primary, p.RateLimits.Secondary} {
			if w == nil || w.UsedPercent < 100 {
				continue
			}
			f.Class = "quota"
			reset := max(w.ResetsAt, w.LegacyReset)
			if reset <= 0 {
				unknownReset = true
				continue
			}
			at := time.Unix(reset, 0).UTC()
			if f.ResetAt == nil || at.After(*f.ResetAt) {
				f.ResetAt = &at
			}
		}
		if f.Class != "" {
			if unknownReset {
				f.ResetAt = nil
			}
			return f
		}
		info := p.Error.Info
		if len(p.Turn.Error.Info) > 0 {
			info = p.Turn.Error.Info
		}
		var kind string
		if json.Unmarshal(info, &kind) == nil {
			switch kind {
			case "usageLimitExceeded", "rateLimitExceeded":
				return Failure{Class: "quota"}
			case "unauthorized":
				return Failure{Class: "auth"}
			case "internalServerError", "serverOverloaded", "flexUnavailable":
				return Failure{Class: "transient"}
			}
		}
		var variants map[string]struct {
			HTTPStatusCode int `json:"httpStatusCode"`
		}
		if json.Unmarshal(info, &variants) == nil {
			for kind, v := range variants {
				switch {
				case v.HTTPStatusCode == 401:
					return Failure{Class: "auth"}
				case v.HTTPStatusCode == 429:
					return Failure{Class: "quota"}
				case v.HTTPStatusCode >= 500 && v.HTTPStatusCode <= 599:
					return Failure{Class: "transient"}
				case kind == "responseStreamDisconnected" || kind == "responseStreamConnectionFailed":
					return Failure{Class: "transient"}
				}
			}
		}
	}
	return matchFailure(message, codexFailureRules)
}
