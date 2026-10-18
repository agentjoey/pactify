package main

import (
	"io"
	"strings"
	"testing"
)

func TestRootCommands(t *testing.T) {
	root := newRootCmd()
	var names []string
	for _, cmd := range root.Commands() {
		names = append(names, cmd.Name())
	}
	if got := strings.Join(names, ","); got != "serve,sub,version" {
		t.Fatalf("root commands = %s", got)
	}
}

func TestRetiredCommandsRejected(t *testing.T) {
	for _, name := range strings.Fields("init join assign checkpoint accept changes merge cancel withdraw config status log validate seat agent doctor mcp register unregister list setup orchestrate plan run schedule audit sessions recipe finish role account") {
		t.Run(name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{name})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("retired command must be rejected, got %v", err)
			}
		})
	}
}
