package read

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// MaxLineSpan is the maximum inclusive line range allowed per read.
	MaxLineSpan = 500
	// MaxResponseBytes caps total bytes returned from ReadLines.
	MaxResponseBytes = 512 * 1024
)

var (
	ErrPathOutsideRoot = errors.New("path outside repository root")
	ErrFileNotFound    = errors.New("file not found")
	ErrInvalidRange    = errors.New("invalid line range")
	ErrSpanTooLarge    = fmt.Errorf("line span exceeds maximum of %d lines", MaxLineSpan)
	ErrResponseTooLarge = errors.New("response exceeds maximum size")
	ErrNotAFile        = errors.New("path is not a regular file")
)

// Reader reads bounded line ranges from files under a resolved repo root.
type Reader struct {
	root string
}

// New creates a Reader with EvalSymlinks-resolved repoRoot.
func New(repoRoot string) (*Reader, error) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}
	return &Reader{root: root}, nil
}

// Root returns the EvalSymlinks-resolved repository root.
func (r *Reader) Root() string {
	return r.root
}

// ReadLines returns a plain-text slice of filePath from startLine through endLine
// (1-indexed, inclusive). Each line is prefixed with its line number and a tab.
func (r *Reader) ReadLines(filePath string, startLine, endLine int) (string, error) {
	if err := validateRange(startLine, endLine); err != nil {
		return "", err
	}

	target, err := r.resolvePath(filePath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrFileNotFound
		}
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", ErrNotAFile
	}

	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrFileNotFound
		}
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	return readLineRange(f, startLine, endLine)
}

// ReadLines is a convenience wrapper around New and Reader.ReadLines.
func ReadLines(repoRoot, filePath string, startLine, endLine int) (string, error) {
	r, err := New(repoRoot)
	if err != nil {
		return "", err
	}
	return r.ReadLines(filePath, startLine, endLine)
}

func validateRange(startLine, endLine int) error {
	if startLine < 1 || endLine < 1 {
		return ErrInvalidRange
	}
	if startLine > endLine {
		return ErrInvalidRange
	}
	if endLine-startLine+1 > MaxLineSpan {
		return ErrSpanTooLarge
	}
	return nil
}

func (r *Reader) resolvePath(filePath string) (string, error) {
	candidate := filepath.Join(r.root, filepath.Clean("/"+filePath))

	if _, err := os.Lstat(candidate); err != nil {
		if os.IsNotExist(err) {
			return "", ErrFileNotFound
		}
		return "", fmt.Errorf("lstat file: %w", err)
	}

	target, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrFileNotFound
		}
		return "", fmt.Errorf("resolve file symlinks: %w", err)
	}

	rel, err := filepath.Rel(r.root, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrPathOutsideRoot
	}

	return target, nil
}

func readLineRange(f *os.File, startLine, endLine int) (string, error) {
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var out strings.Builder
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < startLine {
			continue
		}
		if lineNum > endLine {
			break
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		fmt.Fprintf(&out, "%d\t%s", lineNum, scanner.Text())
		if out.Len() > MaxResponseBytes {
			return "", ErrResponseTooLarge
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	return out.String(), nil
}
