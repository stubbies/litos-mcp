package cli

import (
	"fmt"
	"os"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/repo"
	"github.com/stubbies/litos-mcp/internal/store"
)

type repoEnv struct {
	root        string
	store       *store.Store
	reader      *read.Reader
	coordinator *index.SyncCoordinator
}

func (e *repoEnv) close() {
	if e.store != nil {
		e.store.Close()
	}
}

func resolveRepoRoot(rootFlag string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return repo.ResolveRoot(cwd, rootFlag)
}

func openRepo(rootFlag string) (*repoEnv, error) {
	repoRoot, err := resolveRepoRoot(rootFlag)
	if err != nil {
		return nil, err
	}
	return openRepoAt(repoRoot)
}

func openRepoAt(repoRoot string) (*repoEnv, error) {
	if !store.Exists(repoRoot) {
		return nil, fmt.Errorf("index cache missing; run: litos-mcp init")
	}

	st, err := store.Open(repoRoot)
	if err != nil {
		return nil, err
	}

	reader, err := read.New(repoRoot)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("create line reader: %w", err)
	}

	ext := index.NewExtractor()
	coord := index.NewSyncCoordinator(repoRoot, st, ext)

	return &repoEnv{
		root:        repoRoot,
		store:       st,
		reader:      reader,
		coordinator: coord,
	}, nil
}
