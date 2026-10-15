package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

// main's generic error path exits 1. Subcommands own the protocol's 0/1/2/3 exit codes.
func subExit(c *cobra.Command, code int, err error) {
	if err != nil {
		fmt.Fprintln(c.ErrOrStderr(), err)
	}
	if code != 0 {
		os.Exit(code)
	}
}
func subArgs(n int) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(c, args); err != nil {
			subExit(c, 2, err)
		}
		return nil
	}
}
func subTerminal(s sub.Status) bool {
	return sub.Terminal(s)
}
func newSubCmd() *cobra.Command {
	command := &cobra.Command{Use: "sub", Short: "Run a local coding harness as a background job", Args: subArgs(0), RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	command.SetFlagErrorFunc(func(c *cobra.Command, err error) error { subExit(c, 2, err); return nil })
	var model, effort, cwd string
	var readOnly, write, resume, fresh, jsonOut bool
	var wait time.Duration
	run := &cobra.Command{Use: "run [harness]", Short: "Read a task from stdin and start a background worker (default: " + sub.DefaultHarness + ")", Args: func(c *cobra.Command, args []string) error {
		if err := cobra.MaximumNArgs(1)(c, args); err != nil {
			subExit(c, 2, err)
		}
		return nil
	}, Run: func(c *cobra.Command, args []string) {
		name := sub.DefaultHarness
		if len(args) > 0 {
			name = args[0]
		}
		code, err := subRun(c, name, model, effort, cwd, readOnly, write, resume, fresh, wait, jsonOut)
		subExit(c, code, err)
	}}
	run.Flags().StringVar(&model, "model", "", "Requested model (must be available)")
	run.Flags().StringVar(&effort, "effort", "", "Reasoning effort")
	run.Flags().StringVar(&cwd, "cwd", ".", "Working directory")
	run.Flags().BoolVar(&readOnly, "read-only", false, "Deny all permission requests")
	run.Flags().BoolVar(&write, "write", false, "Allow workspace writes (default)")
	run.Flags().BoolVar(&resume, "resume", false, "Resume the last successful session in this repo")
	run.Flags().BoolVar(&fresh, "fresh", false, "Start a fresh session (default)")
	run.Flags().DurationVar(&wait, "wait", 540*time.Second, "How long to wait before returning the job id")
	run.Flags().BoolVar(&jsonOut, "json", false, "Print JSON")
	var timeout time.Duration
	var waitJSON bool
	waitCmd := &cobra.Command{Use: "wait <job>", Short: "Wait for a job result", Args: subArgs(1), Run: func(c *cobra.Command, args []string) {
		if timeout < 0 {
			subExit(c, 2, fmt.Errorf("--timeout must not be negative"))
			return
		}
		store, err := sub.DefaultStore()
		if err != nil {
			subExit(c, 1, err)
			return
		}
		code, err := subWait(c, store, args[0], timeout, waitJSON, false)
		subExit(c, code, err)
	}}
	waitCmd.Flags().DurationVar(&timeout, "timeout", 0, "Wait timeout (0 waits indefinitely)")
	waitCmd.Flags().BoolVar(&waitJSON, "json", false, "Print JSON")
	var statusJSON bool
	status := &cobra.Command{Use: "status [<job>]", Short: "Show a job or the 10 most recent jobs", Args: func(c *cobra.Command, a []string) error {
		if len(a) > 1 {
			subExit(c, 2, fmt.Errorf("expected at most one job id"))
		}
		return nil
	}, Run: func(c *cobra.Command, a []string) { code, err := subStatus(c, a, statusJSON); subExit(c, code, err) }}
	status.Flags().BoolVar(&statusJSON, "json", false, "Print JSON")
	var resultJSON bool
	result := &cobra.Command{Use: "result <job>", Short: "Read a finished job's result", Args: subArgs(1), Run: func(c *cobra.Command, a []string) {
		store, err := sub.DefaultStore()
		if err != nil {
			subExit(c, 1, err)
			return
		}
		st, err := store.Recover(a[0])
		if err != nil {
			subExit(c, 1, err)
			return
		}
		code := 3
		if subTerminal(st.Status) {
			code, err = subOutput(c, store, st, resultJSON)
		} else {
			err = subPending(c, a[0], resultJSON)
		}
		subExit(c, code, err)
	}}
	result.Flags().BoolVar(&resultJSON, "json", false, "Print JSON")
	cancel := &cobra.Command{Use: "cancel <job>", Short: "Cancel a worker and its agent", Args: subArgs(1), Run: func(c *cobra.Command, a []string) { code, err := subCancel(c, a[0]); subExit(c, code, err) }}
	worker := &cobra.Command{Use: "__worker <job>", Hidden: true, Args: subArgs(1), Run: func(c *cobra.Command, a []string) { code, err := subWorker(c.Context(), a[0]); subExit(c, code, err) }}
	command.AddCommand(run, waitCmd, status, result, cancel, worker, newSubDoctorCmd())
	return command
}
func subRun(c *cobra.Command, name, model, effort, cwd string, readOnly, write, resume, fresh bool, wait time.Duration, jsonOut bool) (int, error) {
	h, ok := sub.Lookup(name)
	if !ok {
		return 2, fmt.Errorf("未知 harness %q；可用：%s", name, strings.Join(sub.Names(), ", "))
	}
	if readOnly && write {
		return 2, fmt.Errorf("--read-only 与 --write 不能同时使用")
	}
	if readOnly && h.NoReadOnly != "" {
		return 2, fmt.Errorf("%s", h.NoReadOnly)
	}
	if resume && fresh {
		return 2, fmt.Errorf("--resume 与 --fresh 不能同时使用")
	}
	if wait < 0 {
		return 2, fmt.Errorf("--wait must not be negative")
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return 2, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return 2, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return 2, err
	}
	if !info.IsDir() {
		return 2, fmt.Errorf("--cwd is not a directory")
	}
	brief, err := io.ReadAll(c.InOrStdin())
	if err != nil {
		return 2, err
	}
	if strings.TrimSpace(string(brief)) == "" {
		return 2, fmt.Errorf("任务说明不能为空，请从 stdin 输入")
	}
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	req := sub.Request{Harness: name, Model: model, Effort: effort, Cwd: dir, Brief: string(brief), Access: "write"}
	if readOnly {
		req.Access = "read-only"
	}
	if resume {
		req.Resume, err = store.LastSessionFor(name, dir)
		if err != nil {
			return 1, err
		}
		if req.Resume == "" {
			return 2, fmt.Errorf("该仓库还没有可续接的 %s 会话", name)
		}
	}
	id := sub.NewJobID(time.Now(), rand.Reader)
	st, err := store.Create(id, req)
	if err != nil {
		return 1, err
	}
	if err = subStartWorker(store, id); err != nil {
		st.Status = sub.StatusFailed
		st.Error = err.Error()
		st.UpdatedAt = time.Now().UTC()
		st.FinishedAt = st.UpdatedAt
		if e := subSaveFallback(store, st); e != nil {
			return 1, e
		}
		return 1, err
	}
	return subWait(c, store, id, wait, jsonOut, true)
}
func subStartWorker(store sub.Store, id string) error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	in, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(store.Dir(id), "worker.out"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	cmd := exec.Command(binary, "sub", "__worker", id)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return store.WithState(context.Background(), id, func(st *sub.State) error {
		if subTerminal(st.Status) || st.CancelRequested {
			return nil
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		identity, err := procx.Inspect(cmd.Process.Pid)
		if err == nil {
			st.PID = identity.PID
			st.Worker = &identity
			st.UpdatedAt = time.Now().UTC()
			err = store.Save(*st)
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		return cmd.Process.Release()
	})
}
func subPending(c *cobra.Command, id string, jsonOut bool) error {
	if jsonOut {
		return subJSON(c, struct {
			JobID  string `json:"jobId"`
			Status string `json:"status"`
			Hint   string `json:"hint"`
		}{id, "running", "pactify sub wait " + id})
	}
	_, err := fmt.Fprintf(c.OutOrStdout(), "%s 仍在运行；使用 pactify sub wait %s 查看结果\n", id, id)
	return err
}
func subJSON(c *cobra.Command, value any) error {
	enc := json.NewEncoder(c.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func subWait(c *cobra.Command, store sub.Store, id string, timeout time.Duration, jsonOut, immediateZero bool) (int, error) {
	var deadline <-chan time.Time
	var timer *time.Timer
	if timeout > 0 || immediateZero {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		st, err := store.Recover(id)
		if err != nil {
			return 1, err
		}
		if subTerminal(st.Status) {
			return subOutput(c, store, st, jsonOut)
		}
		select {
		case <-c.Context().Done():
			return 1, c.Context().Err()
		case <-deadline:
			return 3, subPending(c, id, jsonOut)
		case <-ticker.C:
		}
	}
}
func subOutput(c *cobra.Command, store sub.Store, st sub.State, jsonOut bool) (int, error) {
	data, err := os.ReadFile(store.ResultPath(st.ID))
	if err != nil {
		return 1, err
	}
	var result sub.Result
	if err = json.Unmarshal(data, &result); err != nil {
		return 1, err
	}
	if jsonOut {
		err = subJSON(c, result)
	} else {
		_, err = fmt.Fprint(c.OutOrStdout(), result.Markdown())
	}
	if err != nil {
		return 1, err
	}
	if result.Status == sub.StatusSucceeded {
		return 0, nil
	}
	// Backend model errors retain their offered-model list in both state and result.
	if strings.Contains(st.Error, "requested model") || strings.Contains(st.Error, "requested effort") || strings.Contains(st.Error, "agent exposes no model option over ACP") {
		return 2, nil
	}
	return 1, nil
}
func subStatus(c *cobra.Command, ids []string, jsonOut bool) (int, error) {
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	states := []sub.State{}
	if len(ids) > 0 {
		st, e := store.Recover(ids[0])
		if e != nil {
			return 1, e
		}
		states = append(states, st)
	} else {
		entries, e := os.ReadDir(store.Root)
		if e != nil && !os.IsNotExist(e) {
			return 1, e
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			st, e := store.Recover(entry.Name())
			if e == nil {
				states = append(states, st)
			}
		}
		sort.Slice(states, func(i, j int) bool { return states[i].CreatedAt.After(states[j].CreatedAt) })
		if len(states) > 10 {
			states = states[:10]
		}
	}
	type statusRow struct {
		sub.State
		Duration string `json:"duration"`
	}
	rows := make([]statusRow, 0, len(states))
	for _, st := range states {
		end := st.FinishedAt
		if end.IsZero() {
			end = time.Now()
		}
		rows = append(rows, statusRow{st, end.Sub(st.CreatedAt).Round(time.Second).String()})
	}
	if jsonOut {
		if len(ids) > 0 {
			return 0, subJSON(c, rows[0])
		}
		return 0, subJSON(c, rows)
	}
	for _, row := range rows {
		if _, err = fmt.Fprintf(c.OutOrStdout(), "%s  %s  %s  %s\n", row.ID, row.Status, row.Request.Harness, row.Duration); err != nil {
			return 1, err
		}
	}
	return 0, nil
}
func subCancel(c *cobra.Command, id string) (int, error) {
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	st, err := store.RequestCancel(id)
	if err != nil {
		return 1, err
	}
	if st.Worker != nil && !subTerminal(st.Status) {
		if err = st.Worker.Signal(syscall.SIGTERM, true); err != nil {
			return 1, err
		}
	}
	deadline := time.Now().Add(24 * time.Second)
	for {
		// Even a terminal worker can leave a wedged harness behind.
		if subTerminal(st.Status) {
			if err = st.KillHarnesses(); err != nil {
				return 1, err
			}
			_, err = fmt.Fprintf(c.OutOrStdout(), "%s %s\n", id, st.Status)
			return 0, err
		}
		alive := false
		if st.Worker != nil {
			alive, err = st.Worker.Alive()
			if err != nil {
				return 1, err
			}
		}
		if !alive || time.Now().After(deadline) {
			cleanupErr := st.KillHarnesses()
			if st.Worker != nil {
				cleanupErr = errors.Join(cleanupErr, st.Worker.Signal(syscall.SIGKILL, true))
			}
			if cleanupErr != nil {
				return 1, cleanupErr
			}
			if st.CancelRequested {
				st.Status = sub.StatusCanceled
				st.Error = "任务已取消（worker 已退出）"
				if alive {
					st.Error = "任务已取消（强制终止）"
				}
			} else if !alive {
				st.Status = sub.StatusFailed
				st.Error = "job worker is absent or its process identity no longer matches"
			} else {
				st.Status = sub.StatusCanceled
				st.Error = "任务已取消（强制终止）"
			}
			if err = subSaveFallback(store, st); err != nil {
				return 1, err
			}
			return 0, nil
		}
		select {
		case <-c.Context().Done():
			return 1, c.Context().Err()
		case <-time.After(100 * time.Millisecond):
		}
		st, err = store.Load(id)
		if err != nil {
			return 1, err
		}
	}
}
func subSaveFallback(store sub.Store, st sub.State) error {
	result := sub.Result{JobID: st.ID, Status: st.Status, Harness: st.Request.Harness, Access: st.Request.Access, ModelRequested: st.Request.Model, LogPath: store.LogPath(st.ID), Warnings: []string{st.Error}}
	if h, ok := sub.Lookup(st.Request.Harness); ok && h.Experimental != "" {
		result.Warnings = append([]string{h.Experimental}, result.Warnings...)
	}
	return store.Finish(st.ID, &result, st.Error, time.Now())
}
func subWorker(parent context.Context, id string) (code int, err error) {
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("worker panic: %v", p)
			code = 1
			if log, e := os.OpenFile(store.LogPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				fmt.Fprintf(log, "%v\n%s", err, debug.Stack())
				log.Close()
			}
			if st, e := store.Load(id); e == nil {
				st.Status = sub.StatusFailed
				st.Error = err.Error()
				st.UpdatedAt = time.Now().UTC()
				st.FinishedAt = st.UpdatedAt
				if e = subSaveFallback(store, st); e != nil {
					err = fmt.Errorf("%v; save failed: %w", err, e)
				}
			}
		}
	}()
	st, err := store.Load(id)
	if err != nil {
		return 1, err
	}
	h, ok := sub.Lookup(st.Request.Harness)
	if !ok {
		st.Status = sub.StatusFailed
		st.Error = "unknown harness " + st.Request.Harness
		st.UpdatedAt = time.Now().UTC()
		st.FinishedAt = st.UpdatedAt
		return 2, subSaveFallback(store, st)
	}
	ctx, stop := signal.NotifyContext(parent, syscall.SIGTERM, os.Interrupt)
	defer stop()
	result, err := sub.RunJob(ctx, store, id, h, sub.WorkerConfig{ToolCallLimit: 40})
	if err != nil {
		return 1, err
	}
	if result.Status != sub.StatusSucceeded {
		return 1, nil
	}
	return 0, nil
}
