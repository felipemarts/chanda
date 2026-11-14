//go:build !unix

package main

import (
	"fmt"
	"os"
)

// lockFile falls back to an exclusive-create lock file where flock is not
// available. A stale file after a crash must be removed by hand.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w (remove it if no lmeproxy is running)", path, err)
	}
	f.Close()
	return func() { os.Remove(path) }, nil
}
