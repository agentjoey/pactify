package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubOpencodeLogin(t *testing.T) {
	for _, version := range []string{"1.19.0", "opencode v2.0.32"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("PACTIFY_HOME", root)
			t.Setenv("OPENCODE_CONFIG", "/untrusted/config.json")
			t.Setenv("OPENCODE_CONFIG_CONTENT", `{"plugin":["untrusted"]}`)
			t.Setenv("OPENCODE_CONFIG_DIR", "/untrusted/config")
			bin := t.TempDir()
			checks := filepath.Join(root, "isolation")
			// The peer records arguments and directory names only; no credentials exist.
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo '" + version + "'; exit; fi\nprintf '%s\\n' \"$@\" > '" + filepath.Join(root, "args") + "'\nprintf '%s\\n' \"$XDG_DATA_HOME\" \"$XDG_CONFIG_HOME\" \"$XDG_STATE_HOME\" \"$XDG_CACHE_HOME\" > '" + filepath.Join(root, "homes") + "'\nprintf '%s\\n' \"$OPENCODE_CONFIG_DIR\" \"$OPENCODE_CONFIG\" \"$OPENCODE_CONFIG_CONTENT\" \"$OPENCODE_DISABLE_PROJECT_CONFIG\" \"$OPENCODE_DISABLE_CLAUDE_CODE\" \"$PWD\" > '" + checks + "'\ncat\n"
			if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			command := newSubCmd()
			var out bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&out)
			command.SetIn(strings.NewReader("interactive-input\n"))
			command.SetArgs([]string{"login", "opencode", "provider", "--", "--method", "oauth"})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if version == "1.19.0" {
				if !strings.Contains(out.String(), "auth.json") {
					t.Fatal(out.String())
				}
				if _, err := os.Stat(filepath.Join(root, "args")); !os.IsNotExist(err) {
					t.Fatal("v1 unexpectedly ran login")
				}
				return
			}
			args, err := os.ReadFile(filepath.Join(root, "args"))
			if err != nil {
				t.Fatal(err)
			}
			if string(args) != "auth\nlogin\n--standalone\nprovider\n--method\noauth\n" {
				t.Fatal(string(args))
			}
			if out.String() != "interactive-input\n" {
				t.Fatal(out.String())
			}
			isolation, err := os.ReadFile(checks)
			if err != nil {
				t.Fatal(err)
			}
			managed := filepath.Join(root, "harness-homes", "opencode2")
			// The fake prints its physical cwd; macOS temp dirs resolve /var to /private/var.
			cwd, err := filepath.EvalSymlinks(managed)
			if err != nil {
				t.Fatal(err)
			}
			expected := filepath.Join(managed, "config", "opencode") + "\n\n\n1\n1\n" + cwd + "\n"
			if string(isolation) != expected {
				t.Fatalf("isolation=%q want=%q", isolation, expected)
			}
			homes, _ := os.ReadFile(filepath.Join(root, "homes"))
			for _, name := range []string{"share", "config", "state", "cache"} {
				if !strings.Contains(string(homes), filepath.Join(root, "harness-homes", "opencode2", name)) {
					t.Fatal(string(homes))
				}
			}
		})
	}
}
