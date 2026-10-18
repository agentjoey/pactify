package sub

import (
	"context"
	"github.com/agentjoey/pactify/internal/cockpit"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpencode2LoginHoldsLease(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PACTIFY_HOME", root)
	bin := t.TempDir()
	marker := filepath.Join(root, "login-started")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'opencode v2.0.32'; exit; fi\ntouch '" + marker + "'\ncat\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	in, out := io.Pipe()
	defer in.Close()
	defer out.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- OpencodeLogin(ctx, nil, in, io.Discard, io.Discard) }()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("login did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	home, _ := opencode2Homes().Managed()
	env := []string{"XDG_DATA_HOME=" + home}
	wait, stop := context.WithTimeout(ctx, 80*time.Millisecond)
	release, err := cockpit.Opencode2Lease(wait, env)
	stop()
	if err == nil {
		release()
		t.Fatal("login did not hold lease")
	}
	out.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	release, err = cockpit.Opencode2Lease(ctx, env)
	if err != nil {
		t.Fatal("login leaked lease", err)
	}
	release()
}
