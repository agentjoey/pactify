package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestAgySlowUsagePeer(t *testing.T) {
	if os.Getenv("AGY_SLOW_USAGE_PEER") != "1" {
		return
	}
	args := strings.Join(os.Args, " ")
	if !strings.Contains(args, "-p /usage") || !strings.Contains(args, "--model gemini-3.8-flash-low") {
		os.Exit(91)
	}
	if err := os.WriteFile(os.Getenv("AGY_SLOW_USAGE_READY"), []byte(os.TempDir()), 0600); err != nil {
		os.Exit(92)
	}
	time.Sleep(7 * time.Second)
	// The job can finish and remove its own temp directory while /usage is active.
	if err := os.WriteFile(filepath.Join(os.TempDir(), "usage-finished"), []byte("done"), 0600); err != nil {
		os.Exit(93)
	}
	fmt.Println(`{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"gemini-weekly","remaining_fraction":0.5,"reset_time":"2099-01-01T00:00:00Z"}]}]}}}`)
	os.Exit(0)
}

type agyProcessQuotaBackend struct{ cockpit.Backend }

func (b agyProcessQuotaBackend) Quota(ctx context.Context, opts cockpit.StartOpts) (cockpit.QuotaObservation, error) {
	return cockpit.NewAgyBackend().(interface {
		Quota(context.Context, cockpit.StartOpts) (cockpit.QuotaObservation, error)
	}).Quota(ctx, opts)
}

func TestAgyQuotaProcessSurvivesTerminalCleanup(t *testing.T) {
	store, s, backend := workerFixture(t, "write")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	wrapper := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=^TestAgySlowUsagePeer$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGY_SLOW_USAGE_PEER", "1")
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("AGY_SLOW_USAGE_READY", ready)
	s.script = func(s *fakeSession) {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "usage process did not start"}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	h := newFakeHarness(backend)
	h.Backend = func() cockpit.Backend { return agyProcessQuotaBackend{backend} }
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || r.Status != StatusSucceeded || len(r.QuotaSnapshots) != 2 || contains(r.Warnings, "Quota snapshot unavailable") {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	probeTemp, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(probeTemp)); !os.IsNotExist(err) {
		t.Fatalf("quota temp survived cleanup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir("j1"), "tmp")); !os.IsNotExist(err) {
		t.Fatalf("job temp survived cleanup: %v", err)
	}
}
