package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubConfigModeInPendingAndStatusJSON(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store, _ := sub.DefaultStore()
	st, err := store.Create("queued-config", sub.Request{Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range []string{"pending", "status-detail"} {
		t.Run(surface, func(t *testing.T) {
			c := newSubCmd()
			var out bytes.Buffer
			c.SetOut(&out)
			var err error
			switch surface {
			case "pending":
				err = subPending(c, st.ID, true, st.EffectiveConfigMode())
			case "status-detail":
				_, err = subStatus(c, []string{st.ID}, true)
			}
			if err != nil || !strings.Contains(out.String(), `"configMode": "minimal"`) {
				t.Fatalf("%s missing configuration mode: %s err=%v", surface, &out, err)
			}
		})
	}
}

func TestSubConfigFlagPersistsAcrossDetachedWorker(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	for _, user := range []bool{true, false} {
		args := []string{"sub", "run", "fake", "--cwd", repo, "--json"}
		if user {
			args = append(args, "--with-user-config")
		}
		cmd := exec.Command(bin, args...)
		cmd.Stdin = strings.NewReader("Reply OK.")
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run: %v\n%s", err, data)
		}
		var result sub.Result
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		store, _ := sub.DefaultStore()
		st, err := store.Load(result.JobID)
		if err != nil || st.Request.WithUserConfig != user || result.ConfigMode != st.ConfigMode || result.ConfigMode == "" {
			t.Fatalf("state=%+v result=%s err=%v", st, data, err)
		}
		if _, err := os.Stat(store.ResultPath(result.JobID)); err != nil {
			t.Fatal(err)
		}
	}
}
