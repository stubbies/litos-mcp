package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/repo"
	"github.com/stubbies/litos-mcp/internal/store"
)

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	repoRoot, err := repo.ResolveRoot(cwd, *rootFlag)
	if err != nil {
		return err
	}

	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()

	ext := index.NewExtractor()
	result, err := index.Reindex(context.Background(), repoRoot, st, ext)
	if err != nil {
		return err
	}

	fmt.Printf("files=%d symbols=%d indexer=%s elapsed_ms=%d db_bytes=%d",
		result.FilesIndexed,
		result.SymbolsIndexed,
		result.Indexer,
		result.Elapsed.Milliseconds(),
		result.DBBytes,
	)
	if index.BoundaryIndexer() == "treesitter" {
		fmt.Print(" boundary=treesitter")
	}
	fmt.Println()
	return nil
}
