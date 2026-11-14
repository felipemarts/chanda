package hugot

import (
	"os"
	"sync"
)

var stdoutMu sync.Mutex

// stdoutToStderr runs f with os.Stdout pointing at os.Stderr. hugot and GoMLX
// print with fmt.Printf (for example "GoMLX: dynamic axes failed ..." on the
// first run of e5), and in MCP stdio mode stdout carries only protocol
// frames. Model load and the warm-up call run inside it; the MCP transport
// keeps the *os.File it captured when it connected, so it is unaffected.
func stdoutToStderr(f func() error) error {
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	saved := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = saved }()
	return f()
}
