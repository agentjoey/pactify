package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
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
	b := get(true)
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
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
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
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
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

func catalogFixture(t *testing.T) (*harnessCatalog, Store, Harness, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(path, []byte("fake executable"), 0700); err != nil {
		t.Fatal(err)
	}
	c := newHarnessCatalog()
	c.lookupPath = func(string) (string, error) { return path, nil }
	return c, Store{Root: filepath.Join(t.TempDir(), "jobs")}, Harness{Name: "fixture", Version: func(context.Context) (string, error) { return "1", nil }, Models: func(context.Context, cockpit.StartOpts) ([]string, error) { return []string{"one"}, nil }}, path
}

func TestHarnessCatalogParallelRows(t *testing.T) {
	c, s, h, _ := catalogFixture(t)
	original := registry
	t.Cleanup(func() { registry = original })
	registry = map[string]Harness{}
	// Only fakes: no installed harness or credential home is used.
	for _, name := range []string{"agy", "codex", "kimi", "opencode"} {
		fake := h
		fake.Name = name
		fake.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
			time.Sleep(150 * time.Millisecond)
			return []string{"one"}, nil
		}
		registry[name] = fake
	}
	start := time.Now()
	rows, err := c.harnesses(context.Background(), s, false)
	elapsed := time.Since(start)
	t.Logf("four 150ms model fakes: %s (sequential baseline >=600ms)", elapsed)
	if err != nil || len(rows) != 4 {
		t.Fatalf("%v %v", rows, err)
	}
	if elapsed >= 450*time.Millisecond {
		t.Fatalf("discovery is sequential: %s", elapsed)
	}
	for i, name := range Names() {
		if rows[i].Name != name {
			t.Fatalf("order: %v", rows)
		}
	}
	start = time.Now()
	if _, err := c.harnesses(context.Background(), s, false); err != nil {
		t.Fatal(err)
	}
	t.Logf("memory hit: %s", time.Since(start))
	restarted := newHarnessCatalog()
	restarted.lookupPath = c.lookupPath
	start = time.Now()
	if _, err := restarted.harnesses(context.Background(), s, false); err != nil {
		t.Fatal(err)
	}
	t.Logf("restart/disk hit: %s", time.Since(start))
}

