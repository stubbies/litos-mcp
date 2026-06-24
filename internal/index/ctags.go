package index

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/stubbies/litos-mcp/internal/store"
)

// CtagsAvailable reports whether universal-ctags or ctags is on PATH.
func CtagsAvailable() bool {
	return CtagsCommand() != ""
}

// CtagsCommand returns the first ctags binary that supports JSON output, or empty string.
func CtagsCommand() string {
	for _, name := range []string{"universal-ctags", "ctags"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if ctagsSupportsJSON(path) {
			return name
		}
	}
	return ""
}

func ctagsSupportsJSON(commandPath string) bool {
	out, err := exec.Command(commandPath, "--list-features").Output()
	if err == nil && bytes.Contains(out, []byte("json")) {
		return true
	}
	return exec.Command(commandPath, "--output-format=json", "--version").Run() == nil
}

// CtagsExtractor runs universal-ctags in batched subprocess invocations.
type CtagsExtractor struct {
	command string
}

// NewCtagsExtractor creates an extractor using the given ctags binary name.
func NewCtagsExtractor(command string) *CtagsExtractor {
	return &CtagsExtractor{command: command}
}

func (e *CtagsExtractor) Name() string {
	return "ctags"
}

func (e *CtagsExtractor) Extract(ctx context.Context, repoRoot string, paths []string) ([]store.SymbolRecord, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("ctags: abs root: %w", err)
	}

	var all []store.SymbolRecord
	for _, batch := range batchPaths(paths) {
		symbols, err := e.runBatch(ctx, root, batch)
		if err != nil {
			return nil, err
		}
		all = append(all, symbols...)
	}

	if len(all) == 0 {
		return nil, nil
	}

	lineCounts, err := lineCountsForSymbols(root, all)
	if err != nil {
		return nil, err
	}
	return deriveEndLines(all, lineCounts), nil
}

func (e *CtagsExtractor) runBatch(ctx context.Context, repoRoot string, paths []string) ([]store.SymbolRecord, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	args := []string{
		"--fields=+nK+S+e",
		"--output-format=json",
		"-f", "-",
	}
	args = append(args, paths...)

	cmd := exec.CommandContext(ctx, e.command, args...)
	cmd.Dir = repoRoot

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ctags: %w: %s", err, bytes.TrimSpace(exitErr.Stderr))
		}
		return nil, fmt.Errorf("ctags: %w", err)
	}

	return parseCtagsJSON(repoRoot, out)
}

func parseCtagsJSON(repoRoot string, data []byte) ([]store.SymbolRecord, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var symbols []store.SymbolRecord
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("ctags: parse json line: %w", err)
		}

		tagType, _ := rawString(raw, "_type")
		if tagType == "ptag" {
			continue
		}

		name, ok := rawString(raw, "name")
		if !ok || name == "" {
			continue
		}

		tagPath, ok := rawString(raw, "path")
		if !ok || tagPath == "" {
			continue
		}

		relPath, err := normalizeTagPath(repoRoot, tagPath)
		if err != nil {
			continue
		}

		startLine := rawInt(raw, "line")
		if startLine <= 0 {
			startLine = rawInt(raw, "n")
		}
		if startLine <= 0 {
			continue
		}

		kind := rawStringDefault(raw, "kind", rawStringDefault(raw, "K", ""))
		endLine := rawInt(raw, "end")
		if endLine <= 0 {
			endLine = rawInt(raw, "e")
		}
		scope := rawStringDefault(raw, "scope", rawStringDefault(raw, "S", ""))

		symbols = append(symbols, store.SymbolRecord{
			Name:      name,
			FilePath:  relPath,
			Kind:      kind,
			Scope:     scope,
			StartLine: startLine,
			EndLine:   endLine,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ctags: read output: %w", err)
	}
	return symbols, nil
}

func rawString(raw map[string]json.RawMessage, key string) (string, bool) {
	v, ok := raw[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

func rawStringDefault(raw map[string]json.RawMessage, key, fallback string) string {
	s, ok := rawString(raw, key)
	if !ok || s == "" {
		return fallback
	}
	return s
}

func rawInt(raw map[string]json.RawMessage, key string) int {
	v, ok := raw[key]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(v, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
