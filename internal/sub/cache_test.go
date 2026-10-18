package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

var cacheNames = map[string]string{"GOCACHE": "go-build", "GOMODCACHE": "go-mod", "npm_config_cache": "npm"}

func childEnv(env []string, key string) string {
	var value string
	for _, entry := range env {
		if v, ok := strings.CutPrefix(entry, key+"="); ok {
			value = v
		}
	}
	return value
}

func TestJobSharedCacheEnvironment(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "agy", "opencode"} {
		for _, explicit := range []bool{false, true} {
			t.Run(name+"/explicit="+map[bool]string{false: "no", true: "yes"}[explicit], func(t *testing.T) {
				t.Setenv("NPM_CONFIG_CACHE", "")
				store, session, backend := workerFixture(t, "write")
				want := map[string]string{}
				for key, dir := range cacheNames {
					t.Setenv(key, "")
					want[key] = filepath.Join(filepath.Dir(store.Root), "cache", dir)
					if explicit {
						want[key] = filepath.Join(t.TempDir(), dir)
						t.Setenv(key, want[key])
					}
				}
				session.script = func(s *fakeSession) {
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
				h := newFakeHarness(backend)
				h.Name = name
				result, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
				if err != nil || result.Status != StatusSucceeded {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				for key, path := range want {
					if got := childEnv(backend.gotOpts.Env, key); got != path {
						t.Errorf("%s=%q, want %q", key, got, path)
					}
					if !explicit {
						if info, err := os.Stat(path); err != nil || !info.IsDir() {
							t.Errorf("shared cache missing: %s: %v", path, err)
						}
					}
				}
			})
		}
	}
}

func TestJobUnwritableExplicitCacheFallsBack(t *testing.T) {
	store, session, backend := workerFixture(t, "write")
	for key := range cacheNames {
		t.Setenv(key, "")
	}
	// A regular file cannot be a cache directory, independently of uid or sandbox.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for key := range cacheNames {
		t.Setenv(key, filepath.Join(blocked, "cache"))
	}
	session.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Actual result"}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	h := newFakeHarness(backend)
	h.Name = "codex"
	result, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || result.Status != StatusSucceeded {
		t.Fatalf("%+v %v", result, err)
	}
	for key, dir := range cacheNames {
		want := filepath.Join(filepath.Dir(store.Root), "cache", dir)
		if childEnv(backend.gotOpts.Env, key) != want || !contains(result.Warnings, key+" is not writable") {
			t.Errorf("%s fallback missing: env=%v warnings=%v", key, backend.gotOpts.Env, result.Warnings)
		}
	}
	st, _ := store.Load("j1")
	if st.Error != "" {
		t.Fatalf("cache hints must not become job errors: %q", st.Error)
	}
}

func TestJobTempLifecycle(t *testing.T) {
	for _, ending := range []string{"success", "failure", "startup", "canceled", "panic"} {
		t.Run(ending, func(t *testing.T) {
			t.Setenv("NPM_CONFIG_CACHE", "")
			store, session, backend := workerFixture(t, "write")
			for key := range cacheNames {
				t.Setenv(key, "")
			}
			originalTmp := t.TempDir()
			t.Setenv("TMPDIR", originalTmp)
			write(t, originalTmp, "user-file", "keep")
			otherTmp := filepath.Join(store.Dir("other"), "tmp")
			if err := os.MkdirAll(otherTmp, 0700); err != nil {
				t.Fatal(err)
			}
			write(t, otherTmp, "keep", "other job")
			wantTmp := filepath.Join(store.Dir("j1"), "tmp")
			session.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := cacheProbeBackend{fakeBackend: backend, start: func(o cockpit.StartOpts) {
				if got := childEnv(o.Env, "TMPDIR"); got != wantTmp {
					t.Errorf("TMPDIR=%q, want %q", got, wantTmp)
				}
				if _, err := os.Stat(wantTmp); err != nil {
					t.Errorf("temp not created: %v", err)
					return
				}
				write(t, wantTmp, "build-cache", "temporary")
				if err := os.Symlink(otherTmp, filepath.Join(wantTmp, "other")); err != nil {
					t.Fatal(err)
				}
				switch ending {
				case "startup":
					backend.startErr = errors.New("startup failed")
				case "failure":
					session.script = func(s *fakeSession) {
						s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed}
					}
				case "canceled":
					cancel()
				case "panic":
					panic("startup panic")
				}
			}}
			h := newFakeHarness(backend)
			h.Backend = func() cockpit.Backend { return &b }
			func() {
				defer func() {
					if p := recover(); p != nil && ending != "panic" {
						panic(p)
					}
				}()
				_, _ = RunJob(ctx, store, "j1", h, WorkerConfig{})
			}()
			if _, err := os.Lstat(wantTmp); !os.IsNotExist(err) {
				t.Errorf("temp survived: %v", err)
			}
			for _, path := range []string{filepath.Join(originalTmp, "user-file"), filepath.Join(otherTmp, "keep")} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("unrelated file removed: %s: %v", path, err)
				}
			}
		})
	}
}

type cacheProbeBackend struct {
	*fakeBackend
	start func(cockpit.StartOpts)
}

func (b *cacheProbeBackend) Start(ctx context.Context, opts cockpit.StartOpts) (cockpit.Session, error) {
	b.start(opts)
	return b.fakeBackend.Start(ctx, opts)
}

