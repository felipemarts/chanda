package hugot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdio points os.Stdout and os.Stderr at temp files for the duration
// of f and returns what each received.
func captureStdio(t *testing.T, f func()) (stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	out, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errf, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errf
	func() {
		defer func() { os.Stdout, os.Stderr = savedOut, savedErr }()
		f()
	}()
	out.Close()
	errf.Close()
	o, _ := os.ReadFile(out.Name())
	e, _ := os.ReadFile(errf.Name())
	return string(o), string(e)
}

// MCP stdio safety: whatever hugot prints during load goes to stderr, and
// stdout is restored afterwards, also when the load fails or panics.
func TestStdoutToStderrDuringLoad(t *testing.T) {
	var outside *os.File
	stdout, stderr := captureStdio(t, func() {
		outside = os.Stdout
		err := stdoutToStderr(func() error {
			fmt.Printf("GoMLX: dynamic axes failed\n")
			return errors.New("load failed")
		})
		if err == nil || err.Error() != "load failed" {
			t.Errorf("error not passed through: %v", err)
		}
		func() {
			defer func() { _ = recover() }()
			_ = stdoutToStderr(func() error { panic("boom") })
		}()
		if os.Stdout != outside {
			t.Error("os.Stdout not restored")
		}
		fmt.Print("protocol frame")
	})
	if stdout != "protocol frame" {
		t.Fatalf("stdout = %q, want only the frame written after load", stdout)
	}
	if stderr != "GoMLX: dynamic axes failed\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}

// The real load: e5 prints "GoMLX: dynamic axes failed" on its first run,
// which happens in Open's warm-up call; nothing may reach stdout.
func TestOpenWritesNothingToStdout(t *testing.T) {
	dir := modelDir(t, "e5-small")
	var e *Embedder
	var err error
	stdout, stderr := captureStdio(t, func() {
		e, err = Open(context.Background(), dir)
		if err == nil {
			_, err = e.Embed(context.Background(), []string{"uma frase qualquer", strings.Repeat("palavra ", 300)})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if stdout != "" {
		t.Fatalf("model load wrote to stdout: %q", stdout)
	}
	t.Logf("stderr during load: %q", stderr)
}
