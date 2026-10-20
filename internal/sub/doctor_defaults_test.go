package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestDoctorNativeDefaultModel(t *testing.T) {
	for _, name := range []string{"codex", "kimi"} {
		for _, actual := range []string{"native/model", ""} {
			t.Run(name+"/"+actual, func(t *testing.T) {
				h, _ := Lookup(name)
				h.Home = nil
				h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
					t.Fatal("native defaults must not require model listing")
					return nil, nil
				}
				reports := doctorDefaultProbe(t, h, "", actual)
				if got := reports[0].Passed(); got != (actual != "") {
					t.Fatalf("reported model=%q, passed=%t: %+v", actual, got, reports)
				}
			})
		}
	}
}

func TestDoctorOpencodeConfiguredDefault(t *testing.T) {
	for _, configured := range []string{"user/pro", ""} {
		t.Run(configured, func(t *testing.T) {
			h, _ := Lookup("opencode")
			h.Home = nil
			h.ModelDetails = func(context.Context, cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
				return cockpit.ModelDiscovery{Items: []string{"user/pro", "test/flash-free"}, DefaultModel: configured}, nil
			}
			h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
				return []string{"user/pro", "test/flash-free"}, nil
			}
			want := configured
			if want == "" {
				want = "test/flash-free"
			}
			reports := doctorDefaultProbe(t, h, want, want)
			if !reports[0].Passed() {
				t.Fatalf("reports=%+v", reports)
			}
		})
	}
}

func doctorDefaultProbe(t *testing.T, h Harness, requested, actual string, router ...string) []DoctorReport {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	if len(router) > 0 {
		if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(router[0]), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h.Version = func(context.Context) (string, error) { return "test", nil }
	h.Supported = nil
	h.HidesItself = true
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{
		lookPath: func(string) (string, error) { return "test", nil },
		run: func(_ context.Context, s Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
			st, err := s.Load(id)
			if err != nil {
				t.Fatal(err)
			}
			if st.Request.Model != requested {
				t.Errorf("requested=%q, want %q", st.Request.Model, requested)
			}
			if st.Request.Access == "write" {
				for file, content := range map[string]string{"out.txt": "ok", "doctor-network.json": `{"bind":true,"external":true}`} {
					if err := os.WriteFile(filepath.Join(st.Request.Cwd, file), []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			return Result{Status: StatusSucceeded, CLIVersion: "test", ModelActual: actual, ModelVerified: actual != "", FinalText: "done", InputTokens: 1, ToolCalls: 1}, nil
		},
		readonlyCheck: func(context.Context, Store, string, Result) error { return nil },
	})
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports=%+v err=%v", reports, err)
	}
	return reports
}

func TestDoctorOpencodeRouterDefault(t *testing.T) {
	h, _ := Lookup("opencode")
	h.Home = nil
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
		t.Fatal("configured model should use backend validation")
		return nil, nil
	}
	h.ModelDetails = func(context.Context, cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
		t.Fatal("configured model should use backend validation")
		return cockpit.ModelDiscovery{}, nil
	}
	reports := doctorDefaultProbe(t, h, "user/pro", "user/pro", `{"defaults":{"opencode":{"model":"user/pro"}}}`)
	if !reports[0].Passed() {
		t.Fatalf("reports=%+v", reports)
	}
}
