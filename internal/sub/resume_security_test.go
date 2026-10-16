package sub

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestResumeLockProcessHelper(t *testing.T) {
	if os.Getenv("AJ704_RESUME_HELPER") != "1" {
		return
	}
	store := Store{Root: os.Getenv("AJ704_RESUME_STORE")}
	parent, err := store.Load("parent")
	if err != nil {
		os.Exit(10)
	}
	if os.Getenv("AJ704_RESUME_API") == "1" {
		_, err = store.ResumeDashboardJob(t.Context(), parent.ID, "continue")
	} else {
		_, err = store.ResumeRun(os.Getenv("AJ704_RESUME_CHILD"), parent.Request, false, parent.ID)
	}
	if err == nil {
		os.Exit(0)
	}
	var conflict *ActionConflict
	if errors.As(err, &conflict) {
		os.Exit(9)
	}
	os.Exit(10)
}
func TestResumeLockSharedAcrossCLIAndAPIProcesses(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	parent, err := store.Create("parent", Request{Harness: "fake", Cwd: t.TempDir(), Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, store, parent.ID, StatusSucceeded)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	exits := make(chan int, 5)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cmd := exec.Command(exe, "-test.run=^TestResumeLockProcessHelper$")
			api := "0"
			if i%2 == 0 {
				api = "1"
			}
			cmd.Env = append(os.Environ(), "AJ704_RESUME_HELPER=1", "AJ704_RESUME_STORE="+store.Root, "AJ704_RESUME_CHILD="+fmt.Sprintf("child-%d", i), "AJ704_RESUME_API="+api)
			err := cmd.Run()
			if err == nil {
				exits <- 0
			} else {
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					exits <- exit.ExitCode()
				} else {
					exits <- 10
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(exits)
	success := 0
	for code := range exits {
		if code == 0 {
			success++
		} else if code != 9 {
			t.Errorf("unexpected process exit: %d", code)
		}
	}
	if success != 1 {
		t.Fatal("expected one created turn", success)
	}
}

func TestResumeSkipsOtherCorruptJobs(t *testing.T) {
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	store := Store{Root: t.TempDir()}
	parent, err := store.Create("parent", Request{Harness: "fake", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, store, parent.ID, StatusSucceeded)
	if err := os.Mkdir(store.Dir("damaged"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir("damaged"), "state.json"), []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResumeRun("child", parent.Request, false, parent.ID); err != nil {
		t.Fatal(err)
	}
}
