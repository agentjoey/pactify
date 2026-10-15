package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovedGeminiCommands(t *testing.T) {
	cwd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PACTIFY_HOME", t.TempDir())
	for _, args := range [][]string{
		{"init", "--project", "removed", "--seat", "w:worker:GEMINI.md:gemini-cli"},
		{"agent", "register", "gemini-cli"},
		{"agent", "add", "gemini-cli", "--id", "w", "--roles", "worker", "--print"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "gemini-cli has been removed; use antigravity (agy) instead") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRemovedGeminiAuditFlags(t *testing.T) {
	for _, action := range []string{"install", "uninstall"} {
		cmd := newAuditCmd()
		cmd.SetArgs([]string{action, "--gemini"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag: --gemini") {
			t.Errorf("%s error = %v", action, err)
		}
	}
}

func TestRemovedGeminiSetupRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PACTIFY_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "agents.json"), []byte(`{"agents":[{"kind":"gemini-cli"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"suggest"}, {"--yes"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cwd, _ := os.Getwd()
			dir := t.TempDir()
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chdir(cwd) })
			cmd := newSetupCmd()
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "gemini-cli has been removed; use antigravity (agy) instead") {
				t.Errorf("error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".pact")); !os.IsNotExist(err) {
				t.Errorf("setup wrote .pact before validating removed kind: %v", err)
			}
		})
	}
}
