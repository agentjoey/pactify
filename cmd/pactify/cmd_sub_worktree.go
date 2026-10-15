package main

import (
	"errors"
	"fmt"

	"github.com/agentjoey/pactify/internal/sub"
	"github.com/spf13/cobra"
)

func addSubWorktreeCommands(command, run *cobra.Command) {
	run.Flags().Bool("worktree", false, "Run in a detached worktree based on HEAD (resume reuses it)")
	run.Flags().Lookup("resume").Usage = "Resume the last successful session (worktree: last completed session with an id)"
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
				fmt.Fprintf(c.OutOrStdout(), "已应用到原工作区（未提交）；可以 pactify sub discard %s\n", args[0])
			} else {
				fmt.Fprintf(c.OutOrStdout(), "%s worktree 已丢弃\n", args[0])
			}
		}})
	}
}

func subCreateRun(c *cobra.Command, store sub.Store, id string, req sub.Request, resume bool) (sub.State, int, error) {
	worktree, _ := c.Flags().GetBool("worktree")
	st, err := store.CreateRun(id, req, worktree, resume)
	code := 1
	var noSession *sub.NoSessionError
	if errors.As(err, &noSession) {
		code = 2
	}
	return st, code, err
}
