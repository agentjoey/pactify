package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit/golden"
)

type goldenOutcome struct {
	SessionID string   `json:"session_id"`
	Events    []string `json:"events"`
	Text      string   `json:"text"`
	Usage     Usage    `json:"usage"`
	Failure   string   `json:"failure"`
}

func goldenRun(ctx context.Context, tr golden.Transcript, opts StartOpts, wireHome string, offsets map[string]int64) (goldenOutcome, error) {
	backend := NewOpencodeBackend()
	if tr.Harness == "kimi" {
		backend = NewACPBackend("kimi-cli")
	}
	var s Session
	var err error
	if tr.Resume != "" {
		s, err = backend.(OptsResumer).ResumeWith(ctx, tr.Resume, opts)
	} else {
		s, err = backend.Start(ctx, opts)
	}
	if err != nil {
		return goldenOutcome{}, err
	}
	if tr.Scenario == "failure" && tr.Harness == "opencode" {
		s.(*opencodeSession).opts.Model = "opencode/pactify-nonexistent-model"
	}
	defer s.Close()
	result := goldenOutcome{SessionID: s.ThreadID()}
	var c goldenCollector
	go func() {
		for a := range s.Approvals() {
			_ = a.Respond(DecisionAllow)
			if ack := os.Getenv("GOLDEN_APPROVAL_ACK"); ack != "" {
				os.WriteFile(ack, []byte("ok"), 0600)
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- s.Prompt(ctx, UserMessage{Text: tr.Prompt}) }()
	interrupted := false
	timer := time.NewTimer(1500 * time.Millisecond)
	defer timer.Stop()
	for !c.TurnDone {
		select {
		case <-timer.C:
			if tr.Scenario == "cancel" {
				if err = s.Interrupt(ctx); err != nil {
					return result, err
				}
				interrupted = true
			}
		case e, ok := <-s.Events():
			if !ok {
				return result, fmt.Errorf("session closed before terminal event")
			}
			c.Add(e)
			signature := string(e.Kind)
			if e.State != "" {
				signature += ":" + e.State
			}
			if e.Tool != nil {
				signature += ":" + e.Tool.Phase
			}
			if e.Final {
				signature += ":final"
			}
			if e.Kind != EventSystem && !(e.Kind == EventState && e.State != StateTurnCompleted && e.State != StateTurnFailed && e.State != "agent_thought_chunk") {
				result.Events = append(result.Events, signature)
			}
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
	select {
	case err = <-done:
	case <-ctx.Done():
		return result, ctx.Err()
	}
	if err != nil {
		return result, err
	}
	result.Text = c.FinalText()
	result.Usage = c.Usage
	if c.TurnErr != "" {
		result.Failure = ClassifyFailure(tr.Harness, c.FailureMessage, c.FailureRaw, time.Now()).Class
	}
	if tr.Harness == "kimi" && wireHome != "" {
		// Read only private session wire files; never open config or credentials.
		var record KimiRecord
		for deadline := time.Now().Add(3 * time.Second); ; {
			record, err = ReadKimiRecordOffsets(wireHome, s.ThreadID(), offsets)
			if err == nil && (record.Error != nil || record.EndedTurns > 0 && record.Turns > 0 || tr.Scenario == "cancel") {
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			return result, err
		}
		result.Usage = Usage{InputTokens: record.InputTokens, OutputTokens: record.OutputTokens, TotalTokens: record.InputTokens + record.OutputTokens}
		if record.Error != nil {
			raw, _ := json.Marshal(map[string]any{"reason": record.Reason, "error": record.Error})
			result.Failure = ClassifyFailure("kimi", record.Error.Message, raw, time.Now()).Class
		}
	}
	if tr.Scenario == "cancel" {
		if !interrupted {
			return result, fmt.Errorf("cancel scenario ended before interrupt")
		}
		result.Failure = "canceled"
	}
	result.Text = golden.SanitizeText(result.Text)
	if err = s.Close(); err != nil {
		return result, err
	}
	return result, nil
}

func readGolden(path string) (golden.Transcript, error) {
	var tr golden.Transcript
	b, e := os.ReadFile(path)
	if e == nil {
		e = json.Unmarshal(b, &tr)
	}
	return tr, e
}

func TestGoldenReplay(t *testing.T) {
	files, _ := filepath.Glob("testdata/golden/*.json")
	if len(files) != 9 {
		t.Errorf("need 5 opencode + 4 kimi real recordings, got %d", len(files))
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			tr, err := readGolden(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = replayGolden(t, tr); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestGoldenKimiReadOnlyUnavailable(t *testing.T) {
	t.Log("No kimi read-only recording: upstream cannot enforce read-only access; entry points reject it.")
}
func TestGoldenFilesSanitized(t *testing.T) {
	files, _ := filepath.Glob("testdata/golden/*.json")
	contractFiles, _ := filepath.Glob("golden/testdata/*.json")
	files = append(files, contractFiles...)
	if len(files) == 0 {
		t.Fatal("no recordings")
	}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = golden.Scan(string(b)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}
func TestGoldenDetectsRenamedTokens(t *testing.T) {
	for _, name := range []string{"opencode-write", "kimi-write"} {
		t.Run(name, func(t *testing.T) {
			tr, err := readGolden("testdata/golden/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			for i, f := range tr.Frames {
				key := `"tokens"`
				if tr.Harness == "kimi" {
					key = `"usage"`
				}
				if strings.Contains(f.Data, key) {
					tr.Frames[i].Data = strings.ReplaceAll(f.Data, key, `"tokenz"`)
					changed = true
				}
			}
			if !changed {
				t.Fatal("mutation did not touch a field")
			}
			if err = replayGolden(t, tr); err == nil || !strings.Contains(err.Error(), "replay mismatch") {
				t.Fatalf("renamed usage must cause an outcome mismatch, got %v", err)
			}
		})
	}
}

func replayGolden(t *testing.T, tr golden.Transcript) error {
	t.Helper()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "recording.json")
	b, _ := json.Marshal(tr)
	if err := os.WriteFile(fixture, b, 0600); err != nil {
		return err
	}
	bin := filepath.Join(dir, tr.Harness)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err = os.WriteFile(bin, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(exe, "'", "'\\''")+"' -test.run=^TestGoldenPeer$\n"), 0700); err != nil {
		return err
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOLDEN_PEER_FILE", fixture)
	t.Setenv("GOLDEN_APPROVAL_ACK", filepath.Join(dir, "approval-ack"))
	t.Setenv("PACTIFY_HOME", t.TempDir())
	opts := StartOpts{RepoDir: dir, Model: tr.Model, Access: AccessWrite, MinimalConfig: true, Stderr: io.Discard}
	if tr.Scenario == "readonly" {
		opts.Access = AccessReadOnly
	}
	wireHome := ""
	if tr.Harness == "kimi" {
		wireHome = filepath.Join(dir, "kimi-home")
		var want goldenOutcome
		if err = json.Unmarshal(tr.Expected, &want); err != nil {
			return err
		}
		sessionDir := filepath.Join(wireHome, "session")
		os.MkdirAll(sessionDir, 0700)
		index, _ := json.Marshal(map[string]string{"sessionId": want.SessionID, "sessionDir": sessionDir})
		os.WriteFile(filepath.Join(wireHome, "session_index.jsonl"), append(index, '\n'), 0600)
		for _, f := range tr.Frames {
			if strings.HasPrefix(f.Channel, "wire/") {
				agent := strings.TrimPrefix(f.Channel, "wire/")
				if !filepath.IsLocal(agent) {
					return fmt.Errorf("invalid agent")
				}
				p := filepath.Join(sessionDir, "agents", agent)
				os.MkdirAll(p, 0700)
				os.WriteFile(filepath.Join(p, "wire.jsonl"), []byte(f.Data), 0600)
			}
		}
		opts.Env = []string{"KIMI_CODE_HOME=" + wireHome}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	got, err := goldenRun(ctx, tr, opts, wireHome, nil)
	if err != nil {
		return err
	}
	var want goldenOutcome
	if err = json.Unmarshal(tr.Expected, &want); err != nil {
		return err
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("replay mismatch\ngot: %+v\nwant: %+v", got, want)
	}
	return nil
}

// Replays only the recorded external boundary; the real backend, ACP client,
// event collector, usage parser and failure classifier remain in the test path.
func TestGoldenPeer(t *testing.T) {
	path := os.Getenv("GOLDEN_PEER_FILE")
	if path == "" {
		return
	}
	tr, err := readGolden(path)
	if err != nil {
		os.Exit(2)
	}
	if tr.Harness == "opencode" {
		goldenHTTPPeer(tr)
		os.Exit(0)
	}
	in := bufio.NewReader(os.Stdin)
	ids := map[string]json.RawMessage{}
	for _, f := range tr.Frames {
		switch f.Channel {
		case "stdin":
			line, e := in.ReadBytes('\n')
			if e != nil {
				os.Exit(3)
			}
			var got, want map[string]json.RawMessage
			if json.Unmarshal(line, &got) != nil || json.Unmarshal([]byte(f.Data), &want) != nil {
				os.Exit(4)
			}
			if string(got["method"]) != string(want["method"]) {
				os.Exit(5)
			}
			if len(want["id"]) > 0 {
				ids[string(want["id"])] = got["id"]
			}
		case "stdout":
			var msg map[string]json.RawMessage
			if json.Unmarshal([]byte(f.Data), &msg) != nil {
				os.Exit(6)
			}
			if id, ok := ids[string(msg["id"])]; ok {
				msg["id"] = id
			}
			b, _ := json.Marshal(msg)
			fmt.Println(string(b))
		}
	}
	// Keep the peer alive until the client closes; EOF before dispatch would race
	// the final RPC response with Spawn's process-exit notification.
	io.Copy(io.Discard, in)
	os.Exit(0)
}

func goldenHTTPPeer(tr golden.Transcript) {
	queues := map[string][]golden.Frame{}
	for _, f := range tr.Frames {
		if f.Channel == "http" {
			key := f.Method + " " + f.Path
			queues[key] = append(queues[key], f)
		}
	}
	var mu sync.Mutex
	prompt := make(chan struct{})
	abort := make(chan struct{})
	var abortOnce sync.Once
	var once sync.Once
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(7)
	}
	fmt.Println("opencode server listening on http://" + listener.Addr().String())
	http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		mu.Lock()
		q := queues[key]
		var f golden.Frame
		if len(q) > 0 {
			f = q[0]
			queues[key] = q[1:]
		}
		mu.Unlock()
		if len(q) == 0 {
			http.Error(w, "unrecorded request: "+key, 500)
			return
		}
		if r.URL.Path == "/event" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(f.Status)
			w.(http.Flusher).Flush()
			first, rest, _ := strings.Cut(f.Data, "\n\n")
			io.WriteString(w, first+"\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-prompt:
			case <-r.Context().Done():
				return
			}
			for _, block := range strings.Split(rest, "\n\n") {
				if tr.Scenario == "cancel" && strings.Contains(block, `"name":"MessageAbortedError"`) {
					select {
					case <-abort:
					case <-r.Context().Done():
						return
					}
				}
				io.WriteString(w, block+"\n\n")
				w.(http.Flusher).Flush()
				if strings.Contains(block, `"type":"permission.asked"`) && tr.Scenario != "readonly" {
					for {
						if _, err := os.Stat(os.Getenv("GOLDEN_APPROVAL_ACK")); err == nil {
							os.Remove(os.Getenv("GOLDEN_APPROVAL_ACK"))
							break
						}
						select {
						case <-r.Context().Done():
							return
						case <-time.After(time.Millisecond):
						}
					}
				}
			}
			<-r.Context().Done()
			return
		}
		if strings.HasSuffix(r.URL.Path, "/abort") {
			abortOnce.Do(func() { close(abort) })
		}
		if strings.HasSuffix(r.URL.Path, "/prompt_async") {
			once.Do(func() { close(prompt) })
		}
		w.WriteHeader(f.Status)
		io.WriteString(w, f.Data)
	}))
}

type goldenCollector struct {
	TurnDone       bool
	TurnErr        string
	FailureMessage string
	FailureRaw     json.RawMessage
	Usage          Usage
	text           string
	chunks         strings.Builder
}

func (c *goldenCollector) Add(e Event) {
	if e.Kind == EventTool && e.Tool != nil && e.Tool.Phase == "start" {
		c.chunks.Reset()
	}
	if e.Kind == EventMessage {
		if e.Final {
			c.text = e.Text
		} else {
			c.chunks.WriteString(e.Text)
		}
	}
	if e.Final && e.Text != "" {
		c.text = e.Text
	}
	if e.Usage != nil {
		c.Usage = *e.Usage
	}
	if e.Kind == EventState && (e.State == StateTurnCompleted || e.State == StateTurnFailed) {
		c.TurnDone = true
	}
	if e.State == StateTurnFailed {
		c.TurnErr = e.Err
		c.FailureMessage = e.Err
		c.FailureRaw = e.Raw
	}
}
func (c *goldenCollector) FinalText() string {
	if c.text != "" {
		return c.text
	}
	return c.chunks.String()
}

func TestGoldenReadOnlyContainsDeniedWrite(t *testing.T) {
	tr, err := readGolden("testdata/golden/opencode-readonly.json")
	if err != nil {
		t.Fatal(err)
	}
	if !goldenDeniedWrite(tr.Frames) {
		t.Fatal("plan golden needs an actual rejected write tool call, not a model refusal")
	}
}

func goldenDeniedWrite(frames []golden.Frame) bool {
	for _, f := range frames {
		if f.Path != "/event" {
			continue
		}
		for _, line := range strings.Split(f.Data, "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event struct {
				Properties struct {
					Part struct {
						Tool  string `json:"tool"`
						State struct {
							Status string `json:"status"`
							Input  struct {
								Command string `json:"command"`
							} `json:"input"`
							Error string `json:"error"`
						} `json:"state"`
					} `json:"part"`
				} `json:"properties"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
				continue
			}
			p := event.Properties.Part
			if p.Tool == "bash" && p.State.Status == "error" && p.State.Input.Command == "echo x > denied.txt" && (strings.Contains(strings.ToLower(p.State.Error), "denied") || strings.Contains(strings.ToLower(p.State.Error), "permission")) {
				return true
			}
		}
	}
	return false
}
