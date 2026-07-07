//go:build treesitter

package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	litosmcp "github.com/stubbies/litos-mcp/internal/mcp"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
)

func setupCheckFileServer(t *testing.T, rel, content string) (*mcpsdk.ClientSession, func()) {
	t.Helper()

	root := t.TempDir()
	writeGoFile(t, root, rel, content)

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	server := litosmcp.NewServer(litosmcp.Config{
		RepoRoot: root,
		Store:    st,
		Reader:   reader,
		Version:  "test",
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
	return clientSession, cleanup
}

func TestMCP_CheckFileValid(t *testing.T) {
	session, cleanup := setupCheckFileServer(t, "main.go", "package main\n\nfunc main() {}\n")
	defer cleanup()

	text := callToolText(t, session, "check_file", map[string]any{
		"file_path": "main.go",
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("check_file response is not JSON object: %v\nbody: %s", err, text)
	}
	if result["file_path"] != "main.go" {
		t.Fatalf("file_path = %v, want main.go", result["file_path"])
	}
	if result["ok"] != true {
		t.Fatalf("ok = %v, want true; errors = %v", result["ok"], result["errors"])
	}
}

func TestMCP_CheckFileBroken(t *testing.T) {
	session, cleanup := setupCheckFileServer(t, "broken.go", "package main\n\nfunc broken() {\n\tx :=\n}\n")
	defer cleanup()

	text := callToolText(t, session, "check_file", map[string]any{
		"file_path": "broken.go",
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false {
		t.Fatalf("ok = %v, want false", result["ok"])
	}
	errs, ok := result["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors array, got %#v", result["errors"])
	}
}

func TestMCP_CheckFileUnsupportedExtension(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	server := litosmcp.NewServer(litosmcp.Config{
		RepoRoot: root,
		Store:    st,
		Reader:   reader,
		Version:  "test",
	})
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		session.Close()
		serverSession.Close()
		serverSession.Wait()
		st.Close()
	}()

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "check_file",
		Arguments: map[string]any{
			"file_path": "readme.md",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for unsupported extension")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "unsupported file extension") {
		t.Fatalf("error text = %q, want unsupported file extension", text)
	}
}

func TestMCP_CheckFileNotFound(t *testing.T) {
	session, cleanup := setupCheckFileServer(t, "main.go", "package main\n")
	defer cleanup()

	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "check_file",
		Arguments: map[string]any{
			"file_path": "missing.go",
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
