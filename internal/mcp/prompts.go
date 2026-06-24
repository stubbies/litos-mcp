package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const codeDiscoveryWorkflowText = `You are exploring a codebase with litos-mcp structural search. Follow this workflow:

1. **Search first** — Call search_code_skeleton with functional keywords (e.g. "jwt middleware", "user schema", "payment handler"). Use match_mode "or" when tokens are alternatives; default "and" when all must match.
2. **Read narrowly** — For each promising hit, call read_file_lines with the file_path and start_line/end_line from search results. Do not read whole files.
3. **Prefer litos over grep** — When litos-mcp tools are available, use search_code_skeleton and read_file_lines instead of grep, ripgrep, semantic search over raw files, or Read on entire source files. Grep is a fallback only when litos returns no hits or you need exact string/regex matches litos cannot express.
4. **Iterate** — Refine query keywords from symbol names, kinds, scopes, and matched_in fields in search hits until you locate the implementation you need.

The index stays fresh via boot hydration and filesystem watch; a second search shortly after an edit may reflect changes.`

func registerPrompts(server *mcpsdk.Server) {
	server.AddPrompt(&mcpsdk.Prompt{
		Name:        "code_discovery_workflow",
		Title:       "Code discovery workflow",
		Description: "Guidance for structural code search with litos-mcp instead of grep or whole-file reads.",
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
		Description: "Prefer litos-mcp search_code_skeleton and read_file_lines over grep or whole-file reads.",
		Messages: []*mcpsdk.PromptMessage{
			{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			},
		},
	}, nil
}
