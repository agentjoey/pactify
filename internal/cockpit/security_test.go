package cockpit

import (
	"github.com/agentjoey/pactify/internal/procx"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSecurityKillGroupRequiresRecordedIdentity(t *testing.T) {
	cmd := exec.Command("sleep", "120")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() { cmd.Process.Kill(); <-done }()
	// A cmd alone provides no recorded birth identity and must confer no authority.
	killGroup(procx.Identity{PID: cmd.Process.Pid})
	select {
	case <-done:
		t.Fatal("signaled a process without its recorded identity")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSecurityKillGroupRejectsWrongBirth(t *testing.T) {
	cmd := exec.Command("sleep", "120")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	identity, err := procx.Inspect(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	identity.Start += "-wrong"
	if err := killGroup(identity); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("wrong identity was signaled", err)
	}
}
