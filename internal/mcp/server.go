// Package mcp exposes the pact protocol over the Model Context Protocol:
// every verb as a tool, STATE/log as resources, log changes as notifications.
package mcp

import (
	"context"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/paths"
	"github.com/fsnotify/fsnotify"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// New builds the pactify MCP server with all tools and resources registered.
// SubscribeHandler is set to enable the resources.subscribe capability so
// clients can call Subscribe and receive ResourceUpdated notifications.
func New() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "pactify", Version: "v1"}, &sdk.ServerOptions{
		SubscribeHandler: func(_ context.Context, _ *sdk.SubscribeRequest) error {
			return nil
		},
		UnsubscribeHandler: func(_ context.Context, _ *sdk.UnsubscribeRequest) error {
			return nil
		},
	})
	registerTools(s)
	registerResources(s)
	return s
}

// Watch starts an fsnotify watcher on the project's .pact/ dir and fires
// ResourceUpdated for pact://log and pact://state when log.jsonl changes.
// Returns a stop function.
func Watch(ctx context.Context, s *sdk.Server) (func(), error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(paths.Dir()); err != nil {
		w.Close()
		return nil, err
	}
	logFile := filepath.Clean(paths.Log())
	go func() {
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 || filepath.Clean(ev.Name) != logFile {
					continue
				}
				_ = s.ResourceUpdated(ctx, &sdk.ResourceUpdatedNotificationParams{URI: logURI})
				_ = s.ResourceUpdated(ctx, &sdk.ResourceUpdatedNotificationParams{URI: stateURI})
			case <-ctx.Done():
				return
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() { _ = w.Close() }, nil
}
