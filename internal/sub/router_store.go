package sub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
)

var ErrRouterConflict = errors.New("config changed elsewhere")

func (s Store) RouterPath() string { return filepath.Join(filepath.Dir(s.Root), "router.json") }

// RouterQuota retains the actual quota.json key, including group keys, for Clear.
// Keep QuotaRoute and the existing CLI/cost JSON contracts unchanged.
type RouterQuota struct {
	Key string `json:"key"`
	QuotaRoute
}
type RouterSnapshot struct {
	Path            string        `json:"path"`
	Exists          bool          `json:"exists"`
	Config          *Router       `json:"config"`
	Version         string        `json:"version"`
	ExhaustedRoutes []RouterQuota `json:"exhaustedRoutes"`
}

func routerVersion(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func (s Store) RouterSnapshot(now time.Time) (RouterSnapshot, error) {
	out := RouterSnapshot{Path: s.RouterPath(), Config: &Router{Defaults: map[string]HarnessDefault{}, Aliases: map[string]Route{}, Fallback: map[string][]string{}, QuotaGroups: map[string]QuotaGroup{}}, ExhaustedRoutes: []RouterQuota{}}
	data, err := os.ReadFile(out.Path)
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	out.Exists = err == nil
	out.Version = routerVersion(data)
	if out.Exists {
		out.Config, err = ParseRouter(out.Path, data)
		if err != nil {
			return out, err
		}
	}
	entries, err := s.readQuota(now)
	if err != nil {
		return out, err
	}
	for key, q := range entries {
		out.ExhaustedRoutes = append(out.ExhaustedRoutes, RouterQuota{Key: key, QuotaRoute: q})
	}
	sort.Slice(out.ExhaustedRoutes, func(i, j int) bool { return out.ExhaustedRoutes[i].Key < out.ExhaustedRoutes[j].Key })
	return out, nil
}

// SaveRouter compares the original bytes under the same advisory lock mechanism
// used by quota writers. Atomic rename publishes a private (0600) complete file.
func (s Store) SaveRouter(ctx context.Context, version string, data json.RawMessage) error {
	config, err := ParseRouterStrict(s.RouterPath(), data)
	if err != nil {
		return err
	}
	release, err := lockx.Acquire(ctx, s.RouterPath()+".lock")
	if err != nil {
		return err
	}
	defer release()
	current, err := os.ReadFile(s.RouterPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if version != routerVersion(current) {
		return ErrRouterConflict
	}
	return atomicJSON(s.RouterPath(), config)
}

func (s Store) ClearQuota(ctx context.Context, key string) error {
	release, err := lockx.Acquire(ctx, s.quotaPath()+".lock")
	if err != nil {
		return err
	}
	defer release()
	entries, err := s.readQuota(time.Now())
	if err != nil {
		return err
	}
	delete(entries, key)
	return atomicJSON(s.quotaPath(), entries)
}
