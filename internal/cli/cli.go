package cli

import (
	"fmt"
	"os"
)

const usage = `litos-mcp — LLM context optimizer (MCP server)

Usage:
  litos-mcp init [--root PATH]              Build or refresh the local symbol index
  litos-mcp clean [--root PATH] [--reindex] Delete index cache (and optionally rebuild)
  litos-mcp serve                           Run MCP stdio server (blocks until disconnect)
  litos-mcp version                         Print build and runtime information
  litos-mcp search QUERY [--limit N] [--json]
  litos-mcp outline FILE [--json]
  litos-mcp read-symbol ID [--json]
  litos-mcp find-callers NAME [--dir D] [--limit N] [--json]
  litos-mcp map-dir DIR [--json]
  litos-mcp check FILE [--json]

Query subcommands accept [--root PATH] to override repo discovery.
Use --json for machine-readable stdout; status messages go to stderr.

`

// Run dispatches to the appropriate subcommand.
func Run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("missing subcommand")
	}

	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "clean":
		return runClean(args[1:])
	case "serve":
		return runServe(args[1:])
	case "version":
		return runVersion(args[1:])
	case "search":
		return runSearch(args[1:])
	case "outline":
		return runOutline(args[1:])
	case "read-symbol":
		return runReadSymbol(args[1:])
	case "find-callers":
		return runFindCallers(args[1:])
	case "map-dir":
		return runMapDir(args[1:])
	case "check":
		return runCheck(args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}
