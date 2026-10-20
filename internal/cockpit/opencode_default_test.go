package cockpit

import (
	"context"
	"github.com/agentjoey/pactify/internal/testenv"
	"testing"
	"time"
)

func TestOpencode2DiscoveryDefaultModel(t *testing.T) {
	for _, scenario := range []string{"write", "default-null", "default-missing"} {
		t.Run(scenario, func(t *testing.T) {
			opts := oc2Opts(t)
			t.Setenv("COCKPIT_OC2_SCENARIO", scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			d, err := Opencode2ModelDetails(ctx, opts)
			want := "opencode/test-free"
			if scenario != "write" {
				want = ""
			}
			if err != nil || d.DefaultModel != want {
				t.Fatalf("details=%+v err=%v, want default=%q", d, err, want)
			}
		})
	}
}

func TestOpencodeDiscoveryDefaultModel(t *testing.T) {
	for _, configured := range []string{"opencode/paid", "", "missing/model"} {
		t.Run(configured, func(t *testing.T) {
			testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OPENCODE_DEFAULT_MODEL", configured)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			d, err := OpencodeModelDetails(ctx, StartOpts{RepoDir: t.TempDir()})
			want := configured
			if want == "missing/model" {
				want = ""
			}
			if err != nil || d.DefaultModel != want {
				t.Fatalf("details=%+v err=%v, want default=%q", d, err, want)
			}
		})
	}
}
