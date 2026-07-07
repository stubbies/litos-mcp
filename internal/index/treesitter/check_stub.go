//go:build !treesitter

package treesitter

import "errors"

// ErrSyntaxCheckUnavailable indicates syntax check was not compiled in.
var ErrSyntaxCheckUnavailable = errors.New("syntax check requires tree-sitter build")

// ParseError describes a syntax error location in a source file.
type ParseError struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// CheckSyntaxEnabled reports whether tree-sitter syntax checking is compiled in.
func CheckSyntaxEnabled() bool { return false }

// CheckSyntax is unavailable without the treesitter build tag.
func CheckSyntax(_ string, _ string) ([]ParseError, error) {
	return nil, ErrSyntaxCheckUnavailable
}
