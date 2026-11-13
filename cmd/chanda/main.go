// Command chanda is the CLI and MCP server entry point.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("chanda", version)
		return
	}
	fmt.Fprintln(os.Stderr, "usage: chanda <serve|ingest|sleep|eval|forget|version>")
	os.Exit(2)
}
