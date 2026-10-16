package procx

import (
	"os"
	"os/exec"
	"reflect"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestJobProcessesFromSnapshot(t *testing.T) {
	entries := []Identity{{10, "match"}, {20, "other"}, {30, "reused"}, {40, "unreadable"}, {50, "exited"}, {60, "prefix"}, {70, "argument"}}
	envs := map[int][]string{
		10: {"PACTIFY_JOB=target"}, 20: {"PACTIFY_JOB=other"},
		30: {"PACTIFY_JOB=target"}, 50: {"PACTIFY_JOB=target"},
		60: {"PACTIFY_JOB=target-extra"}, 70: {"COMMAND=PACTIFY_JOB=target"},
	}
	read := func(pid int) ([]string, error) {
		if pid == 40 {
			return nil, syscall.EPERM
		}
		return envs[pid], nil
	}
	inspect := func(pid int) (Identity, error) {
		if pid == 50 {
			return Identity{}, syscall.ESRCH
		}
		if pid == 30 {
			return Identity{30, "new-birth"}, nil
		}
		return entries[slices.IndexFunc(entries, func(p Identity) bool { return p.PID == pid })], nil
	}
	if got := jobProcessesFromSnapshot("target", entries, read, inspect); !reflect.DeepEqual(got, entries[:1]) {
		t.Fatalf("matches=%+v", got)
	}
	if got := jobProcessesFromSnapshot("", entries, func(int) ([]string, error) {
		t.Fatal("empty job id must not read process environments")
		return nil, nil
	}, inspect); len(got) != 0 {
		t.Fatalf("empty job matched %+v", got)
	}
}

func TestFindJobProcessesAndTerminate(t *testing.T) {
	start := func(job string) Identity {
		// Darwin omits environments for protected system executables such as
		// /bin/sleep. Use our own executable so this tests a readable environment.
		cmd := exec.Command(os.Args[0], "-test.run=^TestJobProcessHelper$")
		cmd.Env = append(os.Environ(), "PACTIFY_JOB="+job, "PACTIFY_TEST_JOB_SLEEP=1", "GORACE=atexit_sleep_ms=0")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		p, err := Inspect(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	id := t.Name() + "-" + time.Now().Format("150405.000000000")
	child, other := start(id), start(id+"-other")
	found, err := FindJobProcesses(id)
	if err != nil || !slices.Contains(found, child) || slices.Contains(found, other) {
		t.Fatalf("found=%+v err=%v", found, err)
	}
	wrong := child
	wrong.Start += "-wrong"
	if err := TerminateDescendants("job", []Identity{wrong}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if alive, err := child.Alive(); err != nil || !alive {
		t.Fatalf("wrong birth identity killed child: %v %v", alive, err)
	}
	if err := TerminateDescendants("job", found, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if alive, err := child.Alive(); err != nil || alive {
		t.Fatalf("matched process survived: %v %v", alive, err)
	}
	if alive, err := other.Alive(); err != nil || !alive {
		t.Fatalf("other job affected: %v %v", alive, err)
	}
}

func TestJobProcessHelper(t *testing.T) {
	if os.Getenv("PACTIFY_TEST_JOB_SLEEP") != "1" {
		return
	}
	time.Sleep(120 * time.Second)
	os.Exit(0)
}

func TestFindJobProcessesEmptyID(t *testing.T) {
	if found, err := FindJobProcesses(""); err != nil || len(found) != 0 {
		t.Fatalf("empty id: %+v %v", found, err)
	}
}
