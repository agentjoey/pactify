package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAgyPrivateDirectoriesWithoutLogin(t *testing.T) {
	for _, loggedIn := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-token", true: "token"}[loggedIn], func(t *testing.T) {
			source, home := t.TempDir(), filepath.Join(t.TempDir(), "private")
			if loggedIn {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(source, agyTokenPath)), 0700); err != nil {
					t.Fatal(err)
				}
				write(t, source, agyTokenPath, "fixture")
			}
			err := agyHome(home, source).Prepare(context.Background())
			if (err == nil) != loggedIn {
				t.Fatalf("login validation changed: %v", err)
			}
			for _, name := range []string{".gemini", ".gemini/antigravity-cli"} {
				info, err := os.Lstat(filepath.Join(home, name))
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
					t.Errorf("private %s must be a real 0700 directory: %v, %v", name, info, err)
				}
			}
			if !loggedIn {
				if _, err := os.Lstat(filepath.Join(source, ".gemini")); !os.IsNotExist(err) {
					t.Fatalf("source .gemini was touched: %v", err)
				}
			}
		})
	}
}

func TestAgyVersionPrivateDirectoriesWithoutLogin(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	source, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", source)
	script := "#!/bin/sh\n[ -d \"$HOME/.gemini/antigravity-cli\" ] || exit 83\n[ ! -L \"$HOME/.gemini\" ] && [ ! -L \"$HOME/.gemini/antigravity-cli\" ] || exit 84\nprintf 'agy 1.4.3\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	version, err := AgyVersion(context.Background())
	if err != nil || version != "1.4.3" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	if _, err = os.Lstat(filepath.Join(source, ".gemini")); !os.IsNotExist(err) {
		t.Fatalf("source touched: %v", err)
	}
}

func TestAgyPrivateDirectoriesRepairModesAndRejectLinks(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "permissions", true: "symlink"}[linked], func(t *testing.T) {
			source, home := t.TempDir(), t.TempDir()
			target := filepath.Join(source, ".gemini")
			if err := os.Mkdir(target, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(target, 0755); err != nil {
				t.Fatal(err)
			}
			if linked {
				if err := os.Symlink(target, filepath.Join(home, ".gemini")); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, name := range agyPrivateDirs {
					if err := os.Mkdir(filepath.Join(home, name), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(filepath.Join(home, name), 0755); err != nil {
						t.Fatal(err)
					}
				}
			}
			// No token: setup must still repair private modes, or reject the link.
			if err := agyHome(home, source).Prepare(context.Background()); err == nil {
				t.Fatal("missing login accepted")
			}
			if !linked {
				for _, name := range agyPrivateDirs {
					info, err := os.Lstat(filepath.Join(home, name))
					if err != nil || info.Mode().Perm() != 0700 {
						t.Fatalf("private mode %s: %v %v", name, info, err)
					}
				}
			}
			info, err := os.Lstat(target)
			if err != nil || info.Mode().Perm() != 0755 {
				t.Fatalf("source mode changed: %v %v", info, err)
			}
			if _, err := os.Lstat(filepath.Join(target, "antigravity-cli")); !os.IsNotExist(err) {
				t.Fatalf("source session directory touched: %v", err)
			}
		})
	}
}
