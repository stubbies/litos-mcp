package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const indexStatusURI = "litos://index/status"

func registerResources(server *mcpsdk.Server, env *toolEnv) {
	server.AddResource(&mcpsdk.Resource{
		URI:         indexStatusURI,
		Name:        "index_status",
		Description: "Current litos-mcp index sync status (files, symbols, indexer, reconcile flag).",
		MIMEType:    "application/json",
	}, env.handleIndexStatus)
}

func (e *toolEnv) handleIndexStatus(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	if e.coordinator == nil {
		return nil, fmt.Errorf("index sync coordinator not configured")
	}

	status, err := e.coordinator.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("index status: %w", err)
	}

	data, err := json.Marshal(status)
	if err != nil {
		return nil, fmt.Errorf("marshal index status: %w", err)
	}

	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{{
			URI:      indexStatusURI,
			MIMEType: "application/json",
			Text:     string(data),
		}},
	}, nil
}
