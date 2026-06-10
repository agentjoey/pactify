package mcp

import (
	"context"
	"os"

	"github.com/agentjoey/pactify/internal/paths"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	stateURI = "pact://state"
	logURI   = "pact://log"
)

// readFileResource returns a handler that reads path at request time (so a
// PACT_DIR change between requests is honored). A missing file reads as empty.
func readFileResource(uri string, path func() string, mime string) sdk.ResourceHandler {
	return func(_ context.Context, _ *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		b, err := os.ReadFile(path())
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: uri, MIMEType: mime, Text: string(b)}}}, nil
	}
}

func registerResources(s *sdk.Server) {
	s.AddResource(&sdk.Resource{URI: stateURI, Name: "pact STATE.yml", MIMEType: "application/yaml",
		Description: "The rendered pact state (projection of the log; the log is authoritative)"},
		readFileResource(stateURI, paths.State, "application/yaml"))
	s.AddResource(&sdk.Resource{URI: logURI, Name: "pact log.jsonl", MIMEType: "application/jsonl",
		Description: "The authoritative append-only pact event log"},
		readFileResource(logURI, paths.Log, "application/jsonl"))
}
