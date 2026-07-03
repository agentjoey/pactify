package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAccountCmdRegistered(t *testing.T) {
	root := newRootCmd()
	acct, _, err := root.Find([]string{"account"})
	if err != nil || acct.Name() != "account" {
		t.Fatalf("account command not registered: %v", err)
	}
	for _, sub := range []string{"login", "whoami", "token"} {
		if c, _, err := acct.Find([]string{sub}); err != nil || c.Name() != sub {
			t.Fatalf("account %s not registered: %v", sub, err)
		}
	}
}

func TestAccountLoginNoRelayErrors(t *testing.T) {
	t.Setenv("PACT_RELAY_URL", "")
	root := newRootCmd()
	root.SetArgs([]string{"account", "login"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "no relay URL") {
		t.Fatalf("expected no-relay error, got %v", err)
	}
}
