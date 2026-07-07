package index

import (
	"context"

	"github.com/stubbies/litos-mcp/internal/index/treesitter"
	"github.com/stubbies/litos-mcp/internal/store"
)

// ExtractCallSites extracts callee invocations from paths using tree-sitter when
// compiled in, otherwise per-line regex heuristics for supported extensions.
func ExtractCallSites(ctx context.Context, repoRoot string, paths []string) ([]store.CallSiteRecord, error) {
	if treesitter.CallSitesEnabled() {
		return treesitter.ExtractCallSites(repoRoot, paths)
	}
	return extractCallSitesRegex(ctx, repoRoot, paths)
}
