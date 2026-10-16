package sub

import (
	"context"
	"errors"
	"github.com/agentjoey/pactify/internal/cockpit"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestHarnessCatalogCache(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	calls := 0
	fail := false
	installed := true
	now := time.Now()
	h := Harness{Name: "fixture", Version: func(context.Context) (string, error) { return "1.2.3", nil }, Models: func(context.Context, cockpit.StartOpts) ([]string, error) {
		calls++
		if fail {
			return nil, errors.New("catalog offline")
		}
		return []string{"one"}, nil
	}}
	c := newHarnessCatalog()
	c.now = func() time.Time { return now }
	c.lookupPath = func(string) (string, error) {
		if installed {
			return "/fixture", nil
		}
		return "", errors.New("not installed")
	}
	if err := recordVerifiedVersion(context.Background(), filepath.Dir(s.Root), h.Name, "1.2.0", now); err != nil {
		t.Fatal(err)
	}
	get := func(refresh bool) HarnessInfo {
		t.Helper()
		return c.harness(context.Background(), s, h, HarnessDefault{Model: "default"}, refresh)
	}
	a := get(false)
	if !a.Installed || a.Version != "1.2.3" || a.VerifiedVersion != "1.2.0" || a.Default.Model != "default" || !reflect.DeepEqual(a.Models.Items, []string{"one"}) {
		t.Fatal(a)
	}
	get(false)
	if calls != 1 {
		t.Fatal(calls)
	}
	now = now.Add(time.Minute)
	get(true)
	if calls != 2 {
		t.Fatal(calls)
	}
	now = now.Add(11 * time.Minute)
	fail = true
	b := get(false)
	if calls != 3 || b.Models.Error != "catalog offline" || !reflect.DeepEqual(b.Models.Items, a.Models.Items) || b.Models.FetchedAt.IsZero() {
		t.Fatal(b, calls)
	}
	get(false)
	if calls != 3 {
		t.Fatal("failures should also be cached", calls)
	}
	installed = false
	b = get(true)
	if b.Installed || b.Models.Error == "" || calls != 3 {
		t.Fatal(b, calls)
	}
	installed = true
	h.Models = nil
	b = get(true)
	if b.Models.Error == "" || calls != 3 {
		t.Fatal(b, calls)
	}
}

func TestHarnessCatalogConcurrentRefresh(t *testing.T) {
	s := Store{Root: t.TempDir()}
	c := newHarnessCatalog()
	c.lookupPath = func(string) (string, error) { return "/fixture", nil }
	calls := 0
	h := Harness{Name: "fixture", Models: func(context.Context, cockpit.StartOpts) ([]string, error) { calls++; return []string{"one"}, nil }}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.harness(context.Background(), s, h, HarnessDefault{}, false) }()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestHarnessCatalogCachesSelectionMetadata(t *testing.T) {
	c := newHarnessCatalog()
	c.lookupPath = func(string) (string, error) { return "/fixture", nil }
	s := Store{Root: t.TempDir()}
	calls := 0
	failed := false
	h := Harness{Name: "fixture", Models: func(context.Context, cockpit.StartOpts) ([]string, error) {
		t.Fatal("metadata discovery must not run a second id-only call")
		return nil, nil
	}, ModelDetails: func(context.Context, cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
		calls++
		if failed {
			return cockpit.ModelDiscovery{}, errors.New("offline")
		}
		return cockpit.ModelDiscovery{Items: []string{"one"}, Efforts: []string{"low", "high"}, DefaultModel: "one"}, nil
	}}
	first := c.harness(context.Background(), s, h, HarnessDefault{}, false)
	if first.Default.Model != "one" || !reflect.DeepEqual(first.Efforts, []string{"low", "high"}) {
		t.Fatal(first)
	}
	cached := c.harness(context.Background(), s, h, HarnessDefault{Model: "override"}, false)
	if cached.Default.Model != "override" || !reflect.DeepEqual(cached.Efforts, first.Efforts) || calls != 1 {
		t.Fatal(cached, calls)
	}
	failed = true
	last := c.harness(context.Background(), s, h, HarnessDefault{}, true)
	if calls != 2 || last.Models.Error != "offline" || !reflect.DeepEqual(last.Models.Items, first.Models.Items) || !reflect.DeepEqual(last.Efforts, first.Efforts) || last.Default.Model != "one" {
		t.Fatal(last, calls)
	}
}
