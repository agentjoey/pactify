package sub

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
)

func TestSaveRouterSerializesVersionCheck(t *testing.T) {
	home := t.TempDir()
	s := Store{Root: filepath.Join(home, "jobs")}
	version := fmt.Sprintf("%x", sha256.Sum256(nil))
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.SaveRouter(context.Background(), version, []byte(`{}`)) }()
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if err == ErrRouterConflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	// Failed validation leaves the original bytes untouched.
	before, _ := os.ReadFile(s.RouterPath())
	if err := s.SaveRouter(context.Background(), fmt.Sprintf("%x", sha256.Sum256(before)), []byte(`{"aliases":{"bad":{"harness":"invalid"}}}`)); err == nil {
		t.Fatal("accepted invalid config")
	}
	after, _ := os.ReadFile(s.RouterPath())
	if string(before) != string(after) {
		t.Fatal("invalid write changed disk")
	}
}

func TestSaveRouterRejectsCrossHarnessFallback(t *testing.T) {
	home := t.TempDir()
	s := Store{Root: filepath.Join(home, "jobs")}
	err := s.SaveRouter(context.Background(), fmt.Sprintf("%x", sha256.Sum256(nil)), []byte(`{"aliases":{"mm":{"harness":"opencode"}},"fallback":{"codex":["@mm","kimi"]}}`))
	var configErr *RouterConfigError
	if !errors.As(err, &configErr) || configErr.Field != "fallback.codex[0]" {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(s.RouterPath()); !os.IsNotExist(statErr) {
		t.Fatal("rejected write created router.json")
	}
}

func TestClearQuotaUsesWriterLock(t *testing.T) {
	home := t.TempDir()
	s := Store{Root: filepath.Join(home, "jobs")}
	now := time.Now()
	if err := s.rememberQuota(QuotaRoute{Harness: "codex", Model: "one", RecordedAt: now}); err != nil {
		t.Fatal(err)
	}
	release, err := lockx.Acquire(context.Background(), s.quotaPath()+".lock")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.ClearQuota(ctx, "codex/one")
	release()
	if err == nil {
		t.Fatal("clear bypassed existing quota lock")
	}
	routes, err := s.ExhaustedRoutes(now)
	if err != nil || len(routes) != 1 {
		t.Fatalf("quota changed: %v %v", routes, err)
	}
	if err := s.ClearQuota(context.Background(), "codex/one"); err != nil {
		t.Fatal(err)
	}
	routes, err = s.ExhaustedRoutes(now)
	if err != nil || len(routes) != 0 {
		t.Fatalf("quota not cleared: %v %v", routes, err)
	}
}
