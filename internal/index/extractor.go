package index

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stubbies/litos-mcp/internal/store"
)

const batchSize = 200

// Extractor extracts structural symbols from source files under repoRoot.
type Extractor interface {
	// Name returns the indexer identifier ("ctags" or "regex").
	Name() string
	// Extract returns symbols for repo-relative file paths (forward slashes).
	Extract(ctx context.Context, repoRoot string, paths []string) ([]store.SymbolRecord, error)
}

// NewExtractor returns a ctags extractor when available, otherwise regex.
func NewExtractor() Extractor {
	if cmd := CtagsCommand(); cmd != "" {
		return NewCtagsExtractor(cmd)
	}
	return NewRegexExtractor()
}

// normalizeTagPath converts an absolute or relative ctags path to a repo-relative
// forward-slash path, rejecting paths outside repoRoot.
func normalizeTagPath(repoRoot, tagPath string) (string, error) {
	abs := tagPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(repoRoot, abs)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	relPath, err := filepath.Rel(repoRoot, abs)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("tag path outside repo root: %s", tagPath)
	}
	return filepath.ToSlash(relPath), nil
}

func batchPaths(paths []string) [][]string {
	if len(paths) == 0 {
		return nil
	}
	var batches [][]string
	for i := 0; i < len(paths); i += batchSize {
		end := i + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batches = append(batches, paths[i:end])
	}
	return batches
}

func deriveEndLines(symbols []store.SymbolRecord, lineCounts map[string]int) []store.SymbolRecord {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]store.SymbolRecord, len(symbols))
	copy(out, symbols)

	byFile := make(map[string][]int)
	for i, sym := range out {
		byFile[sym.FilePath] = append(byFile[sym.FilePath], i)
	}

	for path, indices := range byFile {
		sort.Slice(indices, func(i, j int) bool {
			return out[indices[i]].StartLine < out[indices[j]].StartLine
		})
		eof := lineCounts[path]
		for i, idx := range indices {
			sym := &out[idx]
			if sym.EndLine >= sym.StartLine {
				continue
			}
			if i+1 < len(indices) {
				sym.EndLine = out[indices[i+1]].StartLine - 1
			} else if eof > 0 {
				sym.EndLine = eof
			} else {
				sym.EndLine = sym.StartLine
			}
			if sym.EndLine < sym.StartLine {
				sym.EndLine = sym.StartLine
			}
		}
	}
	return out
}

func countFileLines(absPath string) (int, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow long lines in source files.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func lineCountsForSymbols(repoRoot string, symbols []store.SymbolRecord) (map[string]int, error) {
	need := make(map[string]struct{})
	for _, sym := range symbols {
		if sym.EndLine >= sym.StartLine {
			continue
		}
		need[sym.FilePath] = struct{}{}
	}

	counts := make(map[string]int, len(need))
	for rel := range need {
		n, err := countFileLines(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("count lines %s: %w", rel, err)
		}
		counts[rel] = n
	}
	return counts, nil
}
