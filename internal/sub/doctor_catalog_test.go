package sub

import (
	"context"
	"github.com/agentjoey/pactify/internal/cockpit"
	"path/filepath"
	"testing"
)

func TestDoctorFixedModelSkipsDiscovery(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	home := t.TempDir()
	for _, name := range []string{"codex", "kimi"} {
		t.Run(name, func(t *testing.T) {
			h := Harness{Name: name, DoctorModel: "fixed-probe", HidesItself: true, Version: func(context.Context) (string, error) { return "test", nil }, Models: func(context.Context, cockpit.StartOpts) ([]string, error) {
				t.Error("fixed doctor model must not be replaced by discovered models")
				return []string{"other-model"}, nil
			}}
			_, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, name, "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "/fixture", nil }, run: func(_ context.Context, s Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
				st, err := s.Load(id)
				if err != nil {
					t.Fatal(err)
				}
				if st.Request.Model != "fixed-probe" {
					t.Errorf("model = %s", st.Request.Model)
				}
				return Result{Status: StatusFailed}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
