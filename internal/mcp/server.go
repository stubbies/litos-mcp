package mcp

import (
	"context"
	"fmt"
	"os"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
)

// Config holds shared runtime context for MCP tool handlers.
type Config struct {
	RepoRoot    string
	Store       *store.Store
	Reader      *read.Reader
	Version     string
	Coordinator *index.SyncCoordinator
}

// NewServer registers litos-mcp tools on an MCP server instance.
func NewServer(cfg Config) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "litos-mcp",
		Version: cfg.Version,
	}, nil)

	env := &toolEnv{
		store:       cfg.Store,
		reader:      cfg.Reader,
		coordinator: cfg.Coordinator,
	}
	registerTools(server, env)
	registerResources(server, env)
	registerPrompts(server)
	return server
}

// Run serves MCP over stdio and keeps the index warm with fsnotify sync.
func Run(ctx context.Context, cfg Config) error {
	fmt.Fprintf(os.Stderr, "litos-mcp serve: repo=%s db=%s\n", cfg.RepoRoot, cfg.Store.Path())

	if cfg.Coordinator != nil {
		if err := cfg.Coordinator.StartWatcher(ctx); err != nil {
			return fmt.Errorf("start file watcher: %w", err)
		}
	}

	server := NewServer(cfg)
	return server.Run(ctx, &mcpsdk.StdioTransport{})
}
