package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

func TestAgyReadOnlyDenialExplainsFileToolsOnly(t *testing.T) {
	b, o := agyFixture(t, "denied", AccessReadOnly)
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := agyTurn(t, s)
	last := events[len(events)-1]
	if last.State != StateTurnFailed {
		t.Fatal(last)
	}
	for _, term := range []string{"read-only", "shell commands", "writes", "ends the turn", "file tools", "--write", "codex --read-only"} {
		if !strings.Contains(last.Err, term) {
			t.Errorf("diagnostic missing %q: %s", term, last.Err)
		}
	}
}

func TestAgyReadOnlyNeverSkipsPermissions(t *testing.T) {
	b, o := agyFixture(t, "", AccessReadOnly)
	original := b.(*agyBackend).command
	b.(*agyBackend).command = func(ctx context.Context, args ...string) *exec.Cmd {
		for _, arg := range args {
			if strings.HasPrefix(arg, "--dangerously-skip-permissions") {
				t.Error("read-only launched with skip-permissions")
			}
		}
		return original(ctx, args...)
	}
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := agyTurn(t, s)
	if events[len(events)-1].State != StateTurnCompleted {
		t.Fatal(events[len(events)-1])
	}
}

// The tool is deliberately in its own session, with no inherited output pipe.
// Its parent exits promptly on SIGINT, so pipe/group cleanup cannot collect it.
func TestAgyOrphanProcessHelper(t *testing.T) {
	mode := os.Getenv("PACTIFY_AGY_ORPHAN_HELPER")
	if mode == "" {
		return
	}
	if mode == "child" {
		signal.Ignore(syscall.SIGTERM)
		exe, _ := os.Executable()
		grandchild := exec.Command(exe, "-test.run=^TestAgyOrphanProcessHelper$")
		grandchild.Env = append(os.Environ(), "PACTIFY_AGY_ORPHAN_HELPER=grandchild")
		grandchild.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := grandchild.Start(); err != nil {
			os.Exit(42)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Lstat(os.Getenv("PACTIFY_AGY_ORPHAN_RECORD") + ".ready"); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(46)
			}
			time.Sleep(5 * time.Millisecond)
		}
		self, _ := procx.Inspect(os.Getpid())
		grand, _ := procx.Inspect(grandchild.Process.Pid)
		data, _ := json.Marshal([]procx.Identity{self, grand})
		if err := os.WriteFile(os.Getenv("PACTIFY_AGY_ORPHAN_RECORD"), data, 0600); err != nil {
			os.Exit(43)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "grandchild" {
		signal.Ignore(syscall.SIGTERM)
		if err := os.WriteFile(os.Getenv("PACTIFY_AGY_ORPHAN_RECORD")+".ready", []byte("ready"), 0600); err != nil {
			os.Exit(47)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	args := strings.Join(os.Args, " ")
	if strings.Contains(args, "-- models") {
		fmt.Println("model-a\tModel A")
		os.Exit(0)
	}
	exe, _ := os.Executable()
	child := exec.Command(exe, "-test.run=^TestAgyOrphanProcessHelper$")
	child.Env = append(os.Environ(), "PACTIFY_AGY_ORPHAN_HELPER=child")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		os.Exit(44)
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Lstat(os.Getenv("PACTIFY_AGY_ORPHAN_RECORD")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(45)
		}
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Println(`{"event":"init","conversation_id":"orphan-test","init":{"model":"model-a"}}`)
	if mode == "normal" {
		fmt.Println(`{"event":"result","result":{"conversation_id":"orphan-test","status":"SUCCESS","response":"done"}}`)
		os.Exit(0)
	}
	if mode == "stubborn" {
		signal.Ignore(os.Interrupt)
		for {
			time.Sleep(time.Hour)
		}
	}
	<-ch
	fmt.Println(`{"event":"result","result":{"status":"ERROR","error":"interrupted"}}`)
	os.Exit(1)
}

func TestAgyCancellationCollectsDetachedDescendants(t *testing.T) {
	for _, mode := range []string{"prompt-exit", "stubborn", "normal"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(t.TempDir(), "descendants.json")
			b := &agyBackend{command: func(ctx context.Context, args ...string) *exec.Cmd {
				return exec.CommandContext(ctx, exe, append([]string{"-test.run=^TestAgyOrphanProcessHelper$", "--"}, args...)...)
			}, grace: 150 * time.Millisecond}
			o := StartOpts{RepoDir: t.TempDir(), Model: "model-a", Access: AccessWrite, Env: []string{"HOME=" + t.TempDir(), "PACTIFY_AGY_ORPHAN_HELPER=" + mode, "PACTIFY_AGY_ORPHAN_RECORD=" + record}}
			s, err := b.Start(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.Prompt(context.Background(), UserMessage{Text: "test"}); err != nil {
				t.Fatal(err)
			}
			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()
			for s.ThreadID() == "" {
				select {
				case <-s.Events():
				case <-timer.C:
					t.Fatal("no init")
				}
			}
			data, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			var descendants []procx.Identity
			if err = json.Unmarshal(data, &descendants); err != nil || len(descendants) != 2 {
				t.Fatalf("descendants=%s err=%v", data, err)
			}
			t.Cleanup(func() {
				for _, p := range descendants {
					_ = p.Signal(syscall.SIGKILL, false)
				}
			})
			for _, p := range descendants {
				alive, e := p.Alive()
				if e != nil || !alive {
					t.Fatalf("fixture not alive: %+v %v", p, e)
				}
			}
			if mode == "normal" {
				for {
					select {
					case ev := <-s.Events():
						if ev.State == StateTurnCompleted {
							if err = s.Close(); err != nil {
								t.Fatal(err)
							}
							for _, p := range descendants {
								alive, e := p.Alive()
								if e != nil || !alive {
									t.Fatalf("normal completion killed descendant %+v: %v", p, e)
								}
							}
							return
						}
					case <-timer.C:
						t.Fatal("no normal result")
					}
				}
			}
			// Close and the context watcher can race with explicit Interrupt in real jobs.
			if err = s.Interrupt(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			for _, p := range descendants {
				alive, e := p.Alive()
				if e != nil || alive {
					t.Errorf("descendant survived cancellation: %+v, alive=%t err=%v", p, alive, e)
				}
			}
		})
	}
}
