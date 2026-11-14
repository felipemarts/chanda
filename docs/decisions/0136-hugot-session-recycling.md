# 0136 Reload the hugot session every 1,000 model calls

Fixes `eval/deviations.md` 8 (the e5 embedder grows without bound under a
long load).

## Problem

GoMLX's pure-Go backend (`github.com/gomlx/compute` v0.1.28,
`internal/gobackend`) serves the intermediate buffers of each call from
per-size pools (`bufferPool`, a Treiber stack) that never shrink. Measured on
e5-small with texts of 5 to 300 words, after `runtime.GC()`:

| calls | heap in use | phys_footprint |
| --- | --- | --- |
| 200 | 1,146 MB | 1,309 MB |
| 600 | 1,321 MB | 2,441 MB |
| 1,000 | 1,503 MB | 2,805 MB |

About 0.45 MB of heap per call; the heap profile puts the growth in
`gobackend.(*Backend).GetBuffer`. The LongMemEval phase 1 embed server
reached an 11 GB footprint on an 8 GB machine and was killed (SIGKILL) about
every 40 minutes. A long-running `chanda serve` with e5 would do the same.
Bucketed padding (`WithGoMLXSequenceBuckets`) was considered and not used:
it changes the input shapes, so it could change the vectors.

## Decision

`Embedder` and `Reranker` count model calls (the warm-up included). Before a
call, once a session has served `recycleEvery` = 1,000 calls, the session is
destroyed, the freed memory is returned to the OS (`runtime.GC`,
`debug.FreeOSMemory`) and the session is loaded again with its warm-up.
Destroying the session frees the pools (heap from 1.1 GB to 41 MB, measured).

- Vectors and logits are bit-identical across reloads (a fresh session is
  what decision 0034 already pins): `TestEmbedderRecycleIsBitIdentical` and
  `TestRerankerRecycleIsBitIdentical` reload every 2 or 3 calls and compare
  with a session that never reloads. The model id does not change.
- Cost: a reload measured 3.3 to 4.3 s on a busy darwin/arm64 machine
  against about 60 ms per e5 call, so about 6% at 1,000 calls; the growth
  stays near 450 MB between reloads.
- A failed reload leaves the embedder closed: later calls fail with
  `unavailable`, as after `Close`.
