package index

import (
	"context"

	"github.com/stubbies/litos-mcp/internal/index/treesitter"
	"github.com/stubbies/litos-mcp/internal/store"
)

type compositeExtractor struct {
	primary Extractor
}

func (e *compositeExtractor) Name() string {
	return e.primary.Name()
}

func (e *compositeExtractor) Extract(ctx context.Context, repoRoot string, paths []string) ([]store.SymbolRecord, error) {
	symbols, err := e.primary.Extract(ctx, repoRoot, paths)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return treesitter.RefineBoundaries(repoRoot, symbols)
}
