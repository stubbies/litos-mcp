package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/store"
)

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

func TestReindex_FullAndIncremental(t *testing.T) {
	root := t.TempDir()

	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n\nfunc helper() {}\n")
	writeGoFile(t, root, "pkg/service.go", "package pkg\n\ntype Service struct{}\n\nfunc (s *Service) Run() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	ctx := context.Background()

	result, err := index.Reindex(ctx, root, st, ext)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if result.FilesIndexed != 2 {
		t.Fatalf("FilesIndexed = %d, want 2", result.FilesIndexed)
	}
	if result.SymbolsIndexed < 3 {
		t.Fatalf("SymbolsIndexed = %d, want at least 3", result.SymbolsIndexed)
	}
	if result.Indexer != "regex" {
		t.Fatalf("Indexer = %q, want regex", result.Indexer)
	}

	hits, err := st.Search("helper", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("search helper: got %d hits", len(hits))
	}

	// Touch one file and reindex; only that file should be reprocessed.
	mainPath := filepath.Join(root, "main.go")
	info, err := os.Stat(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	future := info.ModTime().Add(time.Second)
	if err := os.Chtimes(mainPath, future, future); err != nil {
		t.Fatal(err)
	}

	beforeSymbols, err := st.SymbolCount()
	if err != nil {
		t.Fatal(err)
	}

	result2, err := index.Reindex(ctx, root, st, ext)
	if err != nil {
		t.Fatalf("incremental Reindex: %v", err)
	}
	if result2.FilesIndexed != 2 {
		t.Fatalf("FilesIndexed after incremental = %d, want 2", result2.FilesIndexed)
	}
	if result2.SymbolsIndexed != beforeSymbols {
		t.Fatalf("symbol count changed unexpectedly: before=%d after=%d", beforeSymbols, result2.SymbolsIndexed)
	}

	// Remove a file from disk; reindex should drop it from the index.
	if err := os.Remove(mainPath); err != nil {
		t.Fatal(err)
	}
	result3, err := index.Reindex(ctx, root, st, ext)
	if err != nil {
		t.Fatalf("Reindex after delete: %v", err)
	}
	if result3.FilesIndexed != 1 {
		t.Fatalf("FilesIndexed after delete = %d, want 1", result3.FilesIndexed)
	}
}

func TestReindex_RegexExcludesUnsupportedExtensions(t *testing.T) {
	root := t.TempDir()

	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeGoFile(t, root, "lib.rs", "fn helper() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	ctx := context.Background()

	result, err := index.Reindex(ctx, root, st, ext)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Fatalf("FilesIndexed = %d, want 1 (.rs excluded in regex mode)", result.FilesIndexed)
	}
	if result.Indexer != "regex" {
		t.Fatalf("Indexer = %q, want regex", result.Indexer)
	}

	indexed, err := st.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, meta := range indexed {
		if filepath.Ext(meta.Path) == ".rs" {
			t.Fatalf("indexed unsupported extension: %s", meta.Path)
		}
	}
}
