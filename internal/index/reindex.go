package index

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/stubbies/litos-mcp/internal/crawl"
	"github.com/stubbies/litos-mcp/internal/store"
)

func filterCrawledForExtractor(crawled []crawl.File, ext Extractor) []crawl.File {
	if ext.Name() != "regex" {
		return crawled
	}
	filtered := make([]crawl.File, 0, len(crawled))
	for _, f := range crawled {
		if regexSupportedPath(f.Path) {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// ReindexResult summarizes a crawl → extract → store run.
type ReindexResult struct {
	FilesIndexed   int
	SymbolsIndexed int
	Indexer        string
	Elapsed        time.Duration
	DBBytes        int64
}

// Reindex crawls repoRoot, incrementally extracts symbols, and upserts into st.
func Reindex(ctx context.Context, repoRoot string, st *store.Store, ext Extractor) (*ReindexResult, error) {
	start := time.Now()

	crawled, err := crawl.Crawl(ctx, repoRoot, crawl.Options{})
	if err != nil {
		return nil, fmt.Errorf("crawl: %w", err)
	}
	crawled = filterCrawledForExtractor(crawled, ext)

	crawledByPath := make(map[string]crawl.File, len(crawled))
	for _, f := range crawled {
		crawledByPath[f.Path] = f
	}

	indexed, err := st.ListFiles()
	if err != nil {
		return nil, fmt.Errorf("list indexed files: %w", err)
	}
	for _, meta := range indexed {
		if _, ok := crawledByPath[meta.Path]; !ok {
			if err := st.RemoveFile(meta.Path); err != nil {
				return nil, fmt.Errorf("remove stale index for %s: %w", meta.Path, err)
			}
		}
	}

	var stalePaths []string
	metaByPath := make(map[string]store.FileMeta, len(crawled))
	for _, f := range crawled {
		metaByPath[f.Path] = store.FileMeta{Path: f.Path, MtimeNs: f.MtimeNs, Size: f.Size}
		stale, err := st.IsStale(f.Path, f.MtimeNs, f.Size)
		if err != nil {
			return nil, fmt.Errorf("check staleness for %s: %w", f.Path, err)
		}
		if stale {
			stalePaths = append(stalePaths, f.Path)
		}
	}
	if err := SyncPaths(ctx, repoRoot, st, ext, stalePaths, metaByPath); err != nil {
		return nil, err
	}

	fileCount, err := st.FileCount()
	if err != nil {
		return nil, fmt.Errorf("count files: %w", err)
	}
	symbolCount, err := st.SymbolCount()
	if err != nil {
		return nil, fmt.Errorf("count symbols: %w", err)
	}

	dbBytes, err := dbSize(st.Path())
	if err != nil {
		return nil, fmt.Errorf("stat database: %w", err)
	}

	return &ReindexResult{
		FilesIndexed:   fileCount,
		SymbolsIndexed: symbolCount,
		Indexer:        ext.Name(),
		Elapsed:        time.Since(start),
		DBBytes:        dbBytes,
	}, nil
}

func groupSymbolsByFile(symbols []store.SymbolRecord) map[string][]store.SymbolRecord {
	out := make(map[string][]store.SymbolRecord)
	for _, sym := range symbols {
		out[sym.FilePath] = append(out[sym.FilePath], store.SymbolRecord{
			Name:      sym.Name,
			Kind:      sym.Kind,
			Scope:     sym.Scope,
			StartLine: sym.StartLine,
			EndLine:   sym.EndLine,
		})
	}
	return out
}

func dbSize(path string) (int64, error) {
	if path == "" || path == ":memory:" {
		return 0, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
