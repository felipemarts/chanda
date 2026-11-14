package hugot

import (
	"runtime"
	"runtime/debug"
)

// recycleEvery is how many model calls a session serves before it is
// destroyed and loaded again (decision 0136). GoMLX's Go backend keeps the
// intermediate buffers of every call in pools that never shrink: measured on
// e5-small, about 0.45 MB of heap per call, which took a long-running
// embedder past 11 GB. Destroying the session frees the pools (heap back to
// about 40 MB). A reload measured 3.3 to 4.3 s on a busy darwin/arm64
// machine, against about 60 ms per e5 call: 1,000 calls keep the overhead
// near 6% and the growth near 450 MB (heap below about 1.6 GB).
// A fresh session produces bit-identical vectors (decision 0034), so the
// model id does not change.
var recycleEvery = 1000

// releaseMemory returns the freed pools to the operating system.
func releaseMemory() {
	runtime.GC()
	debug.FreeOSMemory()
}