func TestHarnessCatalogVersionFingerprint(t *testing.T) {
	c, s, h, path := catalogFixture(t)
	calls := 0
	h.Version = func(context.Context) (string, error) { calls++; return "1", nil }
	get := func(force bool) { c.harness(context.Background(), s, h, HarnessDefault{}, force) }
	get(false)
	get(false)
	if calls != 1 {
		t.Fatalf("cache hit ran version: %d", calls)
	}
	stamp := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	get(false)
	if calls != 2 {
		t.Fatalf("mtime did not invalidate: %d", calls)
	}
	if err := os.WriteFile(path, []byte("size changed"), 0700); err != nil {
		t.Fatal(err)
	}
	get(false)
	if calls != 3 {
		t.Fatalf("size did not invalidate: %d", calls)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	c.lookupPath = func(string) (string, error) { return link, nil }
	get(false)
	if calls != 3 {
		t.Fatalf("symlink target should be the cache key: %d", calls)
	}
	get(true)
	if calls != 4 {
		t.Fatalf("explicit refresh did not rerun version: %d", calls)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("other"), 0700); err != nil {
		t.Fatal(err)
	}
	c.lookupPath = func(string) (string, error) { return other, nil }
	get(false)
	if calls != 5 {
		t.Fatalf("resolved path did not invalidate: %d", calls)
	}
}

func TestHarnessCatalogPersistence(t *testing.T) {
	c, s, h, _ := catalogFixture(t)
	h.ModelDetails = func(context.Context, cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
		return cockpit.ModelDiscovery{Items: []string{"one"}, Warning: "cached warning", Efforts: []string{"high"}, DefaultModel: "one"}, nil
	}
	first := c.harness(context.Background(), s, h, HarnessDefault{}, false)
	h.Version = func(context.Context) (string, error) { t.Fatal("version executed after restart"); return "", nil }
	h.ModelDetails = func(context.Context, cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
		t.Fatal("discovery executed after restart")
		return cockpit.ModelDiscovery{}, nil
	}
	restarted := newHarnessCatalog()
	restarted.lookupPath = c.lookupPath
	second := restarted.harness(context.Background(), s, h, HarnessDefault{}, false)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("%+v != %+v", first, second)
	}
	if err := os.WriteFile(catalogPath(s), []byte("{corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted = newHarnessCatalog()
	restarted.lookupPath = c.lookupPath
	h.Version = func(context.Context) (string, error) { return "2", nil }
	h.ModelDetails = nil
	if row := restarted.harness(context.Background(), s, h, HarnessDefault{}, false); row.Version != "2" || row.Models.Error != "" {
		t.Fatal(row)
	}
}

func waitCatalogRefresh(t *testing.T, c *harnessCatalog, s Store, name string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		running := c.refreshing[s.Root+"\x00"+name]
		c.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background refresh never completed")
}

func TestHarnessCatalogStaleWhileRevalidate(t *testing.T) {
	c, s, h, _ := catalogFixture(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	c.now = func() time.Time { return time.Unix(0, clock.Load()) }
	first := c.harness(context.Background(), s, h, HarnessDefault{}, false)
	clock.Add(int64(11 * time.Minute))
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	h.Models = func(ctx context.Context, _ cockpit.StartOpts) ([]string, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []string{"two"}, nil
	}
	// Background discovery must survive cancellation of the initiating request.
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	row := c.harness(ctx, s, h, HarnessDefault{}, false)
	cancel()
	if !row.Stale || !reflect.DeepEqual(row.Models.Items, first.Models.Items) || time.Since(start) > 100*time.Millisecond {
		t.Fatal(row)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no background discovery")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.harness(context.Background(), s, h, HarnessDefault{}, false) }()
	}
	wg.Wait()
	close(release)
	waitCatalogRefresh(t, c, s, h.Name)
	row = c.harness(context.Background(), s, h, HarnessDefault{}, false)
	if calls.Load() != 1 || row.Stale || !reflect.DeepEqual(row.Models.Items, []string{"two"}) {
		t.Fatal(row, calls.Load())
	}
}

func TestHarnessCatalogFailureTTL(t *testing.T) {
	c, s, h, _ := catalogFixture(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	c.now = func() time.Time { return time.Unix(0, clock.Load()) }
	c.harness(context.Background(), s, h, HarnessDefault{}, false)
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) { return nil, errors.New("offline") }
	failed := c.harness(context.Background(), s, h, HarnessDefault{}, true)
	if failed.Models.Error != "offline" || !reflect.DeepEqual(failed.Models.Items, []string{"one"}) {
		t.Fatal(failed)
	}
	clock.Add(int64(59 * time.Second))
	if c.harness(context.Background(), s, h, HarnessDefault{}, false).Stale {
		t.Fatal("failure expired early")
	}
	clock.Add(int64(2 * time.Second))
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) { return []string{"recovered"}, nil }
	if !c.harness(context.Background(), s, h, HarnessDefault{}, false).Stale {
		t.Fatal("failure did not expire after one minute")
	}
	waitCatalogRefresh(t, c, s, h.Name)
	recovered := c.harness(context.Background(), s, h, HarnessDefault{}, false)
	if recovered.Models.Error != "" || recovered.Stale || !reflect.DeepEqual(recovered.Models.Items, []string{"recovered"}) {
		t.Fatal(recovered)
	}
	clock.Add(int64(2 * time.Minute))
	if c.harness(context.Background(), s, h, HarnessDefault{}, false).Stale {
		t.Fatal("success used failure TTL")
	}
}

func TestHarnessCatalogOpencodeVersionInvalidates(t *testing.T) {
	c, s, h, _ := catalogFixture(t)
	h.Name = "opencode"
	c.harness(context.Background(), s, h, HarnessDefault{}, false)
	h.Version = func(context.Context) (string, error) { return "1.2.0", nil }
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) { return nil, errors.New("offline") }
	row := c.harness(context.Background(), s, h, HarnessDefault{}, true)
	if row.Version != "1.2.0" || len(row.Models.Items) != 0 || row.Models.Error != "offline" {
		t.Fatal(row)
	}
	h.Version = func(context.Context) (string, error) { return "", errors.New("broken version") }
	row = c.harness(context.Background(), s, h, HarnessDefault{}, true)
	if len(row.Models.Items) != 0 || row.Models.Error != "cannot determine opencode CLI version: broken version" {
		t.Fatal(row)
	}
}

func TestHarnessCatalogBackgroundFailureThenExplicitSuccess(t *testing.T) {
	c, s, h, _ := catalogFixture(t)
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	c.now = func() time.Time { return time.Unix(0, clock.Load()) }
	c.harness(context.Background(), s, h, HarnessDefault{}, false)
	clock.Add(int64(11 * time.Minute))
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h.Models = func(ctx context.Context, _ cockpit.StartOpts) ([]string, error) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, errors.New("old background failure")
		}
		return []string{"new success"}, nil
	}
	c.harness(context.Background(), s, h, HarnessDefault{}, false)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no background discovery")
	}
	result := make(chan HarnessInfo, 1)
	go func() { result <- c.harness(context.Background(), s, h, HarnessDefault{}, true) }()
	select {
	case <-result:
		t.Fatal("explicit refresh must wait for in-flight discovery")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case row := <-result:
		if row.Models.Error != "" || !reflect.DeepEqual(row.Models.Items, []string{"new success"}) {
			t.Fatal(row)
		}
	case <-time.After(time.Second):
		t.Fatal("explicit refresh did not complete")
	}
	waitCatalogRefresh(t, c, s, h.Name)
	restarted := newHarnessCatalog()
	restarted.lookupPath = c.lookupPath
	restarted.now = c.now
	row := restarted.harness(context.Background(), s, h, HarnessDefault{}, false)
	if calls.Load() != 2 || row.Models.Error != "" || !reflect.DeepEqual(row.Models.Items, []string{"new success"}) {
		t.Fatal(row, calls.Load())
	}
}

func TestHarnessCatalogPromotionAndAccess(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	c := newHarnessCatalog()
	c.lookupPath = func(string) (string, error) { return "", errors.New("not installed") }
	for _, name := range []string{"codex", "kimi", "agy", "opencode"} {
		t.Run(name, func(t *testing.T) {
			h, ok := Lookup(name)
			if !ok {
				t.Fatal("missing harness")
			}
			row := c.harness(context.Background(), s, h, HarnessDefault{}, false)
			if row.Experimental != (name == "agy") || !row.ReadOnly || row.Sandboxed != (name == "codex") {
				t.Fatalf("catalog promotion or access flags: %+v", row)
			}
		})
	}
}
