package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const codeDiscoveryWorkflowText = `You are exploring a codebase with litos-mcp structural search. Follow this workflow:

1. **Keyword discovery** — Call search_code_skeleton with functional keywords (e.g. "jwt middleware", "user schema", "payment handler"). Use match_mode "or" when tokens are alternatives; default "and" when all must match.
2. **Known symbol name** — Call search_code_skeleton with query set to the symbol name and name_match "exact" for a case-sensitive name match (or "contains" for substring). Do not use FTS keywords when name_match is set.
3. **Known file** — Call outline_file with the repo-relative file_path to list indexed symbols and their symbol_ids before fetching.
4. **Fetch by symbol_id** — For each promising hit from search or outline, call read_symbol with the symbol_id (format: file_path#kind#name#start_line). Prefer read_symbol over read_file_lines; use read_file_lines only when you lack a symbol_id. Do not read whole files.
5. **Prefer litos over grep** — When litos-mcp tools are available, use search_code_skeleton, outline_file, and read_symbol instead of grep, ripgrep, semantic search over raw files, or Read on entire source files. Grep is a fallback only when litos returns no hits or you need exact string/regex matches litos cannot express.
6. **Iterate** — Refine query keywords from symbol names, kinds, scopes, matched_in, and symbol_id fields until you locate the implementation you need. If read_symbol reports a stale symbol_id after edits, re-search or re-outline for a fresh ID.

The index stays fresh via boot hydration and filesystem watch; a second search shortly after an edit may reflect changes.`

func registerPrompts(server *mcpsdk.Server) {
	server.AddPrompt(&mcpsdk.Prompt{
		Name:        "code_discovery_workflow",
		Title:       "Code discovery workflow",
		Description: "Guidance for discover → read_symbol workflow with litos-mcp instead of grep or whole-file reads.",
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
		Description: "Prefer litos-mcp search, outline, and read_symbol over grep or whole-file reads.",
		Messages: []*mcpsdk.PromptMessage{
			{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			},
		},
	}, nil
}
