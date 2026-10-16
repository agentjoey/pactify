package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestSubRetriesFlag(t *testing.T) {
	c := newSubCmd()
	run, _, err := c.Find([]string{"run"})
	if err != nil || run.Flags().Lookup("retries") == nil {
		t.Fatal("run has no --retries")
	}
	n, _ := run.Flags().GetInt("retries")
	if n != 2 {
		t.Fatalf("default=%d", n)
	}
	for _, value := range []string{"-1", "6"} {
		if err := run.Flags().Set("retries", value); err != nil {
			t.Fatal(err)
		}
		code, err := subRun(run, "codex", "", "", ".", false, false, false, false, 0, false, "")
		if code != 2 || err == nil || !strings.Contains(err.Error(), "--retries") {
			t.Fatalf("code=%d err=%v", code, err)
		}
	}
}

func TestSubRetriesZeroPersists(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	bin := buildPactify(t)
	repo := gitRepoForCLI(t)
	out, code := runCLI(t, bin, repo, "test retries zero", "sub", "run", "fake", "--retries", "0", "--wait", "10s", "--json")
	var r sub.Result
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil {
		t.Fatalf("code=%d output=%s", code, out)
	}
	store, _ := sub.DefaultStore()
	st, err := store.Load(r.JobID)
	if err != nil || st.Request.Retries == nil || *st.Request.Retries != 0 {
		t.Fatalf("state=%+v err=%v", st, err)
	}
}
