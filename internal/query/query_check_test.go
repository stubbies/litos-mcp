//go:build treesitter

package query_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbies/litos-mcp/internal/query"
	"github.com/stubbies/litos-mcp/internal/read"
)

func TestCheckFile_ValidGo(t *testing.T) {
	root := t.TempDir()
	rel := "valid.go"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := query.CheckFile(context.Background(), root, rel)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("ok = false, errors = %+v", result.Errors)
	}
	if result.FilePath != rel {
		t.Fatalf("file_path = %q, want %q", result.FilePath, rel)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %+v, want empty", result.Errors)
	}
}

func TestCheckFile_BrokenGo(t *testing.T) {
	root := t.TempDir()
	rel := "broken.go"
	src := "package main\n\nfunc broken() {\n\tx :=\n}\n"
	if err := os.WriteFile(filepath.Join(root, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := query.CheckFile(context.Background(), root, rel)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatal("expected ok=false for broken syntax")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected syntax errors")
	}
}

func TestCheckFile_MissingPath(t *testing.T) {
	root := t.TempDir()
	_, err := query.CheckFile(context.Background(), root, "")
	if err == nil {
		t.Fatal("expected error for missing file_path")
	}
}

func TestCheckFile_FixtureBilling(t *testing.T) {
	root := t.TempDir()
	rel := "billing.go"
	src := "package billing\n\nfunc ProcessPayment() error { return nil }\n"
	if err := os.WriteFile(filepath.Join(root, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	result, err := query.CheckFile(context.Background(), reader.Root(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("fixture billing.go should be valid, errors = %+v", result.Errors)
	}
}
