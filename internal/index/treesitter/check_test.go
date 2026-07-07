//go:build treesitter

package treesitter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbies/litos-mcp/internal/index/treesitter"
)

func TestCheckSyntax_ValidGo(t *testing.T) {
	dir := t.TempDir()
	rel := "valid.go"
	src := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	errs, err := treesitter.CheckSyntax(dir, rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
}

func TestCheckSyntax_BrokenGo(t *testing.T) {
	dir := t.TempDir()
	rel := "broken.go"
	src := "package main\n\nfunc broken() {\n\tx :=\n}\n"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	errs, err := treesitter.CheckSyntax(dir, rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("expected syntax errors for broken Go file")
	}
	if errs[0].Line <= 0 || errs[0].Col <= 0 {
		t.Fatalf("expected positive line/col, got %+v", errs[0])
	}
	if errs[0].Message == "" {
		t.Fatalf("expected non-empty message, got %+v", errs[0])
	}
}

func TestCheckSyntax_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	_, err := treesitter.CheckSyntax(dir, "readme.md")
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestCheckSyntax_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := treesitter.CheckSyntax(dir, "missing.go")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCheckSyntaxEnabled(t *testing.T) {
	if !treesitter.CheckSyntaxEnabled() {
		t.Fatal("expected CheckSyntaxEnabled() true with treesitter build tag")
	}
}
