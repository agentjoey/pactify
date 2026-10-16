//go:build record

package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/cockpit/golden"
)

// Explicit opt-in: this is never invoked by ordinary tests. HOME and each
// protocol data directory must be private children of PACTIFY_RECORD_HOME.
func TestRecordGolden(t *testing.T) {
	root := os.Getenv("PACTIFY_RECORD_HOME")
	if root == "" {
		t.Skip("set PACTIFY_RECORD_HOME and an isolated PACTIFY_HOME explicitly")
	}
	abs, e := filepath.Abs(root)
	if e != nil || abs != root || root != os.Getenv("PACTIFY_HOME") {
		t.Fatal("record home must be absolute and equal isolated PACTIFY_HOME")
	}
	userHome := os.Getenv("HOME")
	if golden.ManagedDirectory(root, userHome) != nil {
		t.Fatal("HOME must be explicitly managed below record home")
	}
	harness, scenario := os.Getenv("PACTIFY_RECORD_HARNESS"), os.Getenv("PACTIFY_RECORD_SCENARIO")
	if harness != "kimi" && harness != "opencode" {
		t.Fatal("recording only permits opencode or kimi; agy is deferred")
	}
	valid := map[string]bool{"write": true, "readonly": true, "resume": true, "cancel": true, "failure": true}
	if !valid[scenario] || harness == "kimi" && scenario == "readonly" {
		t.Fatal("unsupported recording scenario")
	}
	repo := filepath.Join(root, harness, "repo")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + userHome, "PACTIFY_HOME=" + root, "CHOKIDAR_USEPOLLING=1", "KIMI_CODE_NO_AUTO_UPDATE=1"}
	home := ""
	if harness == "kimi" {
		home = os.Getenv("KIMI_CODE_HOME")
		env = append(env, "KIMI_CODE_HOME="+home)
	} else {
		for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
			v := os.Getenv(key)
			if golden.ManagedDirectory(root, v) != nil {
				t.Fatalf("%s must be managed", key)
			}
			env = append(env, key+"="+v)
		}
	}
	if harness == "kimi" && golden.ManagedDirectory(root, home) != nil {
		t.Fatal("KIMI_CODE_HOME must be explicitly managed")
	}
	tr := golden.Transcript{Harness: harness, Scenario: scenario}
	cmd := exec.Command(harness, "--version")
	cmd.Env = acp.CommandEnv(env)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	tr.Version = strings.TrimSpace(string(out))
	var diagnostics bytes.Buffer
	opts := StartOpts{RepoDir: repo, Env: env, Access: AccessWrite, MinimalConfig: true, Stderr: &diagnostics}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if harness == "opencode" {
		models, err := OpencodeModels(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range models {
			if strings.HasPrefix(m, "opencode/") && strings.HasSuffix(m, "-free") {
				if tr.Model == "" || strings.Contains(m, "flash") {
					tr.Model = m
				}
			}
		}
		if requested := os.Getenv("PACTIFY_RECORD_MODEL"); requested != "" {
			found := false
			for _, m := range models {
				if m == requested {
					found = true
				}
			}
			if !found || !(strings.HasPrefix(requested, "opencode/") && strings.HasSuffix(requested, "-free") || strings.HasPrefix(requested, "minimax-coding-plan/")) {
				t.Fatal("record model must be live-listed and free or minimax-coding-plan")
			}
			tr.Model = requested
		}
		if tr.Model == "" {
			t.Fatal("no opencode/*-free model available")
		}
		t.Log("Live allowed model:", tr.Model)
	}
	if harness == "kimi" && scenario == "failure" {
		// A fresh managed configuration, not a read/copy/edit of the user's config.
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		go func() {
			for {
				c, e := listener.Accept()
				if e != nil {
					return
				}
				c.Close()
			}
		}()
		home = filepath.Join(root, "kimi-offline")
		os.MkdirAll(home, 0700)
		config := fmt.Sprintf("default_model = \"offline\"\n[providers.offline]\ntype = \"openai\"\nbase_url = \"http://%s/v1\"\napi_key = \"not-a-real-key\"\n[models.offline]\nprovider = \"offline\"\nmodel = \"offline\"\nmax_context_size = 32768\n[loop_control]\nmax_attempts_per_step = 1\n", listener.Addr().String())
		if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		opts.Env = append(opts.Env, "KIMI_CODE_HOME="+home)
	}
	tr.Prompt = "Use your shell tool to write golden.txt containing alpha in the current directory. Then reply exactly GOLDEN_WRITE_OK."
	if scenario == "readonly" {
		tr.Notes = "Plan agent diagnostic prompt overridden to request one actual write attempt; native plan agent and deny permissions unchanged."
		opts.Access = AccessReadOnly
		opts.Env = append(opts.Env, `OPENCODE_CONFIG_CONTENT={"agent":{"plan":{"prompt":"You are a permission diagnostic agent. Invoke the requested read and write commands exactly once, to observe the permission layer. The policy must reject the write. Do not simulate or predict tool output; invoke the tool, report its actual denial, and stop."}}}`)
		tr.Prompt = "This is an authorized plan-mode permission diagnostic. Actually run ls once, then attempt the exact bash command echo x > denied.txt once. Report its actual denial and stop. Do not retry, use other tools, or seek a workaround."
	}
	if scenario == "resume" {
		b, err := os.ReadFile(filepath.Join(root, harness+"-session"))
		if err != nil {
			t.Fatal(err)
		}
		tr.Resume = string(b)
		tr.Prompt = "Read golden.txt using your tool, and reply exactly GOLDEN_RESUME_alpha."
	}
	if scenario == "cancel" {
		tr.Prompt = "Use your shell tool to run sleep 60 now. Wait for it to finish before replying."
	}
	if scenario == "failure" {
		tr.Prompt = "Reply exactly GOLDEN_FAILURE_PROBE."
	}
	opts.Model = tr.Model
	r := &golden.Recorder{}
	opts.Transcript = r
	var offsets map[string]int64
	if tr.Resume != "" && harness == "kimi" {
		offsets, err = KimiWireOffsets(home, tr.Resume)
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := goldenRun(ctx, tr, opts, home, offsets)
	if err != nil {
		t.Fatalf("record %s/%s: %v; diagnostics: %s", harness, scenario, err, golden.SanitizeText(diagnostics.String()))
	}
	if scenario == "write" {
		if b, e := os.ReadFile(filepath.Join(repo, "golden.txt")); e != nil || strings.TrimSpace(string(b)) != "alpha" {
			t.Fatalf("write evidence missing: %v", e)
		}
	}
	if scenario == "readonly" {
		if result.Failure != "" || !goldenDeniedWrite(r.Snapshot()) {
			t.Fatalf("read-only evidence missing: failure=%s; actual denied write=%t", result.Failure, goldenDeniedWrite(r.Snapshot()))
		}
		if _, e := os.Stat(filepath.Join(repo, "denied.txt")); !os.IsNotExist(e) {
			t.Fatal("plan mode wrote denied.txt")
		}
	}
	if scenario == "failure" {
		want := "other"
		if harness == "kimi" {
			want = "transient"
		}
		if result.Failure != want {
			t.Fatalf("failure=%s want %s", result.Failure, want)
		}
	}
	if scenario == "write" {
		os.WriteFile(filepath.Join(root, harness+"-session"), []byte(result.SessionID), 0600)
	}
	if harness == "kimi" {
		dir, err := kimiSessionDir(home, result.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		paths, _ := filepath.Glob(filepath.Join(dir, "agents", "*", "wire.jsonl"))
		for _, path := range paths {
			agent := filepath.Base(filepath.Dir(path))
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			offset := offsets[agent]
			f.Seek(offset, io.SeekStart)
			b, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			// Retain accounting/error wire fragments, omitting full LLM prompts and tool
			// payloads. ACP already retains actual prompt/tool traffic for replay.
			var lines strings.Builder
			for _, line := range strings.Split(string(b), "\n") {
				var row map[string]json.RawMessage
				if json.Unmarshal([]byte(line), &row) != nil {
					continue
				}
				var kind string
				json.Unmarshal(row["type"], &kind)
				switch kind {
				case "usage.record", "turn.ended", "prompt.completed":
					lines.WriteString(line + "\n")
				case "llm.request":
					for k := range row {
						if k != "type" && k != "modelAlias" && k != "model" && k != "agentId" {
							delete(row, k)
						}
					}
					raw, _ := json.Marshal(row)
					lines.Write(raw)
					lines.WriteByte('\n')
				}
			}
			r.Observe("wire/"+agent, []byte(lines.String()))
		}
	}
	tr.Frames = r.Snapshot()
	tr.Expected, _ = json.Marshal(result)
	path := filepath.Join("testdata", "golden", harness+"-"+scenario+".json")
	if err = golden.Save(path, tr); err != nil {
		t.Fatal(err)
	}
	t.Logf("Saved %s (%d frames): %+v", path, len(tr.Frames), result)
}
