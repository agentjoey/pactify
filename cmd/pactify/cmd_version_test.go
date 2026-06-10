package main

import (
	"strings"
	"testing"
)

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
