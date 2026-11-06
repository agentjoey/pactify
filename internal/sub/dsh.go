package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func dshHomeDir() (string, error) {
	if h := os.Getenv("DSH_HOME"); h != "" {
		return filepath.Abs(h)
	}
	h, err := os.UserHomeDir()
	return filepath.Join(h, ".dsh"), err
}
func managedDSHHome() (string, error) { return managedHarnessHome("dsh") }
func dshHome(home, source string) *managedHome {
	links := map[string]homeLink{}
	target := filepath.Join(source, ".credentials.yaml")
	if _, err := os.Stat(target); err == nil {
		links[".credentials.yaml"] = homeLink{Target: target}
	}
	// Keep this link non-Credential: the file holds a static API key and the
	// SDK process does not write it. The symlink shares the source file; Pactify
	// inspects link metadata only and never reads or copies its contents.
	return &managedHome{Name: "dsh", Home: home, Source: source, AllowMissingSource: true, Links: links}
}
func dshSupported(v string) error {
	if !cockpit.DSHSupportedVersion(v) {
		return fmt.Errorf("dsh %s is unsupported: only the 0.2.1 SDK line is supported; recover with npm install -g @deepseek-ai/dsh@0.2.1-rc.5", v)
	}
	return nil
}
func doctorDSHHome(store Store, id string, r Result, succeeded bool) error {
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	if !succeeded || st.EffectiveConfigMode() != "minimal" || st.HarnessHome == "" {
		return fmt.Errorf("dsh managed-home probe did not succeed")
	}
	source, err := dshHomeDir()
	if err != nil {
		return err
	}
	return dshHome(st.HarnessHome, source).Check()
}
func verifyDSH(_ context.Context, v VerificationInput) Verification {
	s, ok := v.Session.(interface{ DSHUsage() cockpit.DSHUsageRecord })
	if !ok {
		return Verification{UsagePartial: true, Warning: "dsh assistant usage unavailable"}
	}
	r := s.DSHUsage()
	return Verification{Model: r.Model, Verified: r.Model != "", InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, UsageRecorded: r.Recorded, UsagePartial: !r.Recorded}
}
