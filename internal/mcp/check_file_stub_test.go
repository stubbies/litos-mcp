//go:build !treesitter

package mcp_test

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCP_CheckFileUnavailableWithoutTreesitter(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "check_file",
		Arguments: map[string]any{
			"file_path": "main.go",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error without treesitter build")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "syntax check requires tree-sitter build") {
		t.Fatalf("error text = %q, want syntax check requires tree-sitter build", text)
	}
}
