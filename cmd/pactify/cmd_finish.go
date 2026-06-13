package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/agentjoey/pactify/internal/finish"
	"github.com/spf13/cobra"
)

func newFinishCmd() *cobra.Command {
	var remote, branch string
	var pr bool
	var head, title, body string

	cmd := &cobra.Command{
		Use:   "finish",
		Short: "deliver merged work to remote (push + optional PR)",
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			f := finish.Finisher{
				Run: func(dir, name string, args ...string) (string, error) {
					cmd := exec.Command(name, args...)
					cmd.Dir = dir
					out, err := cmd.CombinedOutput()
					return string(out), err
				},
				HasGH: func() bool {
					_, err := exec.LookPath("gh")
					return err == nil
				},
			}

			if pr {
				if head == "" || title == "" {
					return fmt.Errorf("finish: --head and --title are required when --pr is set")
				}
				out, err := f.OpenPR(cwd, branch, head, title, body)
				if err != nil {
					return err
				}
				fmt.Fprint(c.OutOrStdout(), out)
				return nil
			}

			out, err := f.Push(cwd, remote, branch)
			if err != nil {
				return err
			}
			fmt.Fprint(c.OutOrStdout(), out)
			return nil
		},
	}

	cmd.Flags().StringVar(&remote, "remote", "origin", "git remote name")
	cmd.Flags().StringVar(&branch, "branch", "main", "branch to push")
	cmd.Flags().BoolVar(&pr, "pr", false, "open a pull request instead of pushing")
	cmd.Flags().StringVar(&head, "head", "", "head branch for PR (required with --pr)")
	cmd.Flags().StringVar(&title, "title", "", "PR title (required with --pr)")
	cmd.Flags().StringVar(&body, "body", "", "PR body text")

	return cmd
}
