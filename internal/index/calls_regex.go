package index

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/stubbies/litos-mcp/internal/store"
)

var callSitePattern = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)

func extractCallSitesRegex(ctx context.Context, repoRoot string, paths []string) ([]store.CallSiteRecord, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("calls regex: abs root: %w", err)
	}

	var all []store.CallSiteRecord
	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		rel = filepath.ToSlash(rel)
		if !regexSupportedPath(rel) {
			continue
		}

		absPath := filepath.Join(root, filepath.FromSlash(rel))
		calls, err := extractCallSitesFromFile(absPath, rel)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("calls regex: %s: %w", rel, err)
		}
		all = append(all, calls...)
	}
	return all, nil
}

func extractCallSitesFromFile(absPath, relPath string) ([]store.CallSiteRecord, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var calls []store.CallSiteRecord
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		for _, loc := range callSitePattern.FindAllStringSubmatchIndex(line, -1) {
			if len(loc) < 4 {
				continue
			}
			name := line[loc[2]:loc[3]]
			if name == "" {
				continue
			}
			calls = append(calls, store.CallSiteRecord{
				CalleeName: name,
				FilePath:   relPath,
				Line:       lineNum,
				Col:        loc[2] + 1, // 1-based column
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return calls, nil
}

func groupCallSitesByFile(calls []store.CallSiteRecord) map[string][]store.CallSiteRecord {
	out := make(map[string][]store.CallSiteRecord)
	for _, call := range calls {
		out[call.FilePath] = append(out[call.FilePath], store.CallSiteRecord{
			CalleeName: call.CalleeName,
			FilePath:   call.FilePath,
			Line:       call.Line,
			Col:        call.Col,
		})
	}
	return out
}
