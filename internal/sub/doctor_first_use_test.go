package sub

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorDefaultInstalledOnly(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	var out bytes.Buffer
	passed, err := Doctor(context.Background(), s, nil, &out)
	if err != nil || !passed {
		t.Errorf("passed=%v err=%v output=%s", passed, err, out.String())
	}
	if got := strings.Count(out.String(), "SKIP Installed: not installed"); got != len(registry) {
		t.Errorf("skip count=%d output=%s", got, out.String())
	}
	if !strings.Contains(out.String(), "agy (experimental)") {
		t.Errorf("missing experimental label: %s", out.String())
	}
}

func TestDoctorDefaultNoneInstalled(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("PACTIFY_SUB_FAKE", "")
	var out bytes.Buffer
	passed, err := Doctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, nil, &out)
	if passed || err == nil || !strings.Contains(err.Error(), "Install") || !strings.Contains(err.Error(), "pactify sub doctor") {
		t.Fatalf("passed=%v err=%v output=%s", passed, err, out.String())
	}
	if strings.Contains(out.String(), "FAIL Installed") {
		t.Fatalf("missing defaults must skip: %s", out.String())
	}
}
