package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func TestSubFailureStatusAndQuotaJSON(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	now := time.Now()
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Create("quota-job", sub.Request{Harness: "agy", Model: "argon"})
	if err != nil {
		t.Fatal(err)
	}
	r := sub.Result{Status: sub.StatusFailed}
	if err := store.Finish("quota-job", &r, "Individual quota reached. Resets in 166h58m25s.", now); err != nil {
		t.Fatal(err)
	}
	out, code, err := captureStatus(t, nil, false)
	if code != 0 || err != nil || !strings.Contains(out, "failed (quota, resets in 6d 22h)") || !strings.HasPrefix(out, "Exhausted routes: agy/argon") {
		t.Fatalf("%d %v %s", code, err, out)
	}
	for _, ids := range [][]string{nil, {"quota-job"}} {
		out, code, err = captureStatus(t, ids, true)
		if code != 0 || err != nil || !strings.Contains(out, `"failureClass": "quota"`) || !strings.Contains(out, `"quotaResetAt"`) {
			t.Fatalf("%d %v %s", code, err, out)
		}
	}
	var buf bytes.Buffer
	c := &cobra.Command{}
	c.SetOut(&buf)
	code, err = subQuota(c, true)
	var routes []sub.QuotaRoute
	if err != nil || code != 0 || json.Unmarshal(buf.Bytes(), &routes) != nil || len(routes) != 1 || routes[0].JobID != "quota-job" {
		t.Fatalf("quota %d %v %s", code, err, buf.String())
	}
}
