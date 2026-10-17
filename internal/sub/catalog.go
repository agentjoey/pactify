package sub

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/orchestrate"
)

type ModelCatalog struct {
	Warning   string    `json:"warning,omitempty"`
	Items     []string  `json:"items"`
	Error     string    `json:"error"`
	FetchedAt time.Time `json:"fetchedAt"`
}
type HarnessInfo struct {
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

type harnessCatalogEntry struct {
	Models       ModelCatalog
	Efforts      []string
	DefaultModel string
}

type harnessCatalog struct {
	mu         sync.Mutex
	entries    map[string]harnessCatalogEntry
	keyLocks   map[string]*sync.Mutex
	now        func() time.Time
	lookupPath func(string) (string, error)
}

func newHarnessCatalog() *harnessCatalog {
	return &harnessCatalog{entries: map[string]harnessCatalogEntry{}, now: time.Now, lookupPath: exec.LookPath}
}

var processHarnessCatalog = newHarnessCatalog()

// Harnesses uses only version and catalog discovery commands, never a prompt or
// a doctor probe. Cache keys include the store to keep independent homes separate.
func (s Store) Harnesses(ctx context.Context, refresh bool) ([]HarnessInfo, error) {
	router, err := s.LoadRouter()
	if err != nil {
		return nil, err
	}
	rows := []HarnessInfo{}
	for _, name := range Names() {
		h, _ := Lookup(name)
		var defaults HarnessDefault
		if router != nil {
			defaults = router.Defaults[name]
		}
		rows = append(rows, processHarnessCatalog.harness(ctx, s, h, defaults, refresh))
	}
	return rows, nil
}
func (c *harnessCatalog) harness(ctx context.Context, s Store, h Harness, defaults HarnessDefault, refresh bool) HarnessInfo {
	row := HarnessInfo{Sandboxed: h.Name == "codex", Name: h.Name, Default: defaults, Efforts: h.Efforts(), Experimental: h.Experimental != "", ReadOnly: h.NoReadOnly == ""}
	row.VerifiedVersion = readVerifiedVersions(filepath.Join(filepath.Dir(s.Root), "verified-versions.json"))[h.Name].Version
	_, err := c.lookupPath(h.Name)
	row.Installed = err == nil
	if row.Installed && h.Version != nil {
		row.Version, _ = h.Version(ctx)
	}
	// Serialize only this store/harness; external discovery never holds c.mu.
	key := s.Root + "\x00" + h.Name
	c.mu.Lock()
	if c.keyLocks == nil {
		c.keyLocks = map[string]*sync.Mutex{}
	}
	keyLock := c.keyLocks[key]
	if keyLock == nil {
		keyLock = &sync.Mutex{}
		c.keyLocks[key] = keyLock
	}
	c.mu.Unlock()
	keyLock.Lock()
	defer keyLock.Unlock()
	c.mu.Lock()
	cached, ok := c.entries[key]
	c.mu.Unlock()
	now := c.now().UTC()
	if ok && !refresh && now.Sub(cached.Models.FetchedAt) < 10*time.Minute && row.Installed {
		return row.withCatalog(cached)
	}
	next := cached
	next.Models = ModelCatalog{Items: append([]string{}, cached.Models.Items...), FetchedAt: now}
	switch {
	case !row.Installed:
		next.Models.Error = "harness is not installed"
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
	c.mu.Lock()
	c.entries[key] = next
	c.mu.Unlock()
	return row.withCatalog(next)
}

// Router overrides remain authoritative; native defaults and dynamic effort
// metadata share the exact same TTL and stale-on-error behavior as model ids.
func (row HarnessInfo) withCatalog(cached harnessCatalogEntry) HarnessInfo {
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
	opts := cockpit.StartOpts{Env: orchestrate.CrossVendorStrip(h.Kind), MinimalConfig: true}
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
