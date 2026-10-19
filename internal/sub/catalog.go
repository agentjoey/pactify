package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/vendorenv"
)

type ModelCatalog struct {
	Warning   string    `json:"warning,omitempty"`
	Items     []string  `json:"items"`
	Error     string    `json:"error"`
	FetchedAt time.Time `json:"fetchedAt"`
}
type HarnessInfo struct {
	Stale           bool           `json:"stale,omitempty"`
	Sandboxed       bool           `json:"sandboxed"`
	Name            string         `json:"name"`
	Installed       bool           `json:"installed"`
	Version         string         `json:"version"`
	VerifiedVersion string         `json:"verifiedVersion"`
	Experimental    bool           `json:"experimental"`
	ReadOnly        bool           `json:"readOnly"`
	Models          ModelCatalog   `json:"models"`
	Default         HarnessDefault `json:"default"`
	Efforts         []string       `json:"efforts"`
}

const (
	catalogTTL        = 10 * time.Minute
	catalogFailureTTL = time.Minute
	// Covers a version probe (10s), either agy generation (15s each), and setup.
	catalogDiscoveryTimeout = 45 * time.Second
)

type executableStamp struct {
	Path    string
	Size    int64
	ModTime time.Time
}

type harnessCatalogEntry struct {
	Executable   executableStamp
	Version      string
	VersionError string
	Models       ModelCatalog
	Efforts      []string
	DefaultModel string
}

func (e harnessCatalogEntry) stale(now time.Time) bool {
	ttl := catalogTTL
	if e.Models.Error != "" {
		ttl = catalogFailureTTL
	}
	return now.Sub(e.Models.FetchedAt) >= ttl
}

type harnessCatalog struct {
	mu         sync.Mutex
	entries    map[string]harnessCatalogEntry
	keyLocks   map[string]*sync.Mutex
	refreshing map[string]bool
	// Disk I/O is serialized separately; discovery never holds either global lock.
	diskMu     sync.Mutex
	loaded     map[string]bool
	now        func() time.Time
	lookupPath func(string) (string, error)
}

func newHarnessCatalog() *harnessCatalog {
	return &harnessCatalog{
		entries: map[string]harnessCatalogEntry{}, keyLocks: map[string]*sync.Mutex{},
		refreshing: map[string]bool{}, loaded: map[string]bool{},
		now: time.Now, lookupPath: exec.LookPath,
	}
}

var processHarnessCatalog = newHarnessCatalog()

func catalogPath(s Store) string {
	return filepath.Join(filepath.Dir(s.Root), "harness-catalog.json")
}

func (c *harnessCatalog) load(s Store) {
	c.diskMu.Lock()
	defer c.diskMu.Unlock()
	if c.loaded[s.Root] {
		return
	}
	c.loaded[s.Root] = true
	data, err := os.ReadFile(catalogPath(s))
	if err != nil {
		return
	}
	var entries map[string]harnessCatalogEntry
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, entry := range entries {
		if !entry.Models.FetchedAt.IsZero() {
			c.entries[s.Root+"\x00"+name] = entry
		}
	}
}

func (c *harnessCatalog) persist(s Store) {
	c.diskMu.Lock()
	defer c.diskMu.Unlock()
	entries := map[string]harnessCatalogEntry{}
	prefix := s.Root + "\x00"
	c.mu.Lock()
	for key, entry := range c.entries {
		if name, ok := strings.CutPrefix(key, prefix); ok {
			entries[name] = entry
		}
	}
	c.mu.Unlock()
	// Persistence is an optimization: unwritable homes still use the memory cache.
	_ = atomicJSON(catalogPath(s), entries)
}

func (c *harnessCatalog) executable(name string) (executableStamp, bool) {
	path, err := c.lookupPath(name)
	if err != nil {
		return executableStamp{}, false
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	stamp := executableStamp{Path: path}
	if info, err := os.Stat(path); err == nil {
		stamp.Size, stamp.ModTime = info.Size(), info.ModTime().UTC()
	}
	return stamp, true
}

// Harnesses uses only version and catalog discovery commands, never a prompt or
// a doctor probe. Cache keys include the store to keep independent homes separate.
func (s Store) Harnesses(ctx context.Context, refresh bool) ([]HarnessInfo, error) {
	return processHarnessCatalog.harnesses(ctx, s, refresh)
}

func (c *harnessCatalog) harnesses(ctx context.Context, s Store, refresh bool) ([]HarnessInfo, error) {
	router, err := s.LoadRouter()
	if err != nil {
		return nil, err
	}
	names := Names()
	rows := make([]HarnessInfo, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, _ := Lookup(name)
			var defaults HarnessDefault
			if router != nil {
				defaults = router.Defaults[name]
			}
			rows[i] = c.harness(ctx, s, h, defaults, refresh)
		}()
	}
	wg.Wait()
	return rows, nil
}

func (c *harnessCatalog) harness(ctx context.Context, s Store, h Harness, defaults HarnessDefault, refresh bool) HarnessInfo {
	row := HarnessInfo{Sandboxed: h.Name == "codex", Name: h.Name, Default: defaults, Efforts: h.Efforts(), Experimental: h.Experimental != "", ReadOnly: h.NoReadOnly == ""}
	row.VerifiedVersion = readVerifiedVersions(filepath.Join(filepath.Dir(s.Root), "verified-versions.json"))[h.Name].Version
	c.load(s)
	stamp, installed := c.executable(h.Name)
	row.Installed = installed
	key := s.Root + "\x00" + h.Name
	c.mu.Lock()
	keyLock := c.keyLocks[key]
	if keyLock == nil {
		keyLock = &sync.Mutex{}
		c.keyLocks[key] = keyLock
	}
	cached, ok := c.entries[key]
	if ok && !refresh && cached.Executable == stamp {
		row.Stale = cached.stale(c.now())
		if row.Stale && !c.refreshing[key] {
			c.refreshing[key] = true
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), catalogDiscoveryTimeout)
				defer cancel()
				c.refresh(ctx, s, h, key, keyLock, false)
				c.mu.Lock()
				delete(c.refreshing, key)
				c.mu.Unlock()
			}()
		}
		c.mu.Unlock()
		return row.withCatalog(cached)
	}
	c.mu.Unlock()
	return row.withCatalog(c.refresh(ctx, s, h, key, keyLock, refresh))
}

