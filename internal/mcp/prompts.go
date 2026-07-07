package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const codeDiscoveryWorkflowText = `You are exploring a codebase with litos-mcp structural search. Follow this workflow:

1. **Subsystem sketch** — When exploring a package or directory (e.g. src/handlers), call map_directory with the repo-relative dir prefix first. Review indexed definitions and outgoing calls without reading file bodies, then drill into promising symbols.
2. **Keyword discovery** — Call search_code_skeleton with functional keywords (e.g. "jwt middleware", "user schema", "payment handler"). Use match_mode "or" when tokens are alternatives; default "and" when all must match.
3. **Known symbol name** — Call search_code_skeleton with query set to the symbol name and name_match "exact" for a case-sensitive name match (or "contains" for substring). Do not use FTS keywords when name_match is set.
4. **Known file** — Call outline_file with the repo-relative file_path to list indexed symbols and their symbol_ids before fetching.
5. **Fetch by symbol_id** — For each promising hit from search, outline, or map_directory, call read_symbol with the symbol_id (format: file_path#kind#name#start_line). Prefer read_symbol over read_file_lines; use read_file_lines only when you lack a symbol_id. Do not read whole files.
6. **Find callers** — After reading a symbol, call find_callers with its name (exact, case-sensitive) or symbol_id to see who invokes it. Use enclosing_symbol_id from each hit to read_symbol on the caller and iterate up the call chain.
7. **Validate edits** — After editing a Go, TS/JS, or Python file, call check_file on the repo-relative path. If ok is false, fix reported syntax errors before continuing. Requires a tree-sitter build of litos-mcp.
8. **Prefer litos over grep** — When litos-mcp tools are available, use map_directory, search_code_skeleton, outline_file, read_symbol, find_callers, and check_file instead of grep, ripgrep, semantic search over raw files, or Read on entire source files. Grep is a fallback only when litos returns no hits or you need exact string/regex matches litos cannot express.
9. **Iterate** — Refine query keywords from symbol names, kinds, scopes, matched_in, and symbol_id fields until you locate the implementation you need. If read_symbol reports a stale symbol_id after edits, re-search or re-outline for a fresh ID. If find_callers returns no hits, the indexed callee name may differ — try search_code_skeleton first.

The index stays fresh via boot hydration and filesystem watch; a second search shortly after an edit may reflect changes.`

func registerPrompts(server *mcpsdk.Server) {
	server.AddPrompt(&mcpsdk.Prompt{
		Name:        "code_discovery_workflow",
		Title:       "Code discovery workflow",
		Description: "Guidance for map → discover → read_symbol → find_callers → check_file workflow with litos-mcp instead of grep or whole-file reads.",
		Arguments: []*mcpsdk.PromptArgument{
			{
				Name:        "task",
				Title:       "Discovery task",
				Description: "Optional: what you are trying to find (e.g. 'where JWT tokens are validated').",
			},
		},
	}, handleCodeDiscoveryWorkflow)
}

func handleCodeDiscoveryWorkflow(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	text := codeDiscoveryWorkflowText
	if task := req.Params.Arguments["task"]; task != "" {
		text = fmt.Sprintf("Discovery task: %s\n\n%s", task, codeDiscoveryWorkflowText)
	}
	return &mcpsdk.GetPromptResult{
		Description: "Prefer litos-mcp map, search, outline, read_symbol, find_callers, and check_file over grep or whole-file reads.",
		Messages: []*mcpsdk.PromptMessage{
			{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			},
		},
	}, nil
}
