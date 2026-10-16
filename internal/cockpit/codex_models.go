package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"
)

// CodexModels lists available ids through model/list, without creating a thread
// or issuing a turn. Doctor and other id-only consumers share this path.
func CodexModels(ctx context.Context, opts StartOpts) ([]string, error) {
	catalog, err := CodexModelDetails(ctx, opts)
	return catalog.Items, err
}

// CodexModelDetails adds the advertised reasoning effort union for Settings.
// A fresh minimum-config app-server is always closed, including on timeout.
func CodexModelDetails(ctx context.Context, opts StartOpts) (catalog ModelDiscovery, err error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	opts.MinimalConfig = true
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	server, err := newCodexAppServer(ctx, opts)
	if err != nil {
		return catalog, err
	}
	defer func() { err = errors.Join(err, server.Close()) }()
	catalog, err = server.client.modelDetails(ctx)
	if err != nil {
		return catalog, fmt.Errorf("codex: %w", err)
	}
	if len(catalog.Items) == 0 {
		return catalog, fmt.Errorf("codex: model/list returned no models")
	}
	return catalog, nil
}

// modelDetails is also used when an explicit model request fails. Pagination
// and parsing stay identical for discovery and ordinary backend diagnostics.
func (c *codexClient) modelDetails(ctx context.Context) (ModelDiscovery, error) {
	out := ModelDiscovery{Items: []string{}, Efforts: []string{}}
	cursor := ""
	seen := map[string]bool{}
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.rpc.call(ctx, "model/list", params)
		if err != nil {
			return out, fmt.Errorf("model/list failed: %w", err)
		}
		var page struct {
			Data []struct {
				Model                     string `json:"model"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return out, fmt.Errorf("invalid model/list response")
		}
		for _, model := range page.Data {
			if model.Model == "" {
				continue
			}
			if !slices.Contains(out.Items, model.Model) {
				out.Items = append(out.Items, model.Model)
			}
			for _, option := range model.SupportedReasoningEfforts {
				if option.ReasoningEffort != "" && !slices.Contains(out.Efforts, option.ReasoningEffort) {
					out.Efforts = append(out.Efforts, option.ReasoningEffort)
				}
			}
		}
		if page.NextCursor == "" {
			return out, nil
		}
		if seen[page.NextCursor] {
			return out, fmt.Errorf("model/list repeated cursor")
		}
		cursor = page.NextCursor
		seen[cursor] = true
	}
}