func (c *harnessCatalog) refresh(ctx context.Context, s Store, h Harness, key string, keyLock *sync.Mutex, force bool) harnessCatalogEntry {
	// Only this store/harness is serialized. Re-read after locking so an older
	// queued refresh cannot overwrite newer success with an obsolete failure.
	keyLock.Lock()
	defer keyLock.Unlock()
	stamp, installed := c.executable(h.Name)
	c.mu.Lock()
	cached, ok := c.entries[key]
	c.mu.Unlock()
	if ok && !force && cached.Executable == stamp && !cached.stale(c.now()) {
		return cached
	}
	ctx, cancel := context.WithTimeout(ctx, catalogDiscoveryTimeout)
	defer cancel()
	version, versionError := cached.Version, cached.VersionError
	if !installed {
		version, versionError = "", ""
	} else if !ok || force || cached.Executable != stamp || versionError != "" {
		version, versionError = "", ""
		if h.Version != nil {
			var err error
			version, err = h.Version(ctx)
			if err != nil {
				versionError = err.Error()
			}
		}
	}
	h = h.forVersion(version)
	if h.Name == "opencode" && (versionError != "" || cached.Version != version) {
		cached = harnessCatalogEntry{}
	}
	next := cached
	next.Executable, next.Version, next.VersionError = stamp, version, versionError
	next.Models.Error = ""
	next.Models.Items = append([]string{}, cached.Models.Items...)
	switch {
	case !installed:
		next.Models.Error = "harness is not installed"
	case h.Name == "opencode" && versionError != "":
		next.Models.Error = "cannot determine opencode CLI version: " + versionError
	case h.unsupported(version) != nil:
		next.Models.Error = h.unsupported(version).Error()
	case h.Models == nil:
		next.Models.Items = []string{}
		next.Efforts, next.DefaultModel = nil, ""
		next.Models.Error = "model discovery is not supported by this harness"
	default:
		details, err := discoverHarnessModelsIn(ctx, h, filepath.Dir(s.Root))
		if err != nil {
			next.Models.Error = err.Error()
		} else {
			next.Models.Items = append([]string{}, details.Items...)
			next.Models.Warning = details.Warning
			next.Efforts = append([]string{}, details.Efforts...)
			next.DefaultModel = details.DefaultModel
		}
	}
	next.Models.FetchedAt = c.now().UTC()
	c.mu.Lock()
	c.entries[key] = next
	c.mu.Unlock()
	c.persist(s)
	return next
}

// Router overrides remain authoritative; native defaults and dynamic effort
// metadata share the exact same TTL and stale-on-error behavior as model ids.
func (row HarnessInfo) withCatalog(cached harnessCatalogEntry) HarnessInfo {
	row.Version = cached.Version
	row.Models = cached.Models
	if len(cached.Efforts) > 0 {
		row.Efforts = append([]string{}, cached.Efforts...)
	}
	if row.Default.Model == "" {
		row.Default.Model = cached.DefaultModel
	}
	return row
}

// discoverHarnessModels follows doctor's environment/home isolation but never
// enters Doctor or Backend.Start/Prompt. Codex only lists models after its
// handshake; kimi reads ACP session options in a disposable home. AgyModels lists ids; OpencodeModels only
// starts serve and reads /config/providers, without sending a prompt.
func discoverHarnessModelsIn(ctx context.Context, h Harness, storeHome string) (catalog cockpit.ModelDiscovery, err error) {
	opts := cockpit.StartOpts{Env: vendorenv.Strip(h.Kind), MinimalConfig: true}
	discover := func() (cockpit.ModelDiscovery, error) {
		if h.ModelDetails != nil {
			return h.ModelDetails(ctx, opts)
		}
		items, err := h.Models(ctx, opts)
		return cockpit.ModelDiscovery{Items: items}, err
	}
	// Kimi owns a disposable discovery home and can fall back without login.
	if h.Name == "kimi" {
		home := filepath.Join(storeHome, "harness-homes", "kimi")
		opts.Env = append(opts.Env, "KIMI_CODE_HOME="+home, "PACTIFY_HOME="+storeHome)
		return discover()
	}
	dir, err := os.MkdirTemp("", "pactify-models-*")
	if err != nil {
		return catalog, err
	}
	defer os.RemoveAll(dir)
	opts.RepoDir = dir
	if h.Home != nil {
		_, _, env, layouts, e := prepareHarnessHomes(ctx, h.Home, "", nil, true, filepath.Join(storeHome, "credential-quarantine"))
		defer func() {
			for _, layout := range layouts {
				if warnings := layout.Cleanup(); len(warnings) > 0 {
					err = fmt.Errorf("model discovery cleanup failed: %v (discovery error: %v)", warnings, err)
				}
			}
		}()
		if e != nil {
			return catalog, e
		}
		opts.Env = append(opts.Env, env...)
	}
	if h.Name == "opencode" {
		opts.Env = append(opts.Env, opencodeVendorStrip()...)
	}
	return discover()
}
