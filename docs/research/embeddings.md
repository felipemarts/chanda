# Research: embeddings and reranker in pure Go (hugot)

Measured 2026-11-13 on darwin/arm64, 8 cores, Go 1.27.0, `CGO_ENABLED=0`. Scratch project (code, patch, models):
`/private/tmp/claude-501/-Users-felipemarts-Documents-devel-chanda-mcp/ab6f89df-46c7-4bc9-b029-a0ae648888b5/scratchpad/embed`.
Reference for correctness: Python `onnxruntime` 1 CPU + HF `tokenizers` on the same ONNX files.

## Verdict

1. **hugot v0.8.6 runs sentence embeddings and a cross-encoder reranker in pure Go, with no cgo and no build tags**
   (`CGO_ENABLED=0 go build` is enough; binary about 27 MB). The backend is the GoMLX "simplego" engine,
   selected by `hugot.NewGoSession(ctx)`. ONNX Runtime (`-tags ORT`) and XLA (`-tags XLA`) need cgo and are compiled out.
2. **Stock hugot v0.8.6 gives wrong vectors for XLM-R models** (all the multilingual candidates). Its pure-Go tokenizer
   (`gomlx/go-huggingface` v0.4.32) implements SentencePiece *Unigram* as greedy longest-match instead of Viterbi on the
   vocab scores, and it ignores the `Precompiled` (NFKC) normalizer. On 14 Portuguese sentences, 10 tokenise differently
   from HF `tokenizers`; embeddings drift (min cosine to the reference 0.945, mean 0.982). This is silent: no error.
3. **Fix, verified:** a 60-line Viterbi patch to go-huggingface (via a `replace` directive) plus `norm.NFKC` on the input
   text. Token ids become identical to HF `tokenizers` on all 14 sentences for both models, and the vectors match
   ONNX Runtime to **max abs diff 1.2e-07 (min cosine 0.99999994)** for MiniLM and e5-small. Patch:
   `.../scratchpad/embed/tokenizer-viterbi.patch` (145 lines diff: `types.go`, `hftokenizer.go`, `tokenize.go`).
   Upstream should be asked to fix it; until then Chanda vendors the patched module.
4. **Recommendation:** default model `intfloat/multilingual-e5-small` (384-dim, 512 tokens, retrieval-trained,
   `query: ` / `passage: ` prefixes), behind the `Embedder` interface with `model_id` stored per vector. Build with
   `GOEXPERIMENT=simd` (about 4 to 5 times faster, see below). Reranker: `cross-encoder/mmarco-mMiniLMv2-L12-H384-v1`
   works and is cheap, so it can stay optional rather than off. Alternative embedder with a stronger determinism property:
   `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` (see Determinism).

## Versions

```
go 1.27.0                                   (hugot v0.8.6 go.mod requires it; the toolchain auto-downloads)
github.com/knights-analytics/hugot v0.8.6   (Apache-2.0)
github.com/gomlx/gomlx v0.28.34
github.com/gomlx/go-huggingface v0.4.32     (REPLACED by the patched copy, Apache-2.0)
github.com/gomlx/onnx-gomlx v0.5.31, gomlx/compute v0.1.28, gomlx/compute-onnx v0.1.28
golang.org/x/text v0.44.0                   (norm.NFKC)
```

`go list -deps` with `CGO_ENABLED=0` shows no package with cgo files. The ORT and XLA files carry
`//go:build cgo && (ORT || ALL)` and `... (XLA || ALL)`, so without those tags they return an error stub.
`NODOWNLOAD` build tag drops the HF downloader from the binary (use it in the server; keep the downloader in an install command).

Build flags used: `CGO_ENABLED=0 GOEXPERIMENT=simd go build` (simd is experimental, arm64 tested here, amd64 not tested).

## Working code (ran as written)

```go
session, err := hugot.NewGoSession(ctx)            // pure-Go GoMLX simplego backend
defer session.Destroy()
pipe, err := session.NewPipeline(hugot.FeatureExtractionConfig{
    ModelPath: modelDir,                           // model.onnx + tokenizer.json + config.json
    Name:      "emb",
    Options:   []hugot.FeatureExtractionOption{pipelines.WithNormalization()}, // mean pooling is built in; L2 is opt-in
})
// NFKC replaces the unimplemented HF "Precompiled" normalizer; prefix is "query: " / "passage: " for e5, "" for MiniLM.
texts = append(texts, prefix+norm.NFKC.String(s))
pipe.RunPipeline(ctx, []string{"warmup"})          // see Determinism: first call takes a different path on e5
out, err := pipe.RunPipeline(ctx, texts)           // out.Embeddings is [][]float32, 384 dims, unit norm
```

