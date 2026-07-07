package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
		"Fallback when you lack a symbol_id; prefer read_symbol after search_code_skeleton or outline_file."
	readSymbolToolDescription = "Reads the source slice for a symbol by its stable symbol_id from search_code_skeleton or outline_file. " +
		"Prefer this over read_file_lines when you have a symbol_id; re-search if the symbol moved after edits."
	outlineToolDescription = "Returns the indexed symbol skeleton for one file: symbol_ids, kinds, scopes, and line ranges. " +
		"Use when you know the file path and need its structure before fetching symbols with read_symbol."
	reindexToolDescription = "Runs a full crawl and re-extract of the repository index. " +
		"Use after large changes (e.g. git pull) or when search hits seem stale; normal edits are synced automatically."
	findCallersToolDescription = "Finds indexed call sites for a callee by exact name (case-sensitive, no type resolution). " +
		"Pass name or symbol_id from search_code_skeleton. Use dir to limit to a repo-relative directory prefix. " +
		"If no hits, the indexed callee name may differ — try search_code_skeleton first."
)

type searchInput struct {
	Query     string `json:"query" jsonschema:"Functional keyword search context (e.g. 'jwt verification', 'user schema', 'database connection'), or symbol name when name_match is set."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum structural records to output."`
	MatchMode string `json:"match_mode,omitempty" jsonschema:"Multi-token FTS match mode: 'and' (default) requires all tokens; 'or' matches any token. Ignored when name_match is set."`
	NameMatch string `json:"name_match,omitempty" jsonschema:"Symbol name lookup mode: omit for FTS keyword search; 'exact' for case-sensitive name equality; 'contains' for case-sensitive substring match."`
}

type readInput struct {
	FilePath  string `json:"file_path" jsonschema:"Relative file tracking path from repo root."`
	StartLine int    `json:"start_line" jsonschema:"Starting line target (1-indexed)."`
	EndLine   int    `json:"end_line" jsonschema:"Ending line target inclusive."`
}

type readSymbolInput struct {
	SymbolID string `json:"symbol_id" jsonschema:"Stable symbol identifier from search_code_skeleton or outline_file (format: file_path#kind#name#start_line)."`
}

type outlineInput struct {
	FilePath string `json:"file_path" jsonschema:"Relative file path from repo root."`
}

type reindexResult struct {
	Files     int    `json:"files"`
	Symbols   int    `json:"symbols"`
	Indexer   string `json:"indexer"`
	ElapsedMs int64  `json:"elapsed_ms"`
}

type findCallersInput struct {
	Name     string `json:"name,omitempty" jsonschema:"Callee symbol name (exact, case-sensitive). Required unless symbol_id is set."`
	SymbolID string `json:"symbol_id,omitempty" jsonschema:"Parse callee name from a search_code_skeleton symbol_id when name is omitted."`
	Dir      string `json:"dir,omitempty" jsonschema:"Optional repo-relative directory prefix filter."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum caller hits to return (default 20)."`
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
		Name:        "read_symbol",
		Description: readSymbolToolDescription,
	}, env.handleReadSymbol)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "outline_file",
		Description: outlineToolDescription,
	}, env.handleOutline)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "reindex_index",
		Description: reindexToolDescription,
	}, env.handleReindex)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "find_callers",
		Description: findCallersToolDescription,
	}, env.handleFindCallers)
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
		NameMatch: in.NameMatch,
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

func (e *toolEnv) handleFindCallers(ctx context.Context, _ *mcpsdk.CallToolRequest, in findCallersInput) (*mcpsdk.CallToolResult, any, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" && in.SymbolID != "" {
		rec, err := store.ParseSymbolID(in.SymbolID)
		if err != nil {
			return nil, nil, mapSymbolError(in.SymbolID, err)
		}
		name = rec.Name
	}
	if name == "" {
		return nil, nil, fmt.Errorf("name or symbol_id is required")
	}

	if e.coordinator != nil {
		e.coordinator.EnsureFresh(ctx)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}

	hits, err := e.store.FindCallers(name, in.Dir, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("find callers: %w", err)
	}
	return callersResult(name, hits)
}

func callersResult(name string, hits []store.CallerHit) (*mcpsdk.CallToolResult, any, error) {
	if hits == nil {
		hits = []store.CallerHit{}
	}
	if len(hits) == 0 {
		return &mcpsdk.CallToolResult{
			IsError: true,
			Content: []mcpsdk.Content{&mcpsdk.TextContent{
				Text: fmt.Sprintf("no callers found for %q (exact name match); the indexed callee name may differ — try search_code_skeleton first", name),
			}},
		}, nil, nil
	}
	data, err := json.Marshal(hits)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal caller hits: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, nil, nil
}

func (e *toolEnv) handleOutline(ctx context.Context, _ *mcpsdk.CallToolRequest, in outlineInput) (*mcpsdk.CallToolResult, any, error) {
	if in.FilePath == "" {
		return nil, nil, fmt.Errorf("file_path is required")
	}

	if e.coordinator != nil {
		e.coordinator.EnsureFresh(ctx)
	}

	symbols, err := e.store.ListSymbolsByFile(in.FilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("outline file: %w", err)
	}

	entries := make([]store.OutlineEntry, 0, len(symbols))
	for _, sym := range symbols {
		entries = append(entries, store.OutlineEntryFromRecord(sym))
	}
	return outlineResult(entries)
}

func outlineResult(entries []store.OutlineEntry) (*mcpsdk.CallToolResult, any, error) {
	if entries == nil {
		entries = []store.OutlineEntry{}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal outline results: %w", err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, nil, nil
}

func (e *toolEnv) handleReadSymbol(ctx context.Context, _ *mcpsdk.CallToolRequest, in readSymbolInput) (*mcpsdk.CallToolResult, any, error) {
	if in.SymbolID == "" {
		return nil, nil, fmt.Errorf("symbol_id is required")
	}

	if e.coordinator != nil {
		e.coordinator.EnsureFresh(ctx)
	}

	sym, err := e.store.GetSymbolByID(in.SymbolID)
	if err != nil {
		return nil, nil, mapSymbolError(in.SymbolID, err)
	}

	text, err := e.reader.ReadSymbol(sym)
	if err != nil {
		return nil, nil, mapReadError(err)
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
	}, nil, nil
}

func mapSymbolError(id string, err error) error {
	if errors.Is(err, store.ErrInvalidSymbolID) {
		return fmt.Errorf("invalid symbol id: %w", err)
	}
	if errors.Is(err, store.ErrSymbolNotFound) {
		return fmt.Errorf("symbol not found: %s (symbol may be stale after edits; re-search to get a fresh symbol_id)", id)
	}
	return err
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
