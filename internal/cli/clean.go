package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stubbies/litos-mcp/internal/store"
)

func runClean(args []string) error {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	reindex := fs.Bool("reindex", false, "after deleting cache, run full init rebuild")
	if err := fs.Parse(args); err != nil {
		return err
	}

	repoRoot, err := resolveRepoRoot(*rootFlag)
	if err != nil {
		return err
	}

	removed, err := store.RemoveCache(repoRoot)
	if err != nil {
		return fmt.Errorf("%w (stop litos-mcp serve if running)", err)
	}

	for _, path := range removed {
		fmt.Fprintf(os.Stderr, "removed %s\n", filepath.Base(path))
	}
	switch {
	case len(removed) == 0 && *reindex:
		fmt.Fprintln(os.Stderr, "cache absent; rebuilding")
	case len(removed) == 0:
		fmt.Fprintln(os.Stderr, "cache already absent")
	default:
		fmt.Fprintln(os.Stderr, "stop litos-mcp serve before cleaning if the server is running")
	}

	if *reindex {
		return initAt(repoRoot)
	}
	return nil
}
