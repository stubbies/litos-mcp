package main

import (
	"fmt"
	"os"

	"github.com/stubbies/litos-mcp/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "litos-mcp: %v\n", err)
		os.Exit(1)
	}
}
