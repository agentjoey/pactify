package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
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
	var readOnly, write, fresh, jsonOut bool
	var resume, runHarness string
	var wait time.Duration
	run := &cobra.Command{Use: "run [harness]", Short: "Read a task from stdin and start a background worker (default: " + sub.DefaultHarness + ")", Args: func(c *cobra.Command, args []string) error {
		// pflag leaves a space-separated optional value in the positional args.
		// Keep recognized harness names positional, including `--resume codex`.
		if resume == subResumeLatest && len(args) > 0 {
			if _, harness := sub.Lookup(args[0]); !harness && !strings.HasPrefix(args[0], "@") {
				resume, args = args[0], args[1:]
			} else if len(args) > 1 {
				resume, args = args[1], append(args[:1:1], args[2:]...)
			}
		}
		if err := cobra.MaximumNArgs(1)(c, args); err != nil {
			subExit(c, 2, err)
		}
		runHarness = ""
		if len(args) > 0 {
			runHarness = args[0]
		}
		return nil
	}, Run: func(c *cobra.Command, args []string) {
		code, err := subRun(c, runHarness, model, effort, cwd, readOnly, write, c.Flags().Changed("resume"), fresh, wait, jsonOut, resume)
		subExit(c, code, err)
	}}
	run.Flags().Int("completion-followups", 2, "Agy completion followups (0 disables, maximum 5)")
	run.Flags().Int("retries", sub.DefaultRetries, "Transient continuations (0 disables, maximum 5)")
	run.Flags().StringVar(&model, "model", "", "Requested model (must be available)")
	run.Flags().StringVar(&effort, "effort", "", "Reasoning effort")
	run.Flags().StringVar(&cwd, "cwd", ".", "Working directory")
	run.Flags().BoolVar(&readOnly, "read-only", false, "Deny all permission requests")
	run.Flags().BoolVar(&write, "write", false, "Allow workspace writes (default)")
	run.Flags().StringVar(&resume, "resume", "", "Resume a job by id/unique prefix; omit the value for the latest session")
	run.Flags().Lookup("resume").NoOptDefVal = subResumeLatest
	run.Flags().BoolVar(&fresh, "fresh", false, "Start a fresh session (default)")
	run.Flags().DurationVar(&wait, "wait", 540*time.Second, "How long to wait before returning the job id")
	run.Flags().BoolVar(&jsonOut, "json", false, "Print JSON")
	run.Flags().Bool("with-user-config", false, "Load all user integrations instead of the minimal configuration")
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
	status.Flags().Bool("all", false, "Show all jobs instead of the 10 most recent")
	status.Flags().String("repo", "", "Filter jobs by their original repository root")
	var resultJSON bool
	result := &cobra.Command{Use: "result <job>", Short: "Read a finished job's result", Args: subArgs(1), Run: func(c *cobra.Command, a []string) {
		store, err := sub.DefaultStore()
		if err != nil {
			subExit(c, 1, err)
			return
		}
		st, err := store.FinalJob(a[0])
		if err != nil {
			code, err := subJobError(store, a[0], err)
			subExit(c, code, err)
			return
		}
		code := 3
		if subTerminal(st.Status) {
			code, err = subOutput(c, store, st, resultJSON)
		} else {
			err = subPending(c, a[0], resultJSON, st.EffectiveConfigMode())
		}
		subExit(c, code, err)
	}}
	result.Flags().BoolVar(&resultJSON, "json", false, "Print JSON")
	cancel := &cobra.Command{Use: "cancel <job>", Short: "Cancel a worker and its agent", Args: subArgs(1), Run: func(c *cobra.Command, a []string) { code, err := subCancel(c, a[0]); subExit(c, code, err) }}
	worker := &cobra.Command{Use: "__worker <job>", Hidden: true, Args: subArgs(1), Run: func(c *cobra.Command, a []string) { code, err := subWorker(c.Context(), a[0]); subExit(c, code, err) }}
	hook := &cobra.Command{Use: "agy-hook", Hidden: true, Args: subArgs(0), Run: func(c *cobra.Command, _ []string) {
		cockpit.RunAgyHook(c.Context(), c.InOrStdin(), c.OutOrStdout(), os.Getenv("PACTIFY_AGY_READ_ONLY"))
	}}
	command.AddCommand(hook, run, waitCmd, status, result, cancel, worker, newSubDoctorCmd(), newSubLogsCmd(), newSubPanelCmd(), newSubCostCmd(), newSubQuotaCmd(), newSubRouterCmd())
	addSubWorktreeCommands(command, run)
	return command
}

// The slash keeps this implicit-value marker outside the valid job-id space.
const subResumeLatest = "<latest/session>"

