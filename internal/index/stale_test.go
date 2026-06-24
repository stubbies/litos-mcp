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

func TestNeedsReindex(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	needs, err := index.NeedsReindex(ctx, root, st)
	if err != nil {
		t.Fatal(err)
	}
	if !needs {
		t.Fatal("expected NeedsReindex true before first index")
	}

	if _, err := index.Reindex(ctx, root, st, index.NewRegexExtractor()); err != nil {
		t.Fatal(err)
	}

	needs, err = index.NeedsReindex(ctx, root, st)
	if err != nil {
		t.Fatal(err)
	}
	if needs {
		t.Fatal("expected NeedsReindex false after fresh index")
	}

	info, err := os.Stat(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	future := info.ModTime().Add(time.Second)
	if err := os.Chtimes(mainPath, future, future); err != nil {
		t.Fatal(err)
	}

	needs, err = index.NeedsReindex(ctx, root, st)
	if err != nil {
		t.Fatal(err)
	}
	if !needs {
		t.Fatal("expected NeedsReindex true after file touch")
	}
}
