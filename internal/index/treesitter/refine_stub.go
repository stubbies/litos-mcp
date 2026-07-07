//go:build !treesitter

package treesitter

import "github.com/stubbies/litos-mcp/internal/store"

// Enabled reports whether tree-sitter boundary refinement is compiled in.
func Enabled() bool { return false }

// RefineBoundaries returns symbols unchanged when tree-sitter is not compiled in.
func RefineBoundaries(_ string, symbols []store.SymbolRecord) ([]store.SymbolRecord, error) {
	return symbols, nil
}
