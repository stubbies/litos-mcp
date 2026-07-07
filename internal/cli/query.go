package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/stubbies/litos-mcp/internal/query"
	"github.com/stubbies/litos-mcp/internal/store"
)

func runSearch(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("search: requires a query argument")
	}
	queryArg := args[0]

	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	limit := fs.Int("limit", 0, "maximum results (default 10)")
	matchMode := fs.String("match-mode", "", "multi-token FTS match mode: and or or")
	nameMatch := fs.String("name-match", "", "symbol name lookup: exact or contains")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	env, err := openRepo(*rootFlag)
	if err != nil {
		return err
	}
	defer env.close()

	hits, err := query.Search(context.Background(), env.store, env.coordinator, query.SearchOpts{
		Query:     queryArg,
		Limit:     *limit,
		MatchMode: *matchMode,
		NameMatch: *nameMatch,
	})
	if err != nil {
		return err
	}
	if hits == nil {
		hits = []store.SearchHit{}
	}

	if *jsonOut {
		return emitJSON(hits)
	}
	for _, h := range hits {
		fmt.Printf("%s\t%s\t%s\t%s\t%d\n", h.SymbolID, h.FilePath, h.Symbol, h.Kind, h.StartLine)
	}
	return nil
}

func runOutline(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("outline: requires a file path argument")
	}
	filePath := args[0]

	fs := flag.NewFlagSet("outline", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	env, err := openRepo(*rootFlag)
	if err != nil {
		return err
	}
	defer env.close()

	entries, err := query.Outline(context.Background(), env.store, env.coordinator, filePath)
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []store.OutlineEntry{}
	}

	if *jsonOut {
		return emitJSON(entries)
	}
	for _, e := range entries {
		fmt.Printf("%s\t%s\t%s\t%s\t%d-%d\n", e.SymbolID, e.FilePath, e.Symbol, e.Kind, e.StartLine, e.EndLine)
	}
	return nil
}

func runReadSymbol(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("read-symbol: requires a symbol_id argument")
	}
	symbolID := args[0]

	fs := flag.NewFlagSet("read-symbol", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	env, err := openRepo(*rootFlag)
	if err != nil {
		return err
	}
	defer env.close()

	text, err := query.ReadSymbol(context.Background(), env.store, env.reader, symbolID)
	if err != nil {
		return mapQuerySymbolError(symbolID, err)
	}

	if *jsonOut {
		return emitJSON(map[string]string{
			"symbol_id": symbolID,
			"text":      text,
		})
	}
	fmt.Print(text)
	return nil
}

func runFindCallers(args []string) error {
	name := ""
	flagArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		flagArgs = args[1:]
	}

	fs := flag.NewFlagSet("find-callers", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	dir := fs.String("dir", "", "repo-relative directory prefix filter")
	limit := fs.Int("limit", 0, "maximum caller hits (default 20)")
	symbolID := fs.String("symbol-id", "", "parse callee name from symbol_id when name is omitted")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if name == "" && *symbolID == "" {
		return fmt.Errorf("find-callers: requires name argument or --symbol-id")
	}

	env, err := openRepo(*rootFlag)
	if err != nil {
		return err
	}
	defer env.close()

	result, err := query.FindCallers(context.Background(), env.store, env.coordinator, query.FindCallersOpts{
		Name:     name,
		SymbolID: *symbolID,
		Dir:      *dir,
		Limit:    *limit,
	})
	if err != nil {
		if errors.Is(err, query.ErrNoCallers) {
			return fmt.Errorf("%s", query.NoCallersMessage(result.CalleeName))
		}
		if errors.Is(err, store.ErrInvalidSymbolID) {
			return fmt.Errorf("invalid symbol id: %w", err)
		}
		return err
	}
	if result.Hits == nil {
		result.Hits = []store.CallerHit{}
	}

	if *jsonOut {
		return emitJSON(result.Hits)
	}
	for _, h := range result.Hits {
		fmt.Printf("%s:%d:%d\t%s\t%s\n", h.FilePath, h.Line, h.Col, h.EnclosingSymbol, h.CalleeName)
	}
	return nil
}

func runMapDir(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("map-dir: requires a directory argument")
	}
	dirArg := args[0]

	fs := flag.NewFlagSet("map-dir", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	defLimit := fs.Int("def-limit", 0, "maximum symbol definitions (default 50)")
	callLimit := fs.Int("call-limit", 0, "maximum outgoing call entries (default 50)")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	env, err := openRepo(*rootFlag)
	if err != nil {
		return err
	}
	defer env.close()

	result, err := query.MapDirectory(context.Background(), env.store, env.coordinator, query.MapDirectoryOpts{
		Dir:       dirArg,
		DefLimit:  *defLimit,
		CallLimit: *callLimit,
	})
	if err != nil {
		return err
	}
	if result.Definitions == nil {
		result.Definitions = []store.OutlineEntry{}
	}
	if result.OutgoingCalls == nil {
		result.OutgoingCalls = []store.OutgoingCallEntry{}
	}

	if *jsonOut {
		return emitJSON(result)
	}

	fmt.Printf("dir=%s definitions=%d outgoing_calls=%d\n",
		result.Dir, result.DefinitionCount, result.OutgoingCallCount)
	for _, d := range result.Definitions {
		fmt.Printf("def\t%s\t%s\t%s\t%d\n", d.SymbolID, d.FilePath, d.Symbol, d.StartLine)
	}
	for _, c := range result.OutgoingCalls {
		fmt.Printf("call\t%s\t%s:%d\t%s\n", c.CalleeName, c.FilePath, c.Line, c.EnclosingSymbolID)
	}
	return nil
}

func runCheck(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("check: requires a file path argument")
	}
	filePath := args[0]

	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "repo root (default: git root or cwd)")
	jsonOut := fs.Bool("json", false, "emit JSON to stdout")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	env, err := openRepo(*rootFlag)
	if err != nil {
		return err
	}
	defer env.close()

	result, err := query.CheckFile(context.Background(), env.root, filePath)
	if err != nil {
		if errors.Is(err, query.ErrSyntaxCheckUnavailable) {
			return err
		}
		return mapCheckError(err)
	}

	if *jsonOut {
		return emitJSON(result)
	}
	if result.OK {
		fmt.Printf("ok\t%s\n", result.FilePath)
		return nil
	}
	for _, pe := range result.Errors {
		fmt.Printf("error\t%s:%d:%d\t%s\n", result.FilePath, pe.Line, pe.Col, pe.Message)
	}
	return fmt.Errorf("syntax errors in %s", result.FilePath)
}

func emitJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func mapQuerySymbolError(id string, err error) error {
	if errors.Is(err, store.ErrInvalidSymbolID) {
		return fmt.Errorf("invalid symbol id: %w", err)
	}
	if errors.Is(err, store.ErrSymbolNotFound) {
		return fmt.Errorf("symbol not found: %s (symbol may be stale after edits; re-search to get a fresh symbol_id)", id)
	}
	return err
}

func mapCheckError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "file not found"):
		return fmt.Errorf("file not found")
	case strings.Contains(msg, "unsupported file extension"):
		return fmt.Errorf("unsupported file extension for syntax check")
	default:
		return err
	}
}
