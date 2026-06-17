package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentManifestValidate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "m.toml")
	os.WriteFile(f, []byte("kind=\"myx\"\nbinary=\"myx\"\n[runner]\nargs=[\"run\",\"{briefing}\"]\n"), 0o644)
	out := runManifest(t, "validate", f)
	if !contains(out, "OK") {
		t.Fatalf("validate should print OK: %s", out)
	}

	bad := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(bad, []byte("binary=\"x\"\n"), 0o644)
	cmd := newAgentManifestCmd()
	cmd.SetArgs([]string{"validate", bad})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("validate of an invalid manifest must error")
	}
}

func runManifest(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newAgentManifestCmd()
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("manifest %v: %v", args, err)
	}
	return b.String()
}

func TestAgentManifestAddRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	src := filepath.Join(t.TempDir(), "m.toml")
	os.WriteFile(src, []byte("kind=\"myx\"\nbinary=\"myx\"\n[runner]\nargs=[\"run\",\"{briefing}\"]\n"), 0o644)

	runManifest(t, "add", src)
	if _, err := os.Stat(filepath.Join(home, ".pactify", "agents", "myx.toml")); err != nil {
		t.Fatalf("manifest not installed: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(bad, []byte("kind=\"opencode\"\nbinary=\"x\"\n"), 0o644)
	cmd := newAgentManifestCmd()
	cmd.SetArgs([]string{"add", bad})
	cmd.SetOut(&bytes.Buffer{})
	if cmd.Execute() == nil {
		t.Fatal("add of a built-in kind must error")
	}
	runManifest(t, "remove", "myx")
	if _, err := os.Stat(filepath.Join(home, ".pactify", "agents", "myx.toml")); !os.IsNotExist(err) {
		t.Fatal("manifest not removed")
	}
}
