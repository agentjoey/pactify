package cockpit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// notify sends a server->client notification (no id).
func (s *fakeCodexServer) notify(method string, params map[string]any) {
	s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func awaitApproval(t *testing.T, srv *fakeCodexServer) ApprovalRequest {
	t.Helper()
	select {
	case req := <-srv.approvals:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("no approval surfaced")
	}
	return ApprovalRequest{}
}

// [CODEX-APPROVAL-NAME]: FileChangeRequestApprovalParams carries no command /
// tool / name — only itemId. The paths live on the fileChange item announced
// earlier by item/started, so the card must resolve the name through itemId
// instead of showing a blank title the user cannot act on.
func TestFileChangeApprovalNamesTheFilesFromTheStartedItem(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()

	srv.notify("item/started", map[string]any{
		"threadId": "t", "turnId": "u", "startedAtMs": 1,
		"item": map[string]any{
			"type": "fileChange", "id": "exec-9bc110cc", "status": "inProgress",
			"changes": []any{
				map[string]any{"path": "web/src/App.tsx"},
				map[string]any{"path": "web/src/api.ts"},
			},
		},
	})
	// Captured shape: no command/tool/name anywhere in the params.
	srv.request(7, "item/fileChange/requestApproval", json.RawMessage(
		`{"threadId":"t","turnId":"u","itemId":"exec-9bc110cc","startedAtMs":1787578784884,"reason":null,"grantRoot":null}`))

	req := awaitApproval(t, srv)

	if !strings.Contains(req.ToolName, "web/src/App.tsx") {
		t.Errorf("ToolName must name the file under review, got %q", req.ToolName)
	}
	if !strings.Contains(req.ToolName, "more") {
		t.Errorf("ToolName must say more files are involved, got %q", req.ToolName)
	}
}

// The correlating item may be gone (restart, resumed thread). The card still
// has to say what class of approval this is rather than nothing at all.
func TestFileChangeApprovalFallsBackWhenItemUnknown(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()

	srv.request(7, "item/fileChange/requestApproval", json.RawMessage(
		`{"threadId":"t","turnId":"u","itemId":"never-seen","startedAtMs":1,"reason":"needs write access outside the repo","grantRoot":"/tmp"}`))

	req := awaitApproval(t, srv)

	if req.ToolName == "" {
		t.Fatal("ToolName must never be empty on a file-change approval")
	}
	if !strings.Contains(req.ToolName, "needs write access outside the repo") {
		t.Errorf("with no item to correlate, the reason is the only information there is, got %q", req.ToolName)
	}
}

// PermissionsRequestApprovalParams has no command/tool/name either; what it
// does carry is the profile being requested.
func TestPermissionsApprovalNamesWhatIsBeingGranted(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()

	srv.request(7, "item/permissions/requestApproval", json.RawMessage(
		`{"threadId":"t","turnId":"u","itemId":"item-1","cwd":"/tmp/repo","startedAtMs":1,"reason":"needs network","permissions":{"network":{"enabled":true}}}`))

	req := awaitApproval(t, srv)

	if req.ToolName == "" {
		t.Fatal("ToolName must never be empty on a permissions approval")
	}
	if !strings.Contains(req.ToolName, "network") {
		t.Errorf("ToolName must name the permission class being requested, got %q", req.ToolName)
	}
}

// Regression: the command variant does carry `command`, and that must keep
// winning over any of the new fallbacks.
func TestCommandApprovalStillNamesTheCommand(t *testing.T) {
	srv := newFakeCodexServer(t)
	defer srv.Close()

	srv.request(7, "item/commandExecution/requestApproval", json.RawMessage(
		`{"threadId":"t","turnId":"u","itemId":"exec-03fcfee3","startedAtMs":1,"command":"/bin/zsh -lc 'wc -l note.txt'","cwd":"/tmp/repo"}`))

	req := awaitApproval(t, srv)

	if !strings.Contains(req.ToolName, "wc -l note.txt") {
		t.Errorf("ToolName = %q, want the command", req.ToolName)
	}
}
