package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
)

const (
	searchToolDescription = "Searches the lightweight, language-agnostic code skeleton repository index using functional keywords. " +
		"Returns target file paths, matched symbols, and source line numbers without exposing functional block code logic. " +
		"Prefer this tool over grep or bulk file reads for code discovery."
	readToolDescription = "Reads an exact targeted line number slice from a designated workspace repository file path. " +
		"Use only after search_code_skeleton confirms the file path and line range."
	reindexToolDescription = "Runs a full crawl and re-extract of the repository index. " +
		"Use after large changes (e.g. git pull) or when search hits seem stale; normal edits are synced automatically."
)

type searchInput struct {
	Query     string `json:"query" jsonschema:"Functional keyword search context (e.g. 'jwt verification', 'user schema', 'database connection')."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum structural records to output."`
	MatchMode string `json:"match_mode,omitempty" jsonschema:"Multi-token match mode: 'and' (default) requires all tokens; 'or' matches any token."`
}

type readInput struct {
	FilePath  string `json:"file_path" jsonschema:"Relative file tracking path from repo root."`
	StartLine int    `json:"start_line" jsonschema:"Starting line target (1-indexed)."`
	EndLine   int    `json:"end_line" jsonschema:"Ending line target inclusive."`
}

type reindexResult struct {
	Files     int    `json:"files"`
	Symbols   int    `json:"symbols"`
	Indexer   string `json:"indexer"`
	ElapsedMs int64  `json:"elapsed_ms"`
}

type toolEnv struct {
	store       *store.Store
	reader      *read.Reader
	coordinator *index.SyncCoordinator
}

func registerTools(server *mcpsdk.Server, env *toolEnv) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "search_code_skeleton",
		Description: searchToolDescription,
	}, env.handleSearch)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "read_file_lines",
		Description: readToolDescription,
	}, env.handleRead)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "reindex_index",
		Description: reindexToolDescription,
	}, env.handleReindex)
}

func (e *toolEnv) handleSearch(ctx context.Context, _ *mcpsdk.CallToolRequest, in searchInput) (*mcpsdk.CallToolResult, any, error) {
	if in.Query == "" {
		return searchResult([]store.SearchHit{})
	}

	if e.coordinator != nil {
		e.coordinator.EnsureFresh(ctx)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}

	hits, err := e.store.SearchWithOptions(in.Query, limit, store.SearchOptions{
		MatchMode: in.MatchMode,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("search index: %w", err)
	}
	return searchResult(hits)
}

func searchResult(hits []store.SearchHit) (*mcpsdk.CallToolResult, any, error) {
	if hits == nil {
		hits = []store.SearchHit{}
	}
	data, err := json.Marshal(hits)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal search results: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, nil, nil
}

func (e *toolEnv) handleReindex(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
	if e.coordinator == nil {
		return nil, nil, fmt.Errorf("index sync coordinator not configured")
	}

	result, err := e.coordinator.ReconcileFull(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reindex: %w", err)
	}

	out := reindexResult{
		Files:     result.FilesIndexed,
		Symbols:   result.SymbolsIndexed,
		Indexer:   result.Indexer,
		ElapsedMs: result.Elapsed.Milliseconds(),
	}
	data, err := json.Marshal(out)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal reindex result: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, nil, nil
}

func (e *toolEnv) handleRead(_ context.Context, _ *mcpsdk.CallToolRequest, in readInput) (*mcpsdk.CallToolResult, any, error) {
	text, err := e.reader.ReadLines(in.FilePath, in.StartLine, in.EndLine)
	if err != nil {
		return nil, nil, mapReadError(err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
	}, nil, nil
}

func mapReadError(err error) error {
	switch {
	case errors.Is(err, read.ErrFileNotFound):
		return fmt.Errorf("file not found")
	case errors.Is(err, read.ErrPathOutsideRoot):
		return fmt.Errorf("path outside repository root")
	case errors.Is(err, read.ErrInvalidRange):
		return fmt.Errorf("invalid line range")
	case errors.Is(err, read.ErrSpanTooLarge):
		return read.ErrSpanTooLarge
	case errors.Is(err, read.ErrResponseTooLarge):
		return read.ErrResponseTooLarge
	case errors.Is(err, read.ErrNotAFile):
		return fmt.Errorf("path is not a regular file")
	default:
		return err
	}
}
