package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildIdentityFallback(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.13.0"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "revision"}, {Key: "vcs.time", Value: "2026-10-21T01:02:03Z"},
	}}
	for _, tc := range []struct {
		name, v, c, d, wantV, wantC, wantD string
		info                               *debug.BuildInfo
	}{
		{"go-install", "dev", "none", "unknown", "0.13.0", "revision", "2026-10-21T01:02:03Z", info},
		{"release", "v1.0.0", "release-commit", "release-date", "v1.0.0", "release-commit", "release-date", info},
		{"partial-ldflags", "dev", "injected", "injected-date", "0.13.0", "injected", "injected-date", info},
		{"unavailable", "dev", "none", "unknown", "dev", "none", "unknown", nil},
		{"devel", "dev", "none", "unknown", "dev", "revision", "unknown", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "revision"}}}},
		{"empty", "dev", "none", "unknown", "dev", "none", "unknown", &debug.BuildInfo{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, c, d := buildIdentity(tc.v, tc.c, tc.d, tc.info)
			if v != tc.wantV || c != tc.wantC || d != tc.wantD {
				t.Fatalf("got %q %q %q", v, c, d)
			}
		})
	}
}

func TestVersionStringDefault(t *testing.T) {
	got := versionString("dev", "none", "unknown")
	want := "pactify dev (none, unknown)"
	if got != want {
		t.Fatalf("versionString = %q, want %q", got, want)
	}
}

func TestVersionStringInjected(t *testing.T) {
	got := versionString("v0.3.0", "abc1234", "2026-06-10")
	if !strings.Contains(got, "v0.3.0") || !strings.Contains(got, "abc1234") {
		t.Fatalf("versionString = %q", got)
	}
}
