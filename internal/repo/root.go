package repo

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveRoot returns the absolute, EvalSymlinks-resolved repo root.
// Search order: flagRoot → walk up from start for .git → start directory.
// When flagRoot is empty, start is cwd.
func ResolveRoot(cwd string, flagRoot string) (string, error) {
	start := cwd
	if flagRoot != "" {
		start = flagRoot
	}

	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}

	root, err := findGitRoot(abs)
	if err != nil {
		return "", err
	}
	if root != "" {
		return evalSymlinks(root)
	}

	return evalSymlinks(abs)
}

// Root resolves the repo root for serve, MCP tool handlers, and indexers.
// Start directory precedence: CLAUDE_PROJECT_DIR (Claude Code) → os.Getwd()
// (Cursor cwd, manual CLI). The start path is then passed to ResolveRoot.
func Root() (string, error) {
	start, err := resolveStartDir()
	if err != nil {
		return "", err
	}
	return ResolveRoot(start, "")
}

func resolveStartDir() (string, error) {
	if dir := os.Getenv("CLAUDE_PROJECT_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return cwd, nil
}

func findGitRoot(start string) (string, error) {
	dir := start
	for {
		if hasGitMetadata(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func hasGitMetadata(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	// .git may be a directory (normal repo) or a file (worktree/submodule).
	return info.Mode().IsRegular() || info.IsDir()
}

func evalSymlinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %q: %w", path, err)
	}
	return resolved, nil
}
