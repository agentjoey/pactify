// Command permissions exports the adapter's exact policies for the isolated API probe.
package main

import (
	"encoding/json"
	"github.com/agentjoey/pactify/internal/cockpit"
	"os"
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(map[string][]cockpit.Opencode2Rule{
		"readonly": cockpit.Opencode2Permissions(cockpit.AccessReadOnly),
		"write":    cockpit.Opencode2Permissions(cockpit.AccessWrite),
	}); err != nil {
		panic(err)
	}
}