Reranker (also ran):

```go
rr, _ := session.NewPipeline(hugot.CrossEncoderConfig{ModelPath: dir, Name: "rr",
    Options: []hugot.CrossEncoderOption{pipelines.WithSortResults(), pipelines.WithBatchSize(16)}})
res, _ := rr.RunPipeline(ctx, query, docs)         // res.Results[i]: Document, Score, Index (sorted desc)
```

Model download (install step only): `hugot.DownloadModel(ctx, "intfloat/multilingual-e5-small", destDir, opts)` with
`opts := hugot.NewDownloadOptions(); opts.OnnxFilePath = "onnx/model.onnx"`. Note v0.8.6 takes `ctx` first. It fetches
`model.onnx`, `tokenizer.json`, `config.json`, `tokenizer_config.json`, `special_tokens_map.json` into
`destDir/<org>_<name>/`. It refuses repos with several `.onnx` files unless `OnnxFilePath` is set, and it does not fetch
`1_Pooling/config.json` (pooling is fixed to mean, which is right for all models here). Runtime then needs no network.

## Models

| Model | Dim | ONNX fp32 on disk | ONNX on HF | Prefix | Status in hugot (pure Go) |
| --- | --- | --- | --- | --- | --- |
| `intfloat/multilingual-e5-small` | 384 | 449 MB (int8 `Xenova/...` 118 MB) | yes: `onnx/model.onnx` in the repo | `query: ` / `passage: ` required by the model card | works, ref parity 1.2e-07 |
| `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` | 384 | 449 MB | yes: `onnx/model.onnx`, plus `qint8_arm64` etc. | none | works, ref parity 1.2e-07 |
| `cross-encoder/mmarco-mMiniLMv2-L12-H384-v1` (reranker) | 1 logit | 449 MB | yes: `onnx/model.onnx` | none | works, logits match ORT |
| `BAAI/bge-reranker-base` (reranker) | 1 logit | 1.1 GB | yes: `onnx/model.onnx` | none | works, 4 times slower |

Other facts: e5 without prefixes still runs (and the toy test below is unchanged) but the prefixes are part of the
trained format; both models share the XLM-R vocabulary (250k tokens, 16 MB `tokenizer.json`). MiniLM was trained with
128-token inputs, e5-small with 512. 100k vectors at 384 floats is 154 MB of BLOBs.
`onnx-community/bge-reranker-v2-m3-ONNX` and `jina-embeddings-v3` ship `.onnx_data` external weights (2.2 GB), not tried.

Quality: only a smoke test was run (10 Portuguese statements, 10 questions, both models and e5 with and without
prefix: hit@1 10/10, saturated, so it does **not** discriminate). The model choice for Portuguese must be decided on the
Chanda evaluation set, not on this. Note e5 cosine scores are compressed (unrelated Portuguese pair 0.84, paraphrase
0.94), MiniLM spreads them wider (unrelated about 0, paraphrase 0.88): thresholds for dedup and "near duplicate" gates
must be calibrated per model.

## Reranker

`hugot.CrossEncoderConfig` / `pipelines.CrossEncoderPipeline` exists and scores (query, doc) pairs; it applies a float32
sigmoid to the logit. Query "Quanto custa o plano premium?" against 6 documents:

| Model | Result | Time, 6 pairs / 30 pairs (stock arm64) | with simd |
| --- | --- | --- | --- |
| mmarco-mMiniLMv2 | correct order; relevant 1.0000 / 1.0000, rest 0.0001 to 0.0011 (matches ORT: 0.0001, 0.0007, 0.0011, 0.0002) | 341 ms / 1.63 s | 125 ms / 286 ms |
| bge-reranker-base | relevant 0.986 and 0.983, rest 0.0000 | 1.44 s / 5.38 s | 252 ms / 1.40 s |

Risk: the sigmoid saturates at 1.0 in float32, so two strong matches can tie; sort ties by id (deterministic) or
feed the raw logit through a text-classification pipeline instead. A rerank of the top 30 with mmarco takes about 0.3 s with simd.
Scores are bit-identical across repeated calls.

## Determinism (same machine)

Measured with SHA-256 over the float32 bits of each vector.

