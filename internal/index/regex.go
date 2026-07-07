package index

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stubbies/litos-mcp/internal/store"
)

var regexExtSet map[string]struct{}

func init() {
	regexExtSet = make(map[string]struct{}, len(defaultLangPatterns()))
	for ext := range defaultLangPatterns() {
		regexExtSet[ext] = struct{}{}
	}
}

// RegexExtensions returns file extensions (including the dot) that the regex indexer can extract.
func RegexExtensions() []string {
	exts := make([]string, 0, len(regexExtSet))
	for ext := range regexExtSet {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

func regexSupportedPath(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	_, ok := regexExtSet[ext]
	return ok
}

type langPattern struct {
	kind    string
	pattern *regexp.Regexp
}

// RegexExtractor uses language-aware regex heuristics when ctags is unavailable.
type RegexExtractor struct {
	byExt map[string][]langPattern
}

// NewRegexExtractor creates the regex fallback extractor.
func NewRegexExtractor() *RegexExtractor {
	return &RegexExtractor{byExt: defaultLangPatterns()}
}

func (e *RegexExtractor) Name() string {
	return "regex"
}

func (e *RegexExtractor) Extract(ctx context.Context, repoRoot string, paths []string) ([]store.SymbolRecord, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("regex: abs root: %w", err)
	}

	var all []store.SymbolRecord
	lineCounts := make(map[string]int)

	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		rel = filepath.ToSlash(rel)
		patterns := e.patternsFor(rel)
		if len(patterns) == 0 {
			continue
		}

		absPath := filepath.Join(root, filepath.FromSlash(rel))
		symbols, lines, err := e.extractFile(absPath, rel, patterns)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("regex: %s: %w", rel, err)
		}
		if lines > 0 {
			lineCounts[rel] = lines
		}
		all = append(all, symbols...)
	}

	return deriveEndLines(all, lineCounts), nil
}

func (e *RegexExtractor) patternsFor(relPath string) []langPattern {
	ext := strings.ToLower(filepath.Ext(relPath))
	return e.byExt[ext]
}

func (e *RegexExtractor) extractFile(absPath, relPath string, patterns []langPattern) ([]store.SymbolRecord, int, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var symbols []store.SymbolRecord
	var prevComment string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			prevComment = strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			continue
		}
		for _, lp := range patterns {
			m := lp.pattern.FindStringSubmatch(line)
			if m == nil || len(m) < 2 {
				continue
			}
			name := m[1]
			if name == "" {
				continue
			}
			symbols = append(symbols, store.SymbolRecord{
				Name:      name,
				FilePath:  relPath,
				Kind:      lp.kind,
				Scope:     prevComment,
				StartLine: lineNum,
			})
			prevComment = ""
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return symbols, lineNum, nil
}

func defaultLangPatterns() map[string][]langPattern {
	goPatterns := []langPattern{
		{kind: "function", pattern: regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?(\w+)`)},
		{kind: "function", pattern: regexp.MustCompile(`^\s*func\s+\([^)]+\)\s*(\w+)`)},
		{kind: "type", pattern: regexp.MustCompile(`^\s*type\s+(\w+)\s+struct\b`)},
		{kind: "interface", pattern: regexp.MustCompile(`^\s*type\s+(\w+)\s+interface\b`)},
		{kind: "type", pattern: regexp.MustCompile(`^\s*type\s+(\w+)\s+[^(]`)},
	}

	tsPatterns := []langPattern{
		{kind: "function", pattern: regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)`)},
		{kind: "class", pattern: regexp.MustCompile(`^\s*(?:export\s+)?(?:abstract\s+)?class\s+(\w+)`)},
		{kind: "interface", pattern: regexp.MustCompile(`^\s*(?:export\s+)?interface\s+(\w+)`)},
		{kind: "type", pattern: regexp.MustCompile(`^\s*(?:export\s+)?type\s+(\w+)`)},
		{kind: "const", pattern: regexp.MustCompile(`^\s*export\s+const\s+(\w+)`)},
	}

	pyPatterns := []langPattern{
		{kind: "function", pattern: regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)`)},
		{kind: "class", pattern: regexp.MustCompile(`^\s*class\s+(\w+)`)},
	}

	return map[string][]langPattern{
		".go":  goPatterns,
		".ts":  tsPatterns,
		".tsx": tsPatterns,
		".js":  tsPatterns,
		".jsx": tsPatterns,
		".mjs": tsPatterns,
		".cjs": tsPatterns,
		".py":  pyPatterns,
		".pyw": pyPatterns,
	}
}