func subRun(c *cobra.Command, name, model, effort, cwd string, readOnly, write, resume, fresh bool, wait time.Duration, jsonOut bool, resumeJob string) (int, error) {
	retries := sub.DefaultRetries
	if c.Flags().Lookup("retries") != nil {
		retries, _ = c.Flags().GetInt("retries")
	}
	if retries < 0 || retries > sub.MaxRetries {
		return 2, fmt.Errorf("--retries must be between 0 and %d", sub.MaxRetries)
	}
	from, _ := c.Flags().GetString("worktree-from")
	if from != "" && resume {
		return 2, fmt.Errorf("--worktree-from and --resume cannot be used together")
	}
	if resume && fresh {
		return 2, fmt.Errorf("--resume and --fresh cannot be used together")
	}
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	if resume && resumeJob != subResumeLatest {
		previous, err := store.ResolveResumeJob(resumeJob)
		if err != nil {
			return 2, err
		}
		if name == "" {
			name = previous.Request.Harness
		}
	}
	if name == "" {
		name = sub.DefaultHarness
	}
	router, err := store.LoadRouter()
	if err != nil {
		return subRouterError(err)
	}
	route, err := router.Resolve(name)
	if err != nil {
		return 2, err
	}
	if model != "" || c.Flags().Changed("model") {
		route.Model = model
	}
	if effort != "" || c.Flags().Changed("effort") {
		route.Effort = effort
	}
	name, model, effort = route.Harness, route.Model, route.Effort
	h, ok := sub.Lookup(name)
	if !ok {
		return 2, fmt.Errorf("unknown harness %q; available: %s", name, strings.Join(sub.Names(), ", "))
	}

	if effort != "" && len(h.Efforts()) > 0 && !slices.Contains(h.Efforts(), effort) {
		return 2, fmt.Errorf("unsupported effort %q; available: %s", effort, strings.Join(h.Efforts(), ", "))
	}
	if readOnly && write {
		return 2, fmt.Errorf("--read-only and --write cannot be used together")
	}
	if readOnly && h.NoReadOnly != "" {
		return 2, fmt.Errorf("%s", h.NoReadOnly)
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
		return 2, fmt.Errorf("task brief must not be empty; provide it on stdin")
	}
	req := sub.Request{Harness: name, Model: model, Effort: effort, Cwd: dir, Brief: string(brief), Access: "write"}
	req.Retries = &retries
	if c.Flags().Lookup("completion-followups") != nil {
		n, _ := c.Flags().GetInt("completion-followups")
		if n < 0 || n > 5 {
			return 2, fmt.Errorf("--completion-followups must be between 0 and 5")
		}
		req.CompletionFollowups = &n
	}
	req.WithUserConfig, _ = c.Flags().GetBool("with-user-config")
	if readOnly {
		req.Access = "read-only"
	}
	var routing *sub.Routing
	var routeWarnings []string
	if router != nil && !resume {
		quotas, e := store.ExhaustedRoutes(time.Now())
		if e != nil {
			return 1, e
		}
		selected, remaining, warnings, e := router.Select(router.Chain(route), quotas, req.Access, time.Now())
		if e != nil {
			return 2, e
		}
		req.Harness, req.Model, req.Effort = selected.Harness, selected.Model, selected.Effort
		routing = &sub.Routing{Remaining: remaining, QuotaGroups: router.QuotaGroups}
		routeWarnings = warnings
	}
	id := sub.NewJobID(time.Now(), rand.Reader)
	st, code, err := subCreateRun(c, store, id, req, resume, resumeJob)
	if err != nil {
		return code, err
	}
	if routing != nil {
		st.Routing = routing
		st.RouteWarnings = routeWarnings
		if err = store.Save(st); err != nil {
			return 1, err
		}
		for _, warning := range routeWarnings {
			fmt.Fprintln(c.ErrOrStderr(), warning)
		}
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
func subStartWorker(store sub.Store, id string) error { return store.StartWorker(id) }
func subPending(c *cobra.Command, id string, jsonOut bool, configMode string) error {
	if jsonOut {
		return subJSON(c, struct {
			JobID      string `json:"jobId"`
			Status     string `json:"status"`
			Hint       string `json:"hint"`
			ConfigMode string `json:"configMode"`
		}{id, "running", "pactify sub wait " + id, configMode})
	}
	_, err := fmt.Fprintf(c.OutOrStdout(), "%s is still running; use pactify sub wait %s to view the result\n", id, id)
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
		st, err := store.FinalJob(id)
		if err != nil {
			return subJobError(store, id, err)
		}
		if subTerminal(st.Status) {
			return subOutput(c, store, st, jsonOut)
		}
		select {
		case <-c.Context().Done():
			return 1, c.Context().Err()
		case <-deadline:
			return 3, subPending(c, id, jsonOut, st.EffectiveConfigMode())
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
func subCancel(c *cobra.Command, id string) (int, error) {
	store, err := sub.DefaultStore()
	if err != nil {
		return 1, err
	}
	st, err := store.CancelJob(c.Context(), id)
	if err != nil {
		return subJobError(store, id, err)
	}
	_, err = fmt.Fprintf(c.OutOrStdout(), "%s %s\n", st.ID, st.Status)
	return 0, err
}
func subSaveFallback(store sub.Store, st sub.State) error { return store.SaveFallback(st) }
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
	ctx, stop := signal.NotifyContext(parent, syscall.SIGTERM, os.Interrupt)
	defer stop()
	for {
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
		result, err := sub.RunJob(ctx, store, id, h, sub.WorkerConfig{ToolCallLimit: 40})
		if result.FallbackTo != "" {
			id = result.FallbackTo
			continue
		}
		if err != nil {
			return 1, err
		}
		if result.Status != sub.StatusSucceeded {
			return 1, nil
		}
		return 0, nil
	}
}
