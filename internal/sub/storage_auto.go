package sub

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// StartStorageMaintenance is best effort and is called only after the worker
// has started. Its detached child has no inherited output pipes or job result.
func (s Store) StartStorageMaintenance(id string) {
	r, err := s.openStorage()
	if err != nil {
		return
	}
	due, e := storageDue(r)
	home := r.Name()
	r.Close()
	if e != nil || !due {
		return
	}
	binary, err := os.Executable()
	if err != nil {
		return
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer null.Close()
	cmd := exec.Command(binary, "sub", "__prune", id)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "PACTIFY_HOME="+home)
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// MaintainStorage checks the persisted interval every minute. The prune lock
// serializes resident servers, CLI children and manual cleanup across processes.
func (s Store) MaintainStorage(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		call, cancel := context.WithTimeout(ctx, 10*time.Minute)
		_, _ = s.PruneStorageDue(call)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s Store) storageIdle() (bool, error) { return s.storageIdleContext(context.Background()) }
func (s Store) storageIdleContext(ctx context.Context) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	states, err := s.storageStatesContext(ctx)
	if err != nil {
		return false, err
	}
	for _, st := range states {
		if e := ctx.Err(); e != nil {
			return false, e
		}
		if !Terminal(st.Status) || storageProcessReason(st) != "" {
			return false, nil
		}
	}
	return true, nil
}

// A child belongs to the job that scheduled it. Settle for one second after
// idle, then verify that job again: removing a store must not make an old
// detached child interpret missing jobs as idle storage and recreate files.
func (s Store) PruneStorageAfterJob(ctx context.Context, id string) (StorageRun, error) {
	if err := validJobID(id); err != nil {
		return StorageRun{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	r, err := s.openStorage()
	if err != nil {
		return StorageRun{}, err
	}
	defer r.Close()
	due, err := storageDue(r)
	if err != nil || !due {
		return StorageRun{}, err
	}
	wait, stop := context.WithTimeout(ctx, time.Millisecond)
	release, err := storageLock(wait, r, "storage-scheduler.lock", syscall.LOCK_EX)
	stop()
	if err != nil {
		if ctx.Err() != nil {
			return StorageRun{}, ctx.Err()
		}
		return StorageRun{}, nil
	}
	defer release()
	for {
		if err := s.checkStorageTrigger(id); err != nil {
			return StorageRun{}, err
		}
		idle, err := s.storageIdleContext(ctx)
		if err != nil {
			return StorageRun{}, err
		}
		if idle {
			select {
			case <-ctx.Done():
				return StorageRun{}, ctx.Err()
			case <-time.After(time.Second):
			}
			if err = s.checkStorageTrigger(id); err != nil {
				return StorageRun{}, err
			}
			if idle, err = s.storageIdleContext(ctx); err != nil {
				return StorageRun{}, err
			} else if idle {
				return s.pruneStorage(ctx, PruneOptions{}, storageDeps{due: true, trigger: id})
			}
		}
		select {
		case <-ctx.Done():
			return StorageRun{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func storageDue(r *os.Root) (bool, error) {
	policy, _, e := storagePolicy(r, PruneOptions{})
	if e != nil {
		return false, e
	}
	var last StorageRun
	if e = storageReadJSON(r, "storage-status.json", &last); e != nil && !os.IsNotExist(e) {
		return false, e
	}
	interval, _ := time.ParseDuration(policy.Interval)
	return last.StartedAt.IsZero() || time.Since(last.StartedAt) >= interval, nil
}