| Check | MiniLM (stock build) | e5-small (stock build) | simd build (both) |
| --- | --- | --- | --- |
| Same text, 20 repeats, same process | identical | identical (after warm-up) | identical |
| Two separate processes, 40 mixed-length texts | identical digest | identical digest | identical digest |
| Text alone vs inside a batch of 40 mixed lengths | 40/40 bit-identical | 36/40, max diff 7.5e-08 | 30 to 32/40, max diff 1.0e-07 |
| Same batch in reversed order (equal lengths) | identical | identical | identical |

Consequences:

- Output is deterministic for a **fixed call pattern** (same texts, same batch composition, same build, same machine).
- **Warm-up call needed for e5:** the first call logs `GoMLX: dynamic axes failed ... retrying with concrete input shapes`
  and takes a different code path; later results differ from the first call by about 6e-08. A dummy first call fixes it.
- Vectors are **not batch-invariant** at the 1e-7 level except MiniLM on the stock build. Write path should embed in a
  fixed way (for example one text per call, or fixed batches in a canonical order) so the byte-for-byte database
  determinism test holds; ranking effects of 1e-7 are negligible.
- The simd build changes vectors by about 1e-7 versus the stock build, so `model_id` should include the build flavour
  (for example `e5-small@go-simd`), and a CI determinism test must use the same flags as the shipped binary.
- Cross-architecture (arm64 vs amd64) bit equality was **not** tested and should not be assumed.

## Latency (8 cores, arm64, model loaded, warm)

| Case | MiniLM stock | MiniLM simd | e5-small stock | e5-small simd |
| --- | --- | --- | --- | --- |
| 1 short sentence (min of 7) | 24 to 27 ms | 10 ms | 30 ms | 11.8 ms |
| batch of 32 short sentences | 1.06 to 1.09 s | 0.21 s | 1.41 s | 0.26 s |
| 40 texts, 3 to 120 words, one by one | 3.8 s total (about 95 ms each) | 1.6 s | 4.8 to 5.5 s | 2.8 s |
| model load (ONNX parse + graph) | 0.7 to 1.3 s | | 0.7 s | |

There is no per-shape compile cost (no JIT; 40 differently sized inputs cost the same on every pass). Batching barely
helps on the stock build (about 28 ms/sentence at 32 versus 24 ms alone); the engine already uses all cores
(about 560% CPU). Peak RSS is **1.5 to 1.7 GB** for a 449 MB fp32 model: a real cost for an "agent memory" sidecar.
The Xenova int8 MiniLM (118 MB) loads and runs, but with no speed gain, 0.996 cosine to fp32, and an extra
`dynamic axes` fallback; not recommended.

## Risks

- **Tokenizer patch must be carried** (vendored `replace`) until upstream fixes Unigram scoring; add a unit test that
  compares ids against a golden file produced by HF `tokenizers` for Portuguese text, and apply `norm.NFKC` before embedding.
  Other normalizers or pre-tokenizers in a different model's `tokenizer.json` may also be incomplete; check each new model the same way.
- **Stdout pollution:** hugot prints to stdout with `fmt.Printf` (the `GoMLX: dynamic axes failed` line, and statistics
  helpers). The MCP stdio transport uses stdout for the protocol. Swap `os.Stdout` to `os.Stderr` around session creation
  and the warm-up call (not tested, but `fmt.Printf` reads `os.Stdout` at call time). MiniLM does not trigger the line.
- **Memory:** about 1.6 GB resident. Load the embedder lazily and allow unloading; consider fp16 or int8 only after measuring.
- **Go 1.27 and `GOEXPERIMENT=simd`** are new; simd is experimental and was only run on arm64. Without it,
  a batch of 32 costs about 1 s, still acceptable for a write path but slow for bulk import (100k records, about 45 min stock vs 10 min simd).
- Install step must fetch about 450 MB per model (reranker adds 450 MB, or 1.1 GB for bge); read and write paths must
  never call the downloader. Verify a SHA-256 of `model.onnx` at startup, since model files affect determinism.
- hugot is a young project (v0.8.x, moving API: `DownloadModel` gained `ctx`); pin the version and wrap it in `internal/embed`.
- Embedding quality in Portuguese is unmeasured here; e5-small and MiniLM must be compared on the real evaluation set.

## Cheapest fallbacks (not needed, listed for completeness)

- If the patched tokenizer is unacceptable: run the model with `github.com/gomlx/onnx-gomlx` directly and tokenise with
  an own Viterbi Unigram (about 60 lines, same algorithm as the patch). Same backend, same speed.
- cgo build option: `-tags ORT` with `libonnxruntime` (fastest, required for other accelerators). Not measured here,
  and incompatible with the single no-cgo binary constraint.
