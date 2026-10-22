package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func addSubWorktreeCommands(command, run *cobra.Command) {
	run.Flags().String("worktree-from", "", "Start a fresh session in a finished job's worktree")
	run.Flags().Bool("worktree", false, "Run in a detached worktree based on HEAD (resume reuses it)")
	for _, name := range []string{"apply", "discard"} {
		command.AddCommand(&cobra.Command{Use: name + " <job>", Short: name + " a job's isolated worktree", Args: subArgs(1), Run: func(c *cobra.Command, args []string) {
			var notes []string
			store, err := sub.DefaultStore()
			if err == nil {
				if c.Name() == "apply" {
					err = store.ApplyWorktree(args[0])
				} else {
					notes, err = store.DiscardWorktreeNotes(args[0])
				}
			}
			for _, note := range notes {
				fmt.Fprintln(c.ErrOrStderr(), "Note: "+note)
			}
			if err != nil {
				subExit(c, 1, err)
				return
			}
			if c.Name() == "apply" {
				fmt.Fprintf(c.OutOrStdout(), "Applied to the original workspace (uncommitted); run pactify sub discard %s to clean up\n", args[0])
			} else {
				fmt.Fprintf(c.OutOrStdout(), "%s worktree discarded\n", args[0])
			}
		}})
	}
	command.AddCommand(&cobra.Command{Use: "dismiss <job>", Short: "Acknowledge a finished job's failure and warnings", Args: subArgs(1), Run: func(c *cobra.Command, args []string) {
		store, err := sub.DefaultStore()
		if err == nil {
			err = store.Dismiss(c.Context(), args[0])
		}
		if err != nil {
			code, err := subJobError(store, args[0], err)
			subExit(c, code, err)
			return
		}
		fmt.Fprintf(c.OutOrStdout(), "dismissed %s\n", args[0])
	}})
}

func subCreateRun(c *cobra.Command, store sub.Store, id string, req sub.Request, resume bool, resumeJob string) (sub.State, int, error) {
	worktree, _ := c.Flags().GetBool("worktree")
	from, _ := c.Flags().GetString("worktree-from")
	if from != "" {
		if err := checkHarnessInstalled(req.Harness); err != nil {
			return sub.State{}, 2, err
		}
		st, err := store.WorktreeFrom(id, req, from)
		return st, 2, err
	}
	selector := resumeJob
	if resume {
		var previous sub.State
		var err error
		if resumeJob == subResumeLatest {
			previous, err = store.LatestResumeJob(req.Harness, req.Cwd)
		} else {
			previous, err = store.ResolveResumeJob(resumeJob)
		}
		if err != nil {
			return sub.State{}, 2, err
		}
		if !c.Flags().Changed("model") {
			req.Model = previous.Request.Model
		}
		if !c.Flags().Changed("effort") {
			req.Effort = previous.Request.Effort
		}
		selector = previous.ID
		// Report why a session cannot be resumed before blaming a missing CLI.
		if err := store.CheckResume(req, selector); err != nil {
			if resumeJob != subResumeLatest {
				return sub.State{}, 2, err
			}
			return sub.State{}, createRunErrorCode(err), err
		}
	}
	// Check the starting harness before any job record or worktree exists.
	if err := checkHarnessInstalled(req.Harness); err != nil {
		return sub.State{}, 2, err
	}
	if resume && resumeJob != subResumeLatest {
		st, err := store.ResumeRun(id, req, worktree, selector)
		return st, 2, err
	}
	var st sub.State
	var err error
	if resume {
		st, err = store.ResumeRun(id, req, worktree, selector)
	} else {
		st, err = store.CreateRun(id, req, worktree, false)
	}
	if err == nil && resume {
		candidates, listErr := store.ResumeCandidates(req.Harness, req.Cwd)
		if listErr != nil {
			return st, 1, listErr
		}
		if len(candidates) > 1 {
			fmt.Fprintln(c.ErrOrStderr(), "Multiple resumable jobs:")
			states, err := store.States()
			if err != nil {
				return st, 1, err
			}
			index := store.NewSummaryIndex(states)
			for _, candidate := range candidates {
				summary := index.Summarize(candidate, time.Now())
				fmt.Fprintf(c.ErrOrStderr(), "  %s\t%s\t%s\n", candidate.ID, summary.Title, candidate.FinishedAt.Format(time.RFC3339))
			}
			fmt.Fprintf(c.ErrOrStderr(), "Selected %s; use --resume <job-id> to select a different job.\n", st.ResumeOf)
		}
	}
	return st, createRunErrorCode(err), err
}

func createRunErrorCode(err error) int {
	var noSession *sub.NoSessionError
	var noReadOnly *sub.ReadOnlyUnavailableError
	if errors.As(err, &noSession) || errors.As(err, &noReadOnly) || errors.Is(err, sub.ErrWorktreeNeedsCommit) {
		return 2
	}
	return 1
}

func checkHarnessInstalled(name string) error {
	h, _ := sub.Lookup(name)
	if err := h.CheckInstalled(); err != nil {
		return fmt.Errorf("harness CLI %q is not installed on PATH; install %s and ensure it is on PATH, then run pactify sub doctor %s: %w", name, name, name, err)
	}
	return nil
}
