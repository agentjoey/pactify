package sub

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServeTokenWorkerHelper(t *testing.T) {
	if os.Getenv("AJ790_WORKER_HELPER") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("AJ790_ENV_FILE"), []byte(os.Getenv("PACTIFY_SERVE_TOKEN")), 0600); err != nil {
		os.Exit(1)
	}
	time.Sleep(100 * time.Millisecond)
	os.Exit(0)
}

func TestServeTokenDashboardWorkers(t *testing.T) {
	token := strings.Repeat("dummy", 8)
	for _, dashboard := range []bool{true, false} {
		name := "cli"
		if dashboard {
			name = "dashboard"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("PACTIFY_SERVE_TOKEN", token)
			t.Setenv("AJ790_WORKER_HELPER", "1")
			path := filepath.Join(t.TempDir(), "env")
			t.Setenv("AJ790_ENV_FILE", path)
			store := Store{Root: t.TempDir()}
			if _, err := store.Create("worker", Request{}); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestServeTokenWorkerHelper$")
			if err := store.startWorkerCommand("worker", cmd, dashboard); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(3 * time.Second); ; {
				data, err := os.ReadFile(path)
				if err == nil {
					want := token
					if dashboard {
						want = ""
					}
					if string(data) != want {
						t.Fatalf("%s worker token filtering failed", name)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !dashboard {
				st, err := store.Load("worker")
				if err != nil || st.Worker == nil {
					t.Fatal("missing worker identity", err)
				}
				var status syscall.WaitStatus
				_, _ = syscall.Wait4(st.Worker.PID, &status, 0, nil)
			}
		})
	}
}
