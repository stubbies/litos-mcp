package cli

import (
	"database/sql"
	"fmt"
	"os"
	"runtime"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/store"
	"github.com/stubbies/litos-mcp/internal/version"

	_ "modernc.org/sqlite"
)

func runVersion(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("version: unexpected arguments: %v", args)
	}

	fts5OK := checkFTS5()
	ctags := index.CtagsCommand()

	fmt.Printf("litos-mcp %s\n", version.Version)
	fmt.Printf("go %s\n", runtime.Version())
	if ctags != "" {
		fmt.Printf("indexer: ctags available (%s)\n", ctags)
	} else {
		fmt.Println("indexer: ctags not found (regex fallback will be used)")
	}
	if fts5OK {
		fmt.Println("sqlite: FTS5 ok")
	} else {
		fmt.Println("sqlite: FTS5 unavailable")
		os.Exit(1)
	}
	fmt.Printf("boundary: %s\n", index.BoundaryDescription())
	fmt.Printf("callers: %s\n", index.CallersIndexer())
	return nil
}

func checkFTS5() bool {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return false
	}
	defer db.Close()

	return store.ProbeFTS5(db) == nil
}
