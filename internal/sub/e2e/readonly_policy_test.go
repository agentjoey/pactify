//go:build harness_e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/sub"
	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencodeServePeer(t *testing.T) { testenv.OpencodeServePeer(t) }

// Exercise the live scenario assertions offline: a completed CLI job has no
// denied-write events because upstream removed the tools before execution.
func TestOpencodeReadonlyPolicyScenarios(t *testing.T) {
	for _, scenario := range []string{"denied", "long"} {
		t.Run(scenario, func(t *testing.T) {
			testenv.InstallOpencodeServe(t)
			h, _ := sub.Lookup("opencode")
			s := &suite{owner: t, harness: "opencode", adapter: h.ForVersion("1.19.0"), quarantine: t.TempDir()}
			f := s.newFixture(t, false)
			requests := filepath.Join(f.root, "requests.jsonl")
			f.env = append(f.env, "COCKPIT_OC_PEER=1", "COCKPIT_OC_REQUESTS="+requests, "FIXTURE_ROOT="+f.root)
			ctx := context.Background()
			session, err := cockpit.NewOpencodeBackend().Start(ctx, cockpit.StartOpts{RepoDir: f.repo, Env: f.env, Access: cockpit.AccessReadOnly})
			must(t, err)
			must(t, session.Close())
			must(t, os.Remove(requests))
			store := sub.Store{Root: filepath.Join(f.root, "pactify/jobs")}
			st, err := store.Create("readonly", sub.Request{Harness: "opencode", Access: "read-only", Cwd: f.repo})
			must(t, err)
			st.HarnessHome = envValue(f.env, "XDG_DATA_HOME")
			st.HarnessHomes = map[string]string{"XDG_CONFIG_HOME": envValue(f.env, "XDG_CONFIG_HOME")}
			st.Status = sub.StatusSucceeded
			must(t, store.Save(st))
			must(t, os.WriteFile(filepath.Join(store.Dir(st.ID), "log.jsonl"), nil, 0600))
			s.bin = filepath.Join(f.root, "fake-pactify")
			script := `#!/bin/sh
case "$2" in
run) cat > "$FIXTURE_ROOT/prompt"; printf '{"jobId":"readonly"}';;
wait)
  if [ -f part-01.txt ]; then
    answer="E2E_TOKENS=$(awk '{printf "%s", $2}' part-*.txt)"
    calls=60
  else
    answer=E2E_DENIED_DONE
    calls=0
  fi
  printf '{"jobId":"readonly","sessionId":"sid","cliVersion":"1.19.0","status":"succeeded","finalText":"%s","toolCalls":%s}' "$answer" "$calls";;
status|result) printf '{"status":"succeeded"}';;
*) exit 42;;
esac
`
			must(t, os.WriteFile(s.bin, []byte(script), 0700))
			if scenario == "denied" {
				readonlyDenied(f)
				prompt, err := os.ReadFile(filepath.Join(f.root, "prompt"))
				must(t, err)
				if strings.Contains(string(prompt), "actually attempt") || envValue(f.env, "OPENCODE_CONFIG_CONTENT") != "" {
					t.Error("opencode still receives a prompt or override demanding unavailable write tools")
				}
			} else {
				readonlyLong(f)
			}
			rows, err := os.ReadFile(requests)
			if err != nil || !strings.Contains(string(rows), `"path":"/session/sid"`) || strings.Contains(string(rows), `"method":"PATCH"`) || strings.Contains(string(rows), `"method":"POST"`) {
				t.Fatalf("must read the original session policy without replacing it or prompting: %s, err=%v", rows, err)
			}
		})
	}
}
