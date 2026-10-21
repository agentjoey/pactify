package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParallelFileContent(t *testing.T) {
	for _, tc := range []struct {
		name, content  string
		missing, allow bool
	}{
		{name: "without newline", content: "job-1", allow: true},
		{name: "newline", content: "job-1\n", allow: true},
		{name: "trailing whitespace", content: "job-1 \t\r\n", allow: true},
		{name: "wrong job", content: "job-0\n"},
		{name: "leading whitespace", content: " job-1\n"},
		{name: "extra content", content: "job-1\nother"},
		{name: "empty"},
		{name: "missing", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if !tc.missing {
				if err := os.WriteFile(filepath.Join(root, "same.txt"), []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := parallelFileContent(root, 1); (err == nil) != tc.allow {
				t.Fatalf("error=%v, want allow=%t", err, tc.allow)
			}
		})
	}
}

func TestSameFileContent(t *testing.T) {
	for _, tc := range []struct {
		name, src, dst string
		missing, allow bool
	}{
		{name: "identical", src: "alpha", dst: "alpha", allow: true},
		{name: "identical with newline", src: "alpha\n", dst: "alpha\n", allow: true},
		{name: "added newline", src: "alpha", dst: "alpha\n"},
		{name: "dropped newline", src: "alpha\n", dst: "alpha"},
		{name: "different", src: "alpha\n", dst: "beta\n"},
		{name: "missing copy", src: "alpha\n", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte(tc.src), 0600); err != nil {
				t.Fatal(err)
			}
			if !tc.missing {
				if err := os.WriteFile(filepath.Join(dst, "f.txt"), []byte(tc.dst), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := sameFileContent(src, dst, "f.txt"); (err == nil) != tc.allow {
				t.Fatalf("error=%v, want allow=%t", err, tc.allow)
			}
		})
	}
}
