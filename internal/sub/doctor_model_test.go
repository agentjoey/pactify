package sub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestDoctorModelOverride(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode", "fake"} {
		for _, listingFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/listing-fails=%t", name, listingFails), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("PACTIFY_HOME", home)
				t.Setenv("PACTIFY_SUB_FAKE", "1")
				const model = "minimax-coding-plan/MiniMax-M3"
				// Replace external operations; preserve both default-selection inputs
				// so an explicit model must beat DoctorModel and live-list heuristics.
				h := Harness{Name: name, DoctorModel: "preset", HidesItself: true,
					Version: func(context.Context) (string, error) { return "test", nil }}
				if name == "agy" || name == "opencode" {
					h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
						if listingFails {
							return nil, errors.New("catalog unavailable")
						}
						return []string{"test/pro", "test/flash", "test/low-low", model}, nil
					}
				}
				calls := 0
				reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{
					model:    model,
					lookPath: func(string) (string, error) { return "test", nil },
					run: func(_ context.Context, s Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
						calls++
						st, err := s.Load(id)
						if err != nil {
							t.Fatal(err)
						}
						if st.Request.Model != model {
							t.Errorf("%s probe model=%q, want %q", st.Request.Access, st.Request.Model, model)
						}
						if st.Request.Access == "write" {
							for file, content := range map[string]string{"out.txt": "ok", "doctor-network.json": `{"bind":true,"external":true}`} {
								if err := os.WriteFile(filepath.Join(st.Request.Cwd, file), []byte(content), 0600); err != nil {
									t.Fatal(err)
								}
							}
						}
						if name == "opencode" && st.Request.Access == "read-only" {
							writeDeniedDoctorLog(t, s, id)
						}
						return Result{Status: StatusSucceeded, CLIVersion: "test", ModelActual: st.Request.Model, ModelVerified: true, FinalText: "done", InputTokens: 1, ToolCalls: 1}, nil
					},
				})
				if err != nil || len(reports) != 1 || !reports[0].Passed() || calls != 2 {
					t.Fatalf("reports=%+v calls=%d err=%v", reports, calls, err)
				}
				table := readVerifiedVersions(filepath.Join(home, "verified-versions.json"))
				if table[name].Version != "test" || table[name].VerifiedAt.IsZero() {
					t.Fatalf("verified table=%+v", table)
				}
			})
		}
	}
}

func TestDoctorExplicitModelUsesBackendValidation(t *testing.T) {
	for _, fault := range []string{"unavailable", "unsupported", "mismatch", "unverified"} {
		t.Run(fault, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			t.Setenv("PACTIFY_SUB_FAKE", "1")
			if err := recordVerifiedVersion(context.Background(), home, "fake", "previous", time.Now()); err != nil {
				t.Fatal(err)
			}
			const model = "chosen"
			var backends []*fakeBackend
			h := fakeHarness()
			h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
				t.Error("explicit models must use the normal backend validation, without a separate catalog preflight")
				return nil, errors.New("catalog unavailable")
			}
			h.Backend = func() cockpit.Backend {
				s := &fakeSession{events: make(chan cockpit.Event, 8)}
				b := &fakeBackend{sess: s}
				backends = append(backends, b)
				if fault == "unavailable" {
					b.startErr = fmt.Errorf("%w: %q; available: alternative", cockpit.ErrModelUnavailable, model)
				} else if fault == "unsupported" {
					b.startErr = fmt.Errorf("%w (asked for %q)", cockpit.ErrModelUnsupported, model)
				}
				s.script = func(s *fakeSession) {
					s.info.Model = "other"
					s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 1}}
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "done"}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
				return b
			}
			if fault == "unverified" {
				h.Verify = func(context.Context, VerificationInput) Verification { return Verification{Model: model} }
			}
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{model: model})
			if err != nil || len(reports) != 1 || reports[0].Passed() || len(backends) != 2 {
				t.Fatalf("reports=%+v backends=%d err=%v", reports, len(backends), err)
			}
			for _, b := range backends {
				if b.gotOpts.Model != model {
					t.Errorf("backend model=%q, want %q", b.gotOpts.Model, model)
				}
				if b.startErr != nil && !strings.Contains(reports[0].Checks[1].Reason, b.startErr.Error()) {
					t.Errorf("backend refusal missing from doctor output: %+v", reports[0])
				}
			}
			if reports[0].Checks[2].Status != "FAIL" {
				t.Errorf("model check=%+v", reports[0].Checks[2])
			}
			if table := readVerifiedVersions(filepath.Join(home, "verified-versions.json")); table["fake"].Version != "previous" {
				t.Fatalf("failed explicit model overwrote verified version: %+v", table)
			}
		})
	}
}

func TestDoctorModelRequiresSingleHarness(t *testing.T) {
	for _, names := range [][]string{nil, {"codex", "kimi"}} {
		var out strings.Builder
		passed, err := Doctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, names, &out, DoctorOptions{Model: "chosen"})
		if passed || err == nil || !strings.Contains(err.Error(), "requires exactly one harness") || out.Len() != 0 {
			t.Fatalf("names=%v passed=%t err=%v output=%s", names, passed, err, out.String())
		}
	}
}
