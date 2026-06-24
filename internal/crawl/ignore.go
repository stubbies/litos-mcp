package crawl

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultSkipDirNames are path segments skipped at any depth during crawl.
var defaultSkipDirNames = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	"target":       {},
	"__pycache__":  {},
	".next":        {},
	"out":          {},
	"coverage":     {},
	".cache":       {},
}

// allowedExtensions is the set of source-like file extensions eligible for indexing.
var allowedExtensions = map[string]struct{}{
	".go":     {},
	".ts":     {},
	".tsx":    {},
	".js":     {},
	".jsx":    {},
	".mjs":    {},
	".cjs":    {},
	".py":     {},
	".pyw":    {},
	".rs":     {},
	".java":   {},
	".kt":     {},
	".kts":    {},
	".c":      {},
	".h":      {},
	".cpp":    {},
	".cc":     {},
	".cxx":    {},
	".hpp":    {},
	".cs":     {},
	".rb":     {},
	".php":    {},
	".swift":  {},
	".scala":  {},
	".sc":     {},
	".vue":    {},
	".svelte": {},
	".sh":     {},
	".bash":   {},
	".zsh":    {},
	".sql":    {},
	".proto":  {},
}

// skipExtensions are binary or media extensions always skipped even if they slip past gitignore.
var skipExtensions = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".ico": {}, ".bmp": {},
	".pdf": {}, ".zip": {}, ".gz": {}, ".tar": {}, ".bz2": {}, ".7z": {}, ".rar": {},
	".bin": {}, ".exe": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".a": {}, ".o": {}, ".obj": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".eot": {},
	".mp3": {}, ".mp4": {}, ".wav": {}, ".avi": {}, ".mov": {},
	".wasm": {}, ".pyc": {}, ".class": {}, ".jar": {},
}

type ignorePattern struct {
	negated bool
	dirOnly bool
	raw     string
}

// IgnoreRules applies default skip rules, extension filters, and gitignore patterns.
type IgnoreRules struct {
	root     string
	patterns []ignorePattern
	depth    []int
}

// NewIgnoreRules loads .git/info/exclude for repoRoot. Root and nested .gitignore
// files are applied via EnterDir during crawl.
func NewIgnoreRules(repoRoot string) (*IgnoreRules, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("ignore rules: abs root: %w", err)
	}

	rules := &IgnoreRules{root: root}

	if err := rules.loadExclude(); err != nil {
		return nil, err
	}

	return rules, nil
}

func (r *IgnoreRules) loadExclude() error {
	excludePath := filepath.Join(r.root, ".git", "info", "exclude")
	content, err := os.ReadFile(excludePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", excludePath, err)
	}
	r.appendPatterns(parseGitignore(content))
	return nil
}

func (r *IgnoreRules) appendGitignoreFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read gitignore %s: %w", path, err)
	}
	r.appendPatterns(parseGitignore(content))
	return nil
}

func (r *IgnoreRules) appendPatterns(patterns []ignorePattern) {
	r.patterns = append(r.patterns, patterns...)
}

// EnterDir loads a nested .gitignore when descending into absDir during walk.
func (r *IgnoreRules) EnterDir(absDir string) error {
	r.depth = append(r.depth, len(r.patterns))
	return r.appendGitignoreFile(filepath.Join(absDir, ".gitignore"))
}

// LeaveDir pops gitignore patterns loaded for the directory being exited.
func (r *IgnoreRules) LeaveDir() {
	if len(r.depth) == 0 {
		return
	}
	cut := r.depth[len(r.depth)-1]
	r.depth = r.depth[:len(r.depth)-1]
	if cut < len(r.patterns) {
		r.patterns = r.patterns[:cut]
	}
}

// SkipDir reports whether a directory should be skipped entirely during walk.
func (r *IgnoreRules) SkipDir(relPath string, _ string) bool {
	if IsDefaultSkippedDir(relPath) {
		return true
	}
	return r.ignored(relPath, true)
}

// SkipFile reports whether a file should be skipped during walk.
func (r *IgnoreRules) SkipFile(relPath string, _ string) bool {
	if r.ignored(relPath, false) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if _, skip := skipExtensions[ext]; skip {
		return true
	}
	return !IsAllowedExtension(ext)
}

// IsDefaultSkippedDir reports whether relPath contains a default skip directory segment.
func IsDefaultSkippedDir(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	name := filepath.Base(relPath)
	if name == ".lcn_cache.db" {
		return true
	}
	for _, seg := range strings.Split(relPath, "/") {
		if seg == "" {
			continue
		}
		if _, skip := defaultSkipDirNames[seg]; skip {
			return true
		}
	}
	return false
}

// IsAllowedExtension reports whether ext (including leading dot) is indexable source.
func IsAllowedExtension(ext string) bool {
	ext = strings.ToLower(ext)
	if ext == "" {
		return false
	}
	_, ok := allowedExtensions[ext]
	return ok
}

func (r *IgnoreRules) ignored(relPath string, isDir bool) bool {
	relPath = filepath.ToSlash(relPath)
	matched := false
	ignored := false
	for _, p := range r.patterns {
		if p.match(relPath, isDir) {
			matched = true
			ignored = !p.negated
		}
	}
	return matched && ignored
}

func parseGitignore(content []byte) []ignorePattern {
	var patterns []ignorePattern
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, `\#`) {
			line = strings.TrimPrefix(line, `\`)
		}

		p := ignorePattern{raw: line}
		if strings.HasPrefix(line, "!") {
			p.negated = true
			line = strings.TrimPrefix(line, "!")
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		p.raw = line
		if line == "" {
			continue
		}
		patterns = append(patterns, p)
	}
	return patterns
}

func (p ignorePattern) match(relPath string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}

	pat := p.raw
	if strings.HasPrefix(pat, "/") {
		pat = strings.TrimPrefix(pat, "/")
		return pathMatch(pat, relPath)
	}

	base := filepath.Base(relPath)
	if pathMatch(pat, base) {
		return true
	}

	parts := strings.Split(relPath, "/")
	for i := range parts {
		suffix := strings.Join(parts[i:], "/")
		if pathMatch(pat, suffix) {
			return true
		}
	}
	return false
}

func pathMatch(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	ok, err := filepath.Match(pattern, value)
	return err == nil && ok
}
