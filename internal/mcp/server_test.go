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

	required := []string{"file_path", "symbol", "kind", "start_line", "end_line", "scope", "matched_in", "symbol_id"}
	for _, key := range required {
		if _, ok := hits[0][key]; !ok {
			t.Fatalf("hit missing key %q: %#v", key, hits[0])
		}
	}
	if hits[0]["symbol"] != "ProcessPayment" {
		t.Fatalf("symbol = %v, want ProcessPayment", hits[0]["symbol"])
	}
	if hits[0]["symbol_id"] != "main.go#function#ProcessPayment#3" {
		t.Fatalf("symbol_id = %v, want main.go#function#ProcessPayment#3", hits[0]["symbol_id"])
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

func TestMCP_OutlineFile(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	text := callToolText(t, session, "outline_file", map[string]any{
		"file_path": "main.go",
	})

	var entries []map[string]any
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("outline response is not JSON array: %v\nbody: %s", err, text)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(entries))
	}

	required := []string{"symbol_id", "file_path", "symbol", "kind", "start_line", "end_line", "scope"}
	for i, entry := range entries {
		for _, key := range required {
			if _, ok := entry[key]; !ok {
				t.Fatalf("entry[%d] missing key %q: %#v", i, key, entry)
			}
		}
		if _, ok := entry["matched_in"]; ok {
			t.Fatalf("entry[%d] should not include matched_in: %#v", i, entry)
		}
	}

	if entries[0]["symbol"] != "ProcessPayment" {
		t.Fatalf("first symbol = %v, want ProcessPayment", entries[0]["symbol"])
	}
	if entries[1]["symbol"] != "helper" {
		t.Fatalf("second symbol = %v, want helper", entries[1]["symbol"])
	}
	if entries[0]["symbol_id"] != "main.go#function#ProcessPayment#3" {
		t.Fatalf("symbol_id = %v, want main.go#function#ProcessPayment#3", entries[0]["symbol_id"])
	}
}

func TestMCP_OutlineFileUnindexed(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	text := callToolText(t, session, "outline_file", map[string]any{
		"file_path": "missing.go",
	})

	var entries []map[string]any
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("outline response is not JSON array: %v\nbody: %s", err, text)
	}
	if len(entries) != 0 {
		t.Fatalf("unindexed file should return empty array, got %d entries", len(entries))
	}
}

func TestMCP_OutlineFileRoundTrip(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	outlineText := callToolText(t, session, "outline_file", map[string]any{
		"file_path": "main.go",
	})
	var entries []map[string]any
	if err := json.Unmarshal([]byte(outlineText), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) < 1 {
		t.Fatal("expected at least one outline entry")
	}
	symbolID, ok := entries[0]["symbol_id"].(string)
	if !ok || symbolID == "" {
		t.Fatalf("missing symbol_id in outline entry: %#v", entries[0])
	}

	readText := callToolText(t, session, "read_symbol", map[string]any{
		"symbol_id": symbolID,
	})
	if !strings.Contains(readText, "func ProcessPayment()") {
		t.Fatalf("read_symbol body = %q, want ProcessPayment definition", readText)
	}
}

func TestMCP_ReadSymbolRoundTrip(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	searchText := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query": "ProcessPayment",
	})
	var hits []map[string]any
	if err := json.Unmarshal([]byte(searchText), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	symbolID, ok := hits[0]["symbol_id"].(string)
	if !ok || symbolID == "" {
		t.Fatalf("missing symbol_id in search hit: %#v", hits[0])
	}

	readText := callToolText(t, session, "read_symbol", map[string]any{
		"symbol_id": symbolID,
	})
	if !strings.Contains(readText, "func ProcessPayment()") {
		t.Fatalf("read_symbol body = %q, want ProcessPayment definition", readText)
	}

	lineText := callToolText(t, session, "read_file_lines", map[string]any{
		"file_path":  "main.go",
		"start_line": int(hits[0]["start_line"].(float64)),
		"end_line":   int(hits[0]["end_line"].(float64)),
	})
	if readText != lineText {
		t.Fatalf("read_symbol and read_file_lines differ:\nread_symbol:\n%s\nread_file_lines:\n%s", readText, lineText)
	}
}

func TestMCP_SearchNameMatchExact(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	text := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query":       "ProcessPayment",
		"name_match":  "exact",
	})
	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0]["symbol"] != "ProcessPayment" {
		t.Fatalf("symbol = %v, want ProcessPayment", hits[0]["symbol"])
	}
	if hits[0]["symbol_id"] != "main.go#function#ProcessPayment#3" {
		t.Fatalf("symbol_id = %v, want main.go#function#ProcessPayment#3", hits[0]["symbol_id"])
	}

	symbolID := hits[0]["symbol_id"].(string)
	readText := callToolText(t, session, "read_symbol", map[string]any{
		"symbol_id": symbolID,
	})
	if !strings.Contains(readText, "func ProcessPayment()") {
		t.Fatalf("read_symbol body = %q, want ProcessPayment definition", readText)
	}
}

func TestMCP_SearchNameMatchContains(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	text := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query":      "Process",
		"name_match": "contains",
	})
	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0]["symbol"] != "ProcessPayment" {
		t.Fatalf("symbol = %v, want ProcessPayment", hits[0]["symbol"])
	}
}

func TestMCP_SearchInvalidNameMatch(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "search_code_skeleton",
		Arguments: map[string]any{
			"query":      "ProcessPayment",
			"name_match": "exactly",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for invalid name_match")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "invalid name_match") {
		t.Fatalf("error text = %q, want invalid name_match", text)
	}
}

func TestMCP_ReadSymbolInvalidID(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "read_symbol",
		Arguments: map[string]any{
			"symbol_id": "bad-id",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for invalid symbol_id")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "invalid symbol id") {
		t.Fatalf("error text = %q, want invalid symbol id", text)
	}
}

func TestMCP_ReadSymbolStaleID(t *testing.T) {
	_, session, cleanup := setupTestServer(t)
	defer cleanup()

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "read_symbol",
		Arguments: map[string]any{
			"symbol_id": "main.go#function#Gone#99",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for stale symbol_id")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "symbol not found") || !strings.Contains(text, "re-search") {
		t.Fatalf("error text = %q, want symbol not found with re-search hint", text)
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

	for _, key := range []string{"reconcile_needed", "files", "symbols", "indexer", "boundary_indexer"} {
		if _, ok := status[key]; !ok {
			t.Fatalf("status missing key %q: %#v", key, status)
		}
	}
	if status["boundary_indexer"] != index.BoundaryIndexer() {
		t.Fatalf("boundary_indexer = %v, want %q", status["boundary_indexer"], index.BoundaryIndexer())
	}
	if status["files"].(float64) < 1 {
		t.Fatalf("files = %v, want >= 1", status["files"])
	}
	if status["symbols"].(float64) < 1 {
		t.Fatalf("symbols = %v, want >= 1", status["symbols"])
	}
}
