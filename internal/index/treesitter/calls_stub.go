//go:build !treesitter

package treesitter

import "github.com/stubbies/litos-mcp/internal/store"

// CallSitesEnabled reports whether tree-sitter call extraction is compiled in.
func CallSitesEnabled() bool { return false }

// ExtractCallSites is unavailable without the treesitter build tag.
func ExtractCallSites(_ string, _ []string) ([]store.CallSiteRecord, error) {
	return nil, nil
}

// CallSiteExtensions returns extensions with tree-sitter call extraction support.
func CallSiteExtensions() []string { return nil }
