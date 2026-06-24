package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	litosmcp "github.com/stubbies/litos-mcp/internal/mcp"
	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
)

func setupTestServer(t *testing.T) (context.Context, *mcpsdk.ClientSession, func()) {
	t.Helper()

	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc ProcessPayment() {}\n\nfunc helper() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Reindex(context.Background(), root, st, index.NewRegexExtractor()); err != nil {
		t.Fatal(err)
	}

	reader, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	coord := index.NewSyncCoordinator(root, st, index.NewRegexExtractor())

	ctx := context.Background()
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	server := litosmcp.NewServer(litosmcp.Config{
		RepoRoot:    root,
		Store:       st,
		Reader:      reader,
		Version:     "test",
		Coordinator: coord,
	})

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		clientSession.Close()
		serverSession.Close()
		serverSession.Wait()
		st.Close()
	}
	return ctx, clientSession, cleanup
}

func writeGoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func callToolText(t *testing.T, session *mcpsdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool %s returned tool error: %v", name, res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("CallTool %s: expected 1 content item, got %d", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("CallTool %s: expected text content, got %T", name, res.Content[0])
	}
	return text.Text
}

func TestMCP_SearchSchema(t *testing.T) {
	ctx, session, cleanup := setupTestServer(t)
	defer cleanup()

	_ = ctx
	text := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query": "ProcessPayment",
	})

	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatalf("search response is not JSON array: %v\nbody: %s", err, text)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}

	required := []string{"file_path", "symbol", "kind", "start_line", "end_line", "scope", "matched_in"}
	for _, key := range required {
		if _, ok := hits[0][key]; !ok {
			t.Fatalf("hit missing key %q: %#v", key, hits[0])
		}
	}
	if hits[0]["symbol"] != "ProcessPayment" {
		t.Fatalf("symbol = %v, want ProcessPayment", hits[0]["symbol"])
	}
}

func TestMCP_SearchLimit(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	text := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query": "func",
		"limit": 1,
	})

	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) > 1 {
		t.Fatalf("len(hits) = %d, want <= 1", len(hits))
	}
}

func TestMCP_ReadLineFormat(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	text := callToolText(t, session, "read_file_lines", map[string]any{
		"file_path":  "main.go",
		"start_line": 1,
		"end_line":   3,
	})

	lineRe := regexp.MustCompile(`^\d+\t`)
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if !lineRe.MatchString(line) {
			t.Fatalf("line missing number prefix: %q", line)
		}
	}
}

func TestMCP_PromptCodeDiscoveryWorkflow(t *testing.T) {
	ctx, session, cleanup := setupTestServer(t)
	defer cleanup()

	var found bool
	for p, err := range session.Prompts(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		if p.Name == "code_discovery_workflow" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("code_discovery_workflow prompt not listed")
	}

	res, err := session.GetPrompt(ctx, &mcpsdk.GetPromptParams{
		Name: "code_discovery_workflow",
		Arguments: map[string]string{
			"task": "find payment handler",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(res.Messages))
	}
	text, ok := res.Messages[0].Content.(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", res.Messages[0].Content)
	}
	if !strings.Contains(text.Text, "Discovery task: find payment handler") {
		t.Fatalf("prompt missing task prefix: %q", text.Text)
	}
	if !strings.Contains(text.Text, "search_code_skeleton") {
		t.Fatalf("prompt missing search guidance: %q", text.Text)
	}
}

func TestMCP_ReadFileNotFound(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "read_file_lines",
		Arguments: map[string]any{
			"file_path":  "missing.go",
			"start_line": 1,
			"end_line":   1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for missing file")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "file not found") {
		t.Fatalf("error text = %q, want file not found", text)
	}
}

func TestMCP_ReindexIndex(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	text := callToolText(t, session, "reindex_index", nil)

	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("reindex response is not JSON object: %v\nbody: %s", err, text)
	}

	for _, key := range []string{"files", "symbols", "indexer", "elapsed_ms"} {
		if _, ok := result[key]; !ok {
			t.Fatalf("reindex result missing key %q: %#v", key, result)
		}
	}
	if result["files"].(float64) < 1 {
		t.Fatalf("files = %v, want >= 1", result["files"])
	}
	if result["symbols"].(float64) < 1 {
		t.Fatalf("symbols = %v, want >= 1", result["symbols"])
	}
	if result["indexer"] != "regex" {
		t.Fatalf("indexer = %v, want regex", result["indexer"])
	}
}

func TestMCP_IndexStatusResource(t *testing.T) {
	ctx, session, cleanup := setupTestServer(t)
	defer cleanup()

	var found bool
	for r, err := range session.Resources(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		if r.URI == "litos://index/status" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("litos://index/status resource not listed")
	}

	res, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{
		URI: "litos://index/status",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(res.Contents))
	}
	text := res.Contents[0].Text

	var status map[string]any
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		t.Fatalf("status response is not JSON object: %v\nbody: %s", err, text)
	}

	for _, key := range []string{"reconcile_needed", "files", "symbols", "indexer"} {
		if _, ok := status[key]; !ok {
			t.Fatalf("status missing key %q: %#v", key, status)
		}
	}
	if status["files"].(float64) < 1 {
		t.Fatalf("files = %v, want >= 1", status["files"])
	}
	if status["symbols"].(float64) < 1 {
		t.Fatalf("symbols = %v, want >= 1", status["symbols"])
	}
}
