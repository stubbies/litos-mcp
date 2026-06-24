package cli

import (
	"fmt"
	"os"
)

const usage = `litos-mcp — LLM context optimizer (MCP server)

Usage:
  litos-mcp init [--root PATH]   Build or refresh the local symbol index
  litos-mcp serve                Run MCP stdio server (blocks until disconnect)
  litos-mcp version              Print build and runtime information

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
	case "serve":
		return runServe(args[1:])
	case "version":
		return runVersion(args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}
