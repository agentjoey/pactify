package sub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestDashboardWorkerProcessHelper(t *testing.T) {
	if os.Getenv("AJ704_WORKER_HELPER") != "1" {
		return
	}
	values := map[string]string{}
	for _, key := range []string{"PACTIFY_ALLOWED_ORIGINS", "AJ704_WORKER_KEPT"} {
		values[key] = os.Getenv(key)
	}
	data, _ := json.Marshal(values)
	_ = os.WriteFile(os.Getenv("AJ704_WORKER_ENV_FILE"), data, 0600)
	time.Sleep(200 * time.Millisecond)
	os.Exit(0)
}
func TestDashboardWorkerReapedAndEnvironmentFiltered(t *testing.T) {
	for _, dashboard := range []bool{true, false} {
		t.Run(strconv.FormatBool(dashboard), func(t *testing.T) {
			s := Store{Root: t.TempDir()}
			st, err := s.Create("worker", Request{})
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("AJ704_WORKER_HELPER", "1")
			path := filepath.Join(t.TempDir(), "env.json")
			t.Setenv("AJ704_WORKER_ENV_FILE", path)
			t.Setenv("AJ704_WORKER_KEPT", "kept")
			t.Setenv("PACTIFY_ALLOWED_ORIGINS", "https://dashboard.example")
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestDashboardWorkerProcessHelper$")
			if err := s.startWorkerCommand(st.ID, cmd, dashboard); err != nil {
				t.Fatal(err)
			}
			current, err := s.Load(st.ID)
			if err != nil || current.Worker == nil {
				t.Fatal(current, err)
			}
			pid := current.Worker.PID
			t.Cleanup(func() { var status syscall.WaitStatus; _, _ = syscall.Wait4(pid, &status, 0, nil) })
			deadline := time.Now().Add(3 * time.Second)
			var data []byte
			for {
				data, err = os.ReadFile(path)
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			values := map[string]string{}
			if err := json.Unmarshal(data, &values); err != nil {
				t.Fatal(err)
			}
			if values["AJ704_WORKER_KEPT"] != "kept" {
				t.Fatal(values)
			}
			if dashboard {
				if values["PACTIFY_ALLOWED_ORIGINS"] != "" {
					t.Fatal("serve-only environment leaked", values)
				}
				for syscall.Kill(pid, 0) == nil {
					if time.Now().After(deadline) {
						t.Fatal("worker was not reaped")
					}
					time.Sleep(10 * time.Millisecond)
				}
			} else if values["PACTIFY_ALLOWED_ORIGINS"] != "https://dashboard.example" {
				t.Fatal("CLI environment changed", values)
			}
		})
	}
}
