package query

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/index/treesitter"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
)

// SearchOpts configures a symbol index search.
type SearchOpts struct {
	Query     string
	Limit     int
	MatchMode string
	NameMatch string
}

// Search runs an FTS or name-match search against the symbol index.
func Search(ctx context.Context, st *store.Store, coord *index.SyncCoordinator, opts SearchOpts) ([]store.SearchHit, error) {
	if opts.Query == "" {
		return []store.SearchHit{}, nil
	}

	if coord != nil {
		coord.EnsureFresh(ctx)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	hits, err := st.SearchWithOptions(opts.Query, limit, store.SearchOptions{
		MatchMode: opts.MatchMode,
		NameMatch: opts.NameMatch,
	})
	if err != nil {
		return nil, fmt.Errorf("search index: %w", err)
	}
	if hits == nil {
		hits = []store.SearchHit{}
	}
	return hits, nil
}

// Outline returns indexed symbols for a single file.
func Outline(ctx context.Context, st *store.Store, coord *index.SyncCoordinator, filePath string) ([]store.OutlineEntry, error) {
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	if coord != nil {
		coord.EnsureFresh(ctx)
	}

	symbols, err := st.ListSymbolsByFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("outline file: %w", err)
	}

	entries := make([]store.OutlineEntry, 0, len(symbols))
	for _, sym := range symbols {
		entries = append(entries, store.OutlineEntryFromRecord(sym))
	}
	return entries, nil
}

// ReadSymbol returns the source text for an indexed symbol.
func ReadSymbol(_ context.Context, st *store.Store, reader *read.Reader, symbolID string) (string, error) {
	if symbolID == "" {
		return "", fmt.Errorf("symbol_id is required")
	}

	sym, err := st.GetSymbolByID(symbolID)
	if err != nil {
		return "", err
	}

	text, err := reader.ReadSymbol(sym)
	if err != nil {
		return "", err
	}
	return text, nil
}

// FindCallersOpts configures a callee caller lookup.
type FindCallersOpts struct {
	Name     string
	SymbolID string
	Dir      string
	Limit    int
}

// FindCallersResult holds caller hits for a resolved callee name.
type FindCallersResult struct {
	CalleeName string
	Hits       []store.CallerHit
}

// ErrNoCallers indicates FindCallers found no indexed call sites for the callee.
var ErrNoCallers = errors.New("no callers found")

// FindCallers returns indexed call sites for a callee by exact name.
func FindCallers(ctx context.Context, st *store.Store, coord *index.SyncCoordinator, opts FindCallersOpts) (FindCallersResult, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" && opts.SymbolID != "" {
		rec, err := store.ParseSymbolID(opts.SymbolID)
		if err != nil {
			return FindCallersResult{}, err
		}
		name = rec.Name
	}
	if name == "" {
		return FindCallersResult{}, fmt.Errorf("name or symbol_id is required")
	}

	if coord != nil {
		coord.EnsureFresh(ctx)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	hits, err := st.FindCallers(name, opts.Dir, limit)
	if err != nil {
		return FindCallersResult{}, fmt.Errorf("find callers: %w", err)
	}
	if hits == nil {
		hits = []store.CallerHit{}
	}
	if len(hits) == 0 {
		return FindCallersResult{CalleeName: name}, ErrNoCallers
	}
	return FindCallersResult{CalleeName: name, Hits: hits}, nil
}

// NoCallersMessage returns the agent-facing hint when ErrNoCallers is returned.
func NoCallersMessage(calleeName string) string {
	return fmt.Sprintf(
		"no callers found for %q (exact name match); the indexed callee name may differ — try search_code_skeleton first",
		calleeName,
	)
}

// MapDirectoryOpts configures a directory architecture sketch query.
type MapDirectoryOpts struct {
	Dir       string
	DefLimit  int
	CallLimit int
}

// MapDirectory returns symbol definitions and outgoing calls under a repo-relative directory prefix.
func MapDirectory(ctx context.Context, st *store.Store, coord *index.SyncCoordinator, opts MapDirectoryOpts) (store.DirectoryMap, error) {
	if strings.TrimSpace(opts.Dir) == "" {
		return store.DirectoryMap{}, fmt.Errorf("dir is required")
	}

	if coord != nil {
		coord.EnsureFresh(ctx)
	}

	result, err := st.MapDirectory(opts.Dir, opts.DefLimit, opts.CallLimit)
	if err != nil {
		return store.DirectoryMap{}, fmt.Errorf("map directory: %w", err)
	}
	return result, nil
}

// ErrSyntaxCheckUnavailable indicates syntax check was not compiled in.
var ErrSyntaxCheckUnavailable = errors.New("syntax check requires tree-sitter build")

// CheckFileResult holds the syntax check outcome for a single file.
type CheckFileResult struct {
	FilePath string                `json:"file_path"`
	OK       bool                  `json:"ok"`
	Errors   []treesitter.ParseError `json:"errors"`
}

// CheckFile parses a file for tree-sitter ERROR nodes on supported extensions.
func CheckFile(_ context.Context, repoRoot, filePath string) (CheckFileResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return CheckFileResult{}, fmt.Errorf("file_path is required")
	}

	if !treesitter.CheckSyntaxEnabled() {
		return CheckFileResult{}, ErrSyntaxCheckUnavailable
	}

	errs, err := treesitter.CheckSyntax(repoRoot, filePath)
	if err != nil {
		return CheckFileResult{}, err
	}
	if errs == nil {
		errs = []treesitter.ParseError{}
	}
	return CheckFileResult{
		FilePath: filePath,
		OK:       len(errs) == 0,
		Errors:   errs,
	}, nil
}
