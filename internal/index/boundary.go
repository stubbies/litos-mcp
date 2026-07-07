package index

import "github.com/stubbies/litos-mcp/internal/index/treesitter"

// BoundaryIndexer returns the active boundary refinement mode: "treesitter" or "none".
func BoundaryIndexer() string {
	if treesitter.Enabled() {
		return "treesitter"
	}
	return "none"
}

// BoundaryDescription returns a human-readable boundary mode for version output.
func BoundaryDescription() string {
	if treesitter.Enabled() {
		return "tree-sitter (go, ts, py)"
	}
	return "line-range only"
}

// CallersIndexer returns the active call-site extraction mode: "treesitter" or "regex".
func CallersIndexer() string {
	if treesitter.CallSitesEnabled() {
		return "treesitter"
	}
	return "regex"
}