func TestDiscardCleansOnlyRelatedJobTemps(t *testing.T) {
	store, st := worktreeFixture(t)
	finishWorktree(t, store, st.ID, StatusSucceeded)
	next, err := store.CreateRun("next", st.Request, false, true)
	if err != nil {
		t.Fatal(err)
	}
	finishWorktree(t, store, next.ID, StatusSucceeded)
	for _, id := range []string{st.ID, next.ID} {
		// Exercise normal startup to create and record the temp's ownership, but
		// retain it as if the worker crashed before teardown.
		if err := store.prepareJobTemp(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(store.Dir(id), "tmp"), "cache", "orphan")
	}
	other := filepath.Join(store.Dir("unrelated"), "tmp")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, other, "keep", "other job")
	if err := store.DiscardWorktree(next.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{st.ID, next.ID} {
		if _, err := os.Lstat(filepath.Join(store.Dir(id), "tmp")); !os.IsNotExist(err) {
			t.Errorf("temp survived discard for %s: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(other, "keep")); err != nil {
		t.Fatal(err)
	}
}

func TestFinishedAndRecoveredJobsCleanOwnedTemp(t *testing.T) {
	for _, ending := range []string{"finish", "recover", "cancel-orphan"} {
		t.Run(ending, func(t *testing.T) {
			store, _, _ := workerFixture(t, "write")
			if err := store.prepareJobTemp(context.Background(), "j1"); err != nil {
				t.Fatal(err)
			}
			tmp := filepath.Join(store.Dir("j1"), "tmp")
			write(t, tmp, "cache", "orphan")
			st, _ := store.Load("j1")
			st.Status = StatusRunning // No verifiable worker; recovery must reap it.
			if err := store.Save(st); err != nil {
				t.Fatal(err)
			}
			var err error
			switch ending {
			case "finish":
				err = store.Finish("j1", &Result{Status: StatusCanceled}, "force stopped", time.Now())
			case "recover":
				_, err = store.Recover("j1")
			case "cancel-orphan":
				_, err = store.RequestCancel("j1")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
				t.Fatalf("%s left temp: %v", ending, err)
			}
		})
	}
}

func TestJobTempRejectsUnknownOwnershipAndSymlinkedJobs(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	tmp := filepath.Join(store.Dir("j1"), "tmp")
	if err := os.Mkdir(tmp, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, tmp, "user-file", "keep")
	if err := store.prepareJobTemp(context.Background(), "j1"); err == nil {
		t.Fatal("adopted unknown temp")
	}
	if err := store.cleanupJobTemp("j1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "user-file")); err != nil {
		t.Fatal("unknown temp removed", err)
	}
	if err := os.Symlink(store.Dir("j1"), store.Dir("alias")); err != nil {
		t.Fatal(err)
	}
	if err := store.cleanupJobTemp("alias"); err == nil {
		t.Fatal("followed symlinked job directory")
	}
}

func TestJobTempCleanupDoesNotFollowReplacedTemp(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	if err := store.prepareJobTemp(context.Background(), "j1"); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(store.Dir("j1"), "tmp")
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	write(t, other, "user-file", "keep")
	if err := os.Symlink(other, tmp); err != nil {
		t.Fatal(err)
	}
	if err := store.cleanupJobTemp("j1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp link survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "user-file")); err != nil {
		t.Fatal("symlink target removed", err)
	}
}

func TestJobCacheOffAndReadOnlyDoNotGrantWrites(t *testing.T) {
	for _, access := range []cockpit.Access{cockpit.AccessWrite, cockpit.AccessReadOnly} {
		t.Run(string(access), func(t *testing.T) {
			t.Setenv("NPM_CONFIG_CACHE", "")
			home := t.TempDir()
			t.Setenv("GOCACHE", "off")
			t.Setenv("GOMODCACHE", "")
			t.Setenv("npm_config_cache", "")
			opts := cockpit.StartOpts{Access: access, RepoDir: t.TempDir()}
			warnings, err := prepareJobCaches(home, true, &opts)
			if err != nil || len(warnings) != 0 || childEnv(opts.Env, "GOCACHE") != "off" {
				t.Fatalf("%+v %v %v", opts, warnings, err)
			}
			for _, root := range opts.WritableRoots {
				if filepath.Base(root) == "off" {
					t.Fatal("off was treated as a directory")
				}
			}
			if access == cockpit.AccessReadOnly && len(opts.WritableRoots) != 0 {
				t.Fatalf("read-only gained writes: %v", opts.WritableRoots)
			}
		})
	}
}

func TestJobNpmUppercaseCachePreference(t *testing.T) {
	for _, lower := range []bool{false, true} {
		t.Run(map[bool]string{false: "uppercase", true: "lowercase-wins"}[lower], func(t *testing.T) {
			t.Setenv("GOCACHE", "")
			t.Setenv("GOMODCACHE", "")
			t.Setenv("npm_config_cache", "")
			upper := t.TempDir()
			t.Setenv("NPM_CONFIG_CACHE", upper)
			want := upper
			if lower {
				want = t.TempDir()
				t.Setenv("npm_config_cache", want)
			}
			opts := cockpit.StartOpts{Access: cockpit.AccessWrite, RepoDir: t.TempDir()}
			warnings, err := prepareJobCaches(t.TempDir(), true, &opts)
			if err != nil || len(warnings) != 0 || childEnv(opts.Env, "npm_config_cache") != want {
				t.Fatalf("uppercase preference lost: %+v %v %v", opts, warnings, err)
			}
		})
	}
}
