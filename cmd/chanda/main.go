// Command chanda is the CLI and MCP server entry point. Every command lives
// in internal/cli; see its package documentation for the usage.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/felipemarts/chanda/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
