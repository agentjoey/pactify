package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func addSubWorktreeCommands(command, run *cobra.Command) {
	run.Flags().Bool("worktree", false, "Run in a detached worktree based on HEAD (resume reuses it)")
	for _, name := range []string{"apply", "discard"} {
		command.AddCommand(&cobra.Command{Use: name + " <job>", Short: name + " a job's isolated worktree", Args: subArgs(1), Run: func(c *cobra.Command, args []string) {
			store, err := sub.DefaultStore()
			if err == nil {
				if c.Name() == "apply" {
					err = store.ApplyWorktree(args[0])
				} else {
					err = store.DiscardWorktree(args[0])
				}
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
}

func subCreateRun(c *cobra.Command, store sub.Store, id string, req sub.Request, resume bool, resumeJob string) (sub.State, int, error) {
	worktree, _ := c.Flags().GetBool("worktree")
	if resume && resumeJob != subResumeLatest {
		st, err := store.ResumeRun(id, req, worktree, resumeJob)
		return st, 2, err
	}
	st, err := store.CreateRun(id, req, worktree, resume)
	if err == nil && resume {
		candidates, listErr := store.ResumeCandidates(req.Harness, req.Cwd)
		if listErr != nil {
			return st, 1, listErr
		}
		if len(candidates) > 1 {
			fmt.Fprintln(c.ErrOrStderr(), "Multiple resumable jobs:")
			for _, candidate := range candidates {
				summary := store.Summarize(candidate, nil, time.Now())
				fmt.Fprintf(c.ErrOrStderr(), "  %s\t%s\t%s\n", candidate.ID, summary.Title, candidate.FinishedAt.Format(time.RFC3339))
			}
			fmt.Fprintf(c.ErrOrStderr(), "Selected %s; use --resume <job-id> to select a different job.\n", st.ResumeOf)
		}
	}
	code := 1
	var noSession *sub.NoSessionError
	var noReadOnly *sub.ReadOnlyUnavailableError
	if errors.As(err, &noSession) || errors.As(err, &noReadOnly) {
		code = 2
	}
	return st, code, err
}
