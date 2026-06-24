package index

import (
	"context"
	"fmt"

	"github.com/stubbies/litos-mcp/internal/crawl"
	"github.com/stubbies/litos-mcp/internal/store"
)

// NeedsReindex reports whether the on-disk tree differs from the indexed state.
func NeedsReindex(ctx context.Context, repoRoot string, st *store.Store) (bool, error) {
	crawled, err := crawl.Crawl(ctx, repoRoot, crawl.Options{})
	if err != nil {
		return false, fmt.Errorf("crawl: %w", err)
	}

	crawledByPath := make(map[string]crawl.File, len(crawled))
	for _, f := range crawled {
		crawledByPath[f.Path] = f
	}

	indexed, err := st.ListFiles()
	if err != nil {
		return false, fmt.Errorf("list indexed files: %w", err)
	}
	for _, meta := range indexed {
		if _, ok := crawledByPath[meta.Path]; !ok {
			return true, nil
		}
	}

	for _, f := range crawled {
		stale, err := st.IsStale(f.Path, f.MtimeNs, f.Size)
		if err != nil {
			return false, fmt.Errorf("check staleness for %s: %w", f.Path, err)
		}
		if stale {
			return true, nil
		}
	}
	return false, nil
}
