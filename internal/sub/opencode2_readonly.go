package sub

import (
	"context"
	"fmt"
	"github.com/agentjoey/pactify/internal/cockpit"
	"path/filepath"
	"time"
)

// CheckOpencode2Readonly checks the completed job's own persisted session. The
// caller still checks job success and write-target absence independently.
func (store Store) CheckOpencode2Readonly(ctx context.Context, id string, result Result, env []string) error {
	h, _ := Lookup("opencode")
	if !h.ForVersion(result.CLIVersion).Opencode2 {
		return fmt.Errorf("permission evaluation requires a recorded opencode 2.x job")
	}
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	if st.Request.Harness != "opencode" || st.Request.Access != "read-only" || result.SessionID == "" {
		return fmt.Errorf("permission evaluation requires this probe's readonly opencode session")
	}
	env = append(append([]string{}, env...), opencodeVendorStrip()...)
	for i, d := range opencode2Homes().directories() {
		home := st.HarnessHomes[d.Env]
		if i == 0 {
			home = st.HarnessHome
		}
		if home == "" || !filepath.IsAbs(home) {
			return fmt.Errorf("permission evaluation requires the job's saved %s", d.Env)
		}
		env = append(env, d.Env+"="+home)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return cockpit.CheckOpencode2Readonly(ctx, cockpit.StartOpts{RepoDir: st.Request.Cwd, Env: env, MinimalConfig: st.EffectiveConfigMode() == "minimal", Access: cockpit.AccessReadOnly}, result.SessionID)
}
