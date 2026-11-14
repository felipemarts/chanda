# 0036 hugot latencies (measured)

darwin/arm64, 8 cores, Go 1.27.0, `CGO_ENABLED=0`,
`go test -run '^$' -bench . -benchtime 5x` in `embedders/hugot`. The machine
was busy (load average 8 to 10 from parallel builds), so these are upper
bounds; the idle figures of `docs/research/embeddings.md` are about 25 to 40%
lower. Each text is one pipeline call (decision 0034).

| Case | e5-small stock | e5-small simd | MiniLM stock | MiniLM simd |
| --- | --- | --- | --- | --- |
| load (ONNX parse, graph, warm-up; no manifest) | 1.1 s | 1.5 s | 1.2 s | 1.5 s |
| 1 short sentence (about 20 tokens) | 41 ms | 16 ms | 50 ms | 19 ms |
| 1 paragraph (about 150 words; MiniLM cut at 128 tokens) | 350 ms | 63 ms | 202 ms | 50 ms |
| 32 short sentences, one call each | 1.53 s | 0.60 s | 1.42 s | 0.44 s |

| Reranker mmarco-minilm-l12 | stock | simd |
| --- | --- | --- |
| 1 pair | 126 ms | 42 ms |
| 30 pairs | 2.4 s | 0.63 s |

Other measurements: clipping a paragraph costs 0.25 ms (one extra
tokenisation); manifest verification adds about 0.3 s per 450 MB model at
load; the stock-build e5 cost grows linearly with tokens (3 tokens 15 ms, 34
tokens 68 ms, 202 tokens 371 ms). Peak RSS of `chanda-hugot embed` (e5,
simd) 1.51 GB, of `chanda-hugot rerank` 1.48 GB (`/usr/bin/time -l`). The simd build is 2.5 to 5.5 times faster
and should be the shipped flavour; its model ids end in `+simd`.
