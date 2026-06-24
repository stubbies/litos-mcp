package cli

import (
	"context"
	"fmt"
	"os"

	litosmcp "github.com/stubbies/litos-mcp/internal/mcp"
	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/repo"
	"github.com/stubbies/litos-mcp/internal/store"
	"github.com/stubbies/litos-mcp/internal/version"
)

func runServe(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("serve: unexpected arguments: %v", args)
	}

	repoRoot, err := repo.Root()
	if err != nil {
		return err
	}

	if !store.Exists(repoRoot) {
		fmt.Fprintf(os.Stderr, "litos-mcp serve: cache missing, running init for %s\n", repoRoot)
		if err := runInit(nil); err != nil {
			return fmt.Errorf("auto-init: %w", err)
		}
	}

	st, err := store.Open(repoRoot)
	if err != nil {
		return err
	}
	defer st.Close()

	reader, err := read.New(repoRoot)
	if err != nil {
		return fmt.Errorf("create line reader: %w", err)
	}

	ext := index.NewExtractor()
	coord := index.NewSyncCoordinator(repoRoot, st, ext)

	ctx := context.Background()
	elapsed, err := coord.Hydrate(ctx)
	if err != nil {
		return fmt.Errorf("hydrate index: %w", err)
	}
	fmt.Fprintf(os.Stderr, "litos-mcp serve: hydrated index in %dms\n", elapsed.Milliseconds())

	return litosmcp.Run(ctx, litosmcp.Config{
		RepoRoot:    repoRoot,
		Store:       st,
		Reader:      reader,
		Version:     version.Version,
		Coordinator: coord,
	})
}
