package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The peer is a real stdio subprocess that replays what Cursor CLI 2026.11.17
// answered without a login (testdata/cursor) and scripts the logged-in paths,
// which could not be captured without credentials.
func cursorFixture(t *testing.T, path string) (initialize, newUnauthenticated json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Initialize json.RawMessage `json:"initialize"`
		New        json.RawMessage `json:"sessionNewUnauthenticated"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f.Initialize, f.New
}

func TestCursorPeer(t *testing.T) {
	if os.Getenv("CURSOR_PEER") != "1" {
		return
	}
	initialize, unauthenticated := cursorFixture(t, os.Getenv("CURSOR_PEER_FIXTURE"))
	logPath := os.Getenv("CURSOR_PEER_LOG")
	logf := func(format string, args ...any) {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err == nil {
			fmt.Fprintf(f, format+"\n", args...)
			f.Close()
		}
	}
	logf("env CURSOR_CONFIG_DIR=%s", os.Getenv("CURSOR_CONFIG_DIR"))
	mode := os.Getenv("CURSOR_PEER_MODE") // "", "silent-auth", "no-ask"
	out := json.NewEncoder(os.Stdout)
	reply := func(id json.RawMessage, result any) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(in.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "" {
			logf("response id=%s result=%s error=%s", m.ID, m.Result, m.Error)
			continue
		}
		logf("request %s %s", m.Method, m.Params)
		switch m.Method {
		case "initialize":
			var res any
			_ = json.Unmarshal(initialize, &res)
			reply(m.ID, res)
		case "authenticate":
			if mode != "silent-auth" {
				reply(m.ID, map[string]any{})
			}
		case "session/new":
			if mode == "silent-auth" {
				var e any
				_ = json.Unmarshal(unauthenticated, &e)
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": e})
				continue
			}
			modes := []map[string]any{{"id": "agent"}, {"id": "ask"}, {"id": "plan"}}
			if mode == "no-ask" {
				modes = modes[:1]
			}
			reply(m.ID, map[string]any{"sessionId": "cursor-chat-1",
				"modes": map[string]any{"currentModeId": "agent", "availableModes": modes},
				"configOptions": []map[string]any{{"id": "model", "category": "model", "currentValue": "auto",
					"options": []map[string]any{{"value": "auto"}, {"value": "composer-2"}}}}})
		case "session/set_mode", "session/load":
			reply(m.ID, map[string]any{})
		case "session/set_config_option":
			var p struct{ Value string }
			_ = json.Unmarshal(m.Params, &p)
			reply(m.ID, map[string]any{"configOptions": []map[string]any{{"id": "model", "category": "model", "currentValue": p.Value}}})
		case "session/prompt":
			update := func(u map[string]any) {
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "cursor-chat-1", "update": u}})
			}
			// Cursor's blocking extension requests must be answered before the turn can continue.
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "cursor/ask_question", "params": map[string]any{"toolCallId": "q1", "questions": []any{}}})
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": 901, "method": "cursor/create_plan", "params": map[string]any{"toolCallId": "p1", "plan": "x"}})
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": 902, "method": "cursor/task", "params": map[string]any{"toolCallId": "t1"}})
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": 903, "method": "session/request_permission", "params": map[string]any{
				"sessionId": "cursor-chat-1", "toolCall": map[string]any{"toolCallId": "c1", "title": "Run ls"},
				"options": []map[string]any{{"optionId": "allow-once", "kind": "allow_once"}, {"optionId": "reject-once", "kind": "reject_once"}}}})
			update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "done"}})
			time.Sleep(300 * time.Millisecond)
			reply(m.ID, map[string]any{"stopReason": "end_turn"})
		}
	}
	os.Exit(0)
}

func setupCursorBin(t *testing.T, name, mode string) (logPath string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	version, err := os.ReadFile("testdata/cursor/version.txt")
	if err != nil {
		t.Fatal(err)
	}
	help, err := filepath.Abs("testdata/cursor/help.txt")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	quoted := "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n--version) echo %s ;;\n--help) cat %q ;;\nacp) exec %s -test.run='^TestCursorPeer$' ;;\nesac\n", strings.TrimSpace(string(version)), help, quoted)
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	logPath = filepath.Join(t.TempDir(), "peer.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	fixture, err := filepath.Abs("testdata/cursor/acp-unauthenticated.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_PEER_FIXTURE", fixture)
	t.Setenv("CURSOR_PEER", "1")
	t.Setenv("CURSOR_PEER_LOG", logPath)
	t.Setenv("CURSOR_PEER_MODE", mode)
	return logPath
}

func readPeerLog(t *testing.T, path string) string {
	t.Helper()
	data, _ := os.ReadFile(path)
	return string(data)
}

func TestResolveCursorBinary(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"cursor-agent", "agent"} {
		t.Run(name, func(t *testing.T) {
			setupCursorBin(t, name, "")
			path, version, err := ResolveCursorBinary(ctx)
			if err != nil || filepath.Base(path) != name || version != "2026.11.17-f532c45" {
				t.Fatalf("%q %q %v", path, version, err)
			}
		})
	}
}

func TestResolveCursorBinaryRejectsForeignAgent(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo 2026.11.17-f532c45 ;;\n--help) echo 'Usage: agent [options]\nSome other agent' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "agent"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	if _, _, err := ResolveCursorBinary(context.Background()); err == nil || !strings.Contains(err.Error(), "does not identify Cursor Agent") {
		t.Fatalf("a foreign agent was accepted: %v", err)
	}
	foreign := "#!/bin/sh\necho 1.2.3\n"
	if err := os.WriteFile(filepath.Join(bin, "agent"), []byte(foreign), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin:")
	if _, _, err := ResolveCursorBinary(context.Background()); err == nil || !strings.Contains(err.Error(), "unexpected --version") {
		t.Fatalf("a foreign version was accepted: %v", err)
	}
}

func TestResolveCursorBinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, _, err := ResolveCursorBinary(context.Background()); err == nil {
		t.Fatal("missing binary resolved")
	}
}

func TestCursorProbeUsesPrivateConfigDir(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "dir")
	script := "#!/bin/sh\nprintf '%s' \"$CURSOR_CONFIG_DIR\" > " + marker + "\ncase \"$1\" in\n--version) echo 2026.11.17-f532c45 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "cursor-agent"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	user := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", user)
	t.Setenv("PATH", bin+":/usr/bin:/bin:/usr/sbin")
	if _, _, err := ResolveCursorBinary(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(marker)
	if string(got) == "" || string(got) == user {
		t.Fatalf("probe touched the user's config dir: %q", got)
	}
}

func TestCursorUnauthenticatedHandshakeClassifiesAsAuth(t *testing.T) {
	old := cursorAuthTimeout
	cursorAuthTimeout = 200 * time.Millisecond
	t.Cleanup(func() { cursorAuthTimeout = old })
	logPath := setupCursorBin(t, "cursor-agent", "silent-auth")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := NewCursorBackend().Start(ctx, StartOpts{RepoDir: t.TempDir()})
	if err == nil {
		t.Fatal("start succeeded without a login")
	}
	if f := FailureFromError("cursor", err, time.Now()); f.Class != "auth" {
		t.Fatalf("class=%q err=%v", f.Class, err)
	}
	log := readPeerLog(t, logPath)
	if !strings.Contains(log, `request authenticate {"methodId":"cursor_login"}`) {
		t.Fatalf("no authenticate call:\n%s", log)
	}
}

func TestCursorStartModesAndModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   StartOpts
		wantIn []string
		wantNo []string
		access string
	}{
		{"write keeps agent mode", StartOpts{Access: AccessWrite}, nil, []string{"session/set_mode"}, ""},
		{"read-only selects ask", StartOpts{Access: AccessReadOnly}, []string{`request session/set_mode {"modeId":"ask"`}, nil, "ask"},
		{"model is set", StartOpts{Model: "composer-2"}, []string{`request session/set_config_option {"configId":"model","sessionId":"cursor-chat-1","value":"composer-2"}`}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logPath := setupCursorBin(t, "cursor-agent", "")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tc.opts.RepoDir = t.TempDir()
			tc.opts.Env = []string{"CURSOR_CONFIG_DIR=/managed/cursor"}
			s, err := NewCursorBackend().Start(ctx, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			log := readPeerLog(t, logPath)
			for _, want := range append(tc.wantIn, "env CURSOR_CONFIG_DIR=/managed/cursor") {
				if !strings.Contains(log, want) {
					t.Fatalf("missing %q in:\n%s", want, log)
				}
			}
			for _, no := range tc.wantNo {
				if strings.Contains(log, no) {
					t.Fatalf("unexpected %q in:\n%s", no, log)
				}
			}
			if s.ThreadID() != "cursor-chat-1" {
				t.Fatalf("thread=%q", s.ThreadID())
			}
			if info := s.(InfoSession).Info(); tc.access != "" && info.Access != tc.access {
				t.Fatalf("info=%+v", info)
			}
		})
	}
}

func TestCursorReadOnlyFailsClosedWithoutAskMode(t *testing.T) {
	setupCursorBin(t, "cursor-agent", "no-ask")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := NewCursorBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly})
	if !errors.Is(err, ErrReadOnlyUnenforceable) {
		t.Fatalf("read-only started without ask mode: %v", err)
	}
	var started *StartError
	if !errors.As(err, &started) || started.SessionID() != "cursor-chat-1" {
		t.Fatalf("created chat not reported for concealment: %v", err)
	}
}

func TestCursorRejectsUnknownModelAndEffort(t *testing.T) {
	setupCursorBin(t, "cursor-agent", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := NewCursorBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Model: "nope"})
	if !errors.Is(err, ErrModelUnavailable) || !strings.Contains(err.Error(), "auto") {
		t.Fatalf("%v", err)
	}
	if f := FailureFromError("cursor", err, time.Now()); f.Class != "model" {
		t.Fatalf("class=%q", f.Class)
	}
	_, err = NewCursorBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Effort: "high"})
	if !errors.Is(err, ErrEffortUnavailable) {
		t.Fatalf("%v", err)
	}
}

func TestCursorResumeSetsAccessMode(t *testing.T) {
	for _, access := range []Access{AccessReadOnly, AccessWrite} {
		t.Run(string(access), func(t *testing.T) {
			logPath := setupCursorBin(t, "cursor-agent", "")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s, err := NewCursorBackend().(OptsResumer).ResumeWith(ctx, "old-chat", StartOpts{RepoDir: t.TempDir(), Access: access})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			log := readPeerLog(t, logPath)
			want := "agent"
			if access == AccessReadOnly {
				want = "ask"
			}
			if !strings.Contains(log, `request session/load`) || !strings.Contains(log, `"sessionId":"old-chat"`) || !strings.Contains(log, `request session/set_mode {"modeId":"`+want+`"`) {
				t.Fatalf("%s", log)
			}
		})
	}
}

func TestCursorTurnAnswersPermissionsAndExtensionRequests(t *testing.T) {
	logPath := setupCursorBin(t, "cursor-agent", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := NewCursorBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	go func() {
		for a := range s.Approvals() {
			_ = a.Respond(DecisionDeny) // read-only: pactify denies every request
		}
	}()
	done := make(chan Event, 1)
	go func() {
		for e := range s.Events() {
			if e.Kind == EventState && e.State == StateTurnCompleted {
				done <- e
				return
			}
		}
	}()
	if err := s.Prompt(ctx, UserMessage{Text: "go"}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-done:
		if e.Text != "done" || !e.Final {
			t.Fatalf("final=%+v", e)
		}
	case <-ctx.Done():
		t.Fatal("turn never completed")
	}
	// Allow the peer's response log to flush.
	time.Sleep(200 * time.Millisecond)
	log := readPeerLog(t, logPath)
	for _, want := range []string{
		`response id=900 result={"outcome":{"outcome":"cancelled"}}`,
		`response id=901 result={"outcome":{"outcome":"cancelled"}}`,
		`response id=902 result= error={"code":-32601,`,
		`response id=903 result={"outcome":{"optionId":"reject-once","outcome":"selected"}}`,
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("missing %q in:\n%s", want, log)
		}
	}
}

func TestCursorModelDetails(t *testing.T) {
	setupCursorBin(t, "cursor-agent", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := CursorModelDetails(ctx, StartOpts{RepoDir: t.TempDir()})
	if err != nil || d.DefaultModel != "auto" || strings.Join(d.Items, ",") != "auto,composer-2" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestCursorFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		message string
		raw     string
		want    string
	}{
		{"Authentication required", `{"code":-32000}`, "auth"},
		{"Please run 'agent login' first", "", "auth"},
		{"resource_exhausted: usage limit reached", "", "quota"},
		{"You've hit your usage limit", "", "quota"},
		{"Connection error", "", "transient"},
		{"ACP stopped: refusal", `{"stopReason":"refusal"}`, "refusal"},
		{"something else", "", "other"},
	} {
		got := ClassifyFailure("cursor", tc.message, json.RawMessage(tc.raw), time.Now()).Class
		if got != tc.want {
			t.Errorf("%q: got %q want %q", tc.message, got, tc.want)
		}
	}
}
