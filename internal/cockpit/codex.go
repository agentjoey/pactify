package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
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
	return newCodexSession(ctx, opts.RepoDir, "")
}

// Resume spawns codex app-server and resumes an existing thread.
func (b *codexBackend) Resume(ctx context.Context, threadID string) (Session, error) {
	return newCodexSession(ctx, "", threadID)
}

// codexSession owns one codex app-server process and its JSON-RPC connection.
type codexSession struct {
	client *codexClient

	threadID string

	events    chan Event
	approvals chan ApprovalRequest

	closeOnce sync.Once
	closeErr  error
}

func newCodexSession(ctx context.Context, repoDir, resumeThreadID string) (*codexSession, error) {
	s := &codexSession{
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
	}

	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.Dir = repoDir
	cmd.Env = filteredEnviron()
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { killGroup(cmd); return nil }

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex: start app-server: %w", err)
	}

	s.client = newCodexClient(stdin, stdout, func() error {
		_ = stdin.Close()
		killGroup(cmd)
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

	if resumeThreadID != "" {
		id, err := s.client.threadResume(ctx, resumeThreadID)
		if err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("codex: thread/resume: %w", err)
		}
		if id == "" {
			id = resumeThreadID
		}
		s.threadID = id
	} else {
		id, err := s.client.threadStart(ctx, repoDir)
		if err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("codex: thread/start: %w", err)
		}
		s.threadID = id
	}

	return s, nil
}

func (s *codexSession) Prompt(ctx context.Context, msg UserMessage) error {
	return s.client.turnStart(ctx, s.threadID, msg.Text)
}

func (s *codexSession) Interrupt(ctx context.Context) error {
	return s.client.turnInterrupt(ctx, s.threadID)
}

func (s *codexSession) Events() <-chan Event { return s.events }

func (s *codexSession) Approvals() <-chan ApprovalRequest { return s.approvals }

func (s *codexSession) ThreadID() string { return s.threadID }

func (s *codexSession) Close() error {
	s.closeOnce.Do(func() {
		if s.client != nil {
			s.closeErr = s.client.Close()
		}
		close(s.events)
		close(s.approvals)
	})
	return s.closeErr
}

func (s *codexSession) dispatchEvent(e Event) {
	select {
	case s.events <- e:
	default:
	}
}

func (s *codexSession) dispatchApproval(a ApprovalRequest) {
	select {
	case s.approvals <- a:
	default:
	}
}

// newCodexSessionWithClient wires a codexSession around an already-constructed
// client. It is used by pipe-backed tests so they can drive the JSON-RPC layer
// without spawning a real subprocess.
func newCodexSessionWithClient(c *codexClient, threadID string) *codexSession {
	return &codexSession{
		client:    c,
		threadID:  threadID,
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 16),
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

func (c *codexClient) threadStart(ctx context.Context, cwd string) (string, error) {
	params := map[string]any{"cwd": cwd}
	raw, err := c.rpc.call(ctx, "thread/start", params)
	if err != nil {
		return "", err
	}
	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("parse thread/start result: %w", err)
	}
	return res.Thread.ID, nil
}

// threadResume resumes an existing thread and returns the resumed thread's id.
//
// thread/resume is a ClientRequest (ThreadResumeParams -> ThreadResumeResponse),
// not a notification: sent id-less the server never answers, so a resume that
// failed — unknown thread, wrong cwd — was indistinguishable from one that
// worked, and the session went on to prompt a thread that did not exist.
func (c *codexClient) threadResume(ctx context.Context, threadID string) (string, error) {
	raw, err := c.rpc.call(ctx, "thread/resume", map[string]any{"threadId": threadID})
	if err != nil {
		return "", err
	}
	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("parse thread/resume result: %w", err)
	}
	return res.Thread.ID, nil
}

func (c *codexClient) turnStart(ctx context.Context, threadID, text string) error {
	params := map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": text}},
	}
	raw, err := c.rpc.call(ctx, "turn/start", params)
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

func (c *codexClient) handleNotification(method string, params json.RawMessage) {
	if c.dispatchEvent == nil {
		return
	}

	raw := cloneRaw(params)

	switch method {
	case "item/agentMessage/delta":
		// AgentMessageDeltaNotification.delta is a plain string, not an object.
		c.dispatchEvent(Event{Kind: EventMessage, Text: jsonStringAt(params, "delta"), Final: false, Raw: raw})

	case "item/completed":
		if item, ok := decodeCodexItem(params); ok {
			switch {
			case item.Type == "agentMessage":
				c.dispatchEvent(Event{Kind: EventMessage, Final: true, Raw: raw})
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
			c.dispatchEvent(Event{Kind: EventState, State: "turn_failed", Raw: raw})
			summary := jsonStringAt(params, "turn", "error", "message")
			if summary == "" {
				summary = "codex: turn failed"
			}
			// The `error` notification usually reported this already.
			if c.noteErr(summary) {
				c.dispatchEvent(Event{Kind: EventError, Err: summary, Raw: raw})
			}
			return
		}
		// "completed" and "interrupted" are both terminal and non-error.
		c.dispatchEvent(Event{Kind: EventState, State: "turn_completed", Raw: raw})

	case "error":
		// ErrorNotification.error is a TurnError, whose only required field is
		// `message`.
		summary := jsonStringAt(params, "error", "message")
		if summary == "" {
			summary = jsonStringAt(params, "message")
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
// variants that belong on the tool timeline contribute fields here; each field
// exists on exactly one variant, so decoding them together is unambiguous.
type codexThreadItem struct {
	Type string `json:"type"`
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
