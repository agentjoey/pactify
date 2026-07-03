package main

import (
	"context"
	"fmt"
	"time"

	"github.com/agentjoey/pactify/internal/cloudauth"
	"github.com/agentjoey/pactify/internal/cloudclient"
	"github.com/spf13/cobra"
)

func newAccountCmd() *cobra.Command {
	acct := &cobra.Command{
		Use:   "account",
		Short: "authenticate this machine to the AgentWorks relay (cloud account)",
		Long: "Derive the account keypair from your master secret and exchange it for a\n" +
			"relay bearer token. The master secret is read from $PACTIFY_MASTER_SECRET,\n" +
			"$LINX_MASTER_SECRET, ~/.config/pactify/master-secret, or ~/.config/linx/master-secret.",
	}
	acct.AddCommand(newAccountPairCmd(), newAccountLoginCmd(), newAccountWhoamiCmd(), newAccountTokenCmd())
	return acct
}

func newAccountPairCmd() *cobra.Command {
	var relay string
	var force bool
	c := &cobra.Command{
		Use:   "pair",
		Short: "join an existing account by pairing with an already-authenticated device",
		Long: "Relay-blind pairing: this machine publishes an ephemeral key and receives\n" +
			"the account master secret, wrapped end-to-end, from a device that already has\n" +
			"it. Approve the printed code on that device (linx web/CLI). The relay never\n" +
			"sees the secret.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := resolveRelayURL(relay)
			if err != nil {
				return err
			}
			if !force {
				if _, err := cloudauth.LoadMasterSecret(); err == nil {
					return fmt.Errorf("a master secret already exists — pass --force to re-pair and overwrite")
				}
			}
			out := cmd.OutOrStdout()
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()
			secret, err := cloudclient.New(url).Pair(ctx, func(code string) {
				fmt.Fprintf(out, "Pairing code: %s\n", code)
				fmt.Fprintln(out, "On an already-paired device (linx web/CLI), approve this code. Waiting…")
			})
			if err != nil {
				return err
			}
			if err := cloudauth.SaveMasterSecret(secret, force); err != nil {
				return err
			}
			path, _ := cloudauth.PactifySecretPath()
			fmt.Fprintf(out, "paired — master secret saved to %s\nrun `pactify account login` to get a relay token.\n", path)
			return nil
		},
	}
	c.Flags().StringVar(&relay, "relay", "", "relay base URL (default $PACT_RELAY_URL)")
	c.Flags().BoolVar(&force, "force", false, "overwrite an existing master secret")
	return c
}

func resolveRelayURL(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if u := cloudclient.RelayURLFromEnv(); u != "" {
		return u, nil
	}
	return "", fmt.Errorf("no relay URL: pass --relay or set $PACT_RELAY_URL")
}

func newAccountLoginCmd() *cobra.Command {
	var relay string
	c := &cobra.Command{
		Use:   "login",
		Short: "authenticate to the relay and cache a session token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := resolveRelayURL(relay)
			if err != nil {
				return err
			}
			master, err := cloudauth.LoadMasterSecret()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			sess, err := cloudclient.New(url).Authenticate(ctx, master)
			if err != nil {
				return err
			}
			if err := cloudclient.SaveSession(sess); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "logged in as account %s via %s\n", sess.AccountID, sess.RelayURL)
			return nil
		},
	}
	c.Flags().StringVar(&relay, "relay", "", "relay base URL (default $PACT_RELAY_URL)")
	return c
}

func newAccountWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "show the cached account session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			sess, err := cloudclient.LoadSession()
			if err != nil {
				return fmt.Errorf("no cached session (run `pactify account login`): %w", err)
			}
			fmt.Fprintf(out, "account:  %s\n", sess.AccountID)
			fmt.Fprintf(out, "relay:    %s\n", sess.RelayURL)
			fmt.Fprintf(out, "pubkey:   %s\n", sess.PublicKeyHex)
			if exp, ok := cloudclient.TokenExpMs(sess.Token); ok {
				d := time.UnixMilli(exp)
				state := "valid"
				if time.Now().After(d) {
					state = "EXPIRED"
				}
				fmt.Fprintf(out, "token:    %s (expires %s)\n", state, d.Format(time.RFC3339))
			}
			// Confirm the master secret is still reachable (needed to re-login).
			if _, err := cloudauth.LoadMasterSecret(); err != nil {
				fmt.Fprintf(out, "secret:   NOT FOUND (%v)\n", err)
			} else {
				fmt.Fprintln(out, "secret:   present")
			}
			return nil
		},
	}
}

func newAccountTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "print the cached bearer token (for scripting)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sess, err := cloudclient.LoadSession()
			if err != nil {
				return fmt.Errorf("no cached session (run `pactify account login`): %w", err)
			}
			if exp, ok := cloudclient.TokenExpMs(sess.Token); ok && time.Now().After(time.UnixMilli(exp)) {
				return fmt.Errorf("cached token expired — run `pactify account login`")
			}
			fmt.Fprintln(cmd.OutOrStdout(), sess.Token)
			return nil
		},
	}
}
