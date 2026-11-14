# chanda-hugot: the optional multilingual embedder

Chanda's default embedder is the built-in, lexical `hash` provider. This
directory adds real multilingual sentence embeddings and a cross-encoder
reranker, run in pure Go (no cgo) with
[hugot](https://github.com/knights-analytics/hugot) v0.8.6 on the GoMLX
`simplego` backend. It is a separate Go module because hugot needs Go 1.27
and a patched tokenizer (`third_party/go-huggingface-patched`, see its
`PATCH.md`); the main module keeps Go 1.26 and does not depend on it.
Background: `docs/research/embeddings.md`, decisions 0006 and 0033 to 0036.

| Model (`models pull` name) | Hugging Face repo | Role | Dim | Window | Prefixes |
| --- | --- | --- | --- | --- | --- |
| `e5-small` | intfloat/multilingual-e5-small | embedder | 384 | 512 | `query: ` / `passage: ` |
| `paraphrase-minilm-l12` | sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2 | embedder | 384 | 128 | none |
| `mmarco-minilm-l12` | cross-encoder/mmarco-mMiniLMv2-L12-H384-v1 | reranker | logit | 512 | none |

Each ONNX file is about 450 MB on disk and about 1.5 GB resident once loaded.

## Build

```sh
cd embedders/hugot
CGO_ENABLED=0 GOEXPERIMENT=simd go build -o chanda-hugot ./cmd/chanda-hugot   # 4 to 5 times faster
CGO_ENABLED=0 go build -o chanda-hugot ./cmd/chanda-hugot                      # portable "stock" kernels
```

The Go 1.27 toolchain is fetched automatically (`GOTOOLCHAIN=auto`). The build
flavour is part of the model id (`+simd` or `+stock`) because the two give
vectors that differ by about 1e-7: vectors from one flavour are never mixed
with queries from the other. `GOEXPERIMENT=simd` was only exercised on arm64.

## Install a model (the only step that uses the network)

```sh
./chanda-hugot models list
./chanda-hugot models pull e5-small /var/lib/chanda/models/e5-small
./chanda-hugot models pull mmarco-minilm-l12 /var/lib/chanda/models/mmarco   # optional reranker
./chanda-hugot models verify /var/lib/chanda/models/e5-small
```

`pull` downloads `model.onnx`, `tokenizer.json` and the config files into a
new directory and writes `chanda-model.json`, which names the model and holds
the SHA-256 of every file. Every load checks those digests (a changed file
changes the vectors). For files fetched by other means, `models adopt <model>
<dir>` writes the manifest offline. A directory without a manifest is still
accepted when its name is the model name (`e5-small`) or hugot's download
name (`intfloat_multilingual-e5-small`), without checksum verification.

Reading and writing never download anything: a missing model is an
`unavailable` error.

## Configure

```yaml
# chanda.yaml
embed:
  provider: hugot
  model_path: /var/lib/chanda/models/e5-small
  model_id: e5-small@hugot0.8.6+simd   # optional; if set it must match the model and the build
  dim: 384                             # must match the model (the default 256 is the hash embedder's)
```

A `model_id` or `dim` that disagrees with the model on disk is rejected at
start-up, as for the hash embedder. Changing model, hugot version or build
flavour changes the model id, so vectors must be rebuilt.

Similarity scales differ per model: e5 compresses cosines (an unrelated
Portuguese pair scores about 0.83, a paraphrase 0.94), MiniLM spreads them
(about 0.08 and 0.88). Thresholds such as `write.near_dup_threshold` and
`sleep.merge_similarity` must be calibrated for the model in use.

## Try it

```sh
./chanda-hugot embed /var/lib/chanda/models/e5-small "O gato dorme no sofá."
./chanda-hugot embed --query /var/lib/chanda/models/e5-small "onde o gato dorme?"
./chanda-hugot rerank /var/lib/chanda/models/mmarco "Quanto custa o plano premium?" "O plano custa R\$ 49,90." "O gato dorme."
```

Output is JSON on stdout. Anything hugot prints while loading
(`GoMLX: dynamic axes failed ...` on e5's first run) is sent to stderr, so
stdout stays clean for the MCP stdio transport.

`chanda-hugot serve|ingest|...` will be the regular chanda CLI with provider
`hugot` registered once the CLI moves out of `cmd/chanda` into an importable
package (decision 0033); until then those commands report that.

## Library

```go
hugot.Register()                                   // embed.provider "hugot" in internal/embed.NewFromConfig
e, err := hugot.Open(ctx, dir)                     // *hugot.Embedder, a core.RoleEmbedder
v, err := e.Embed(ctx, texts)                      // stored texts ("passage: " for e5)
q, err := e.EmbedQuery(ctx, queries)               // queries ("query: " for e5)
r, err := hugot.OpenReranker(ctx, dir)             // *hugot.Reranker, a core.Reranker
s, err := r.Score(ctx, query, docs)                // raw logits, higher is more relevant
```

Determinism: input is NFKC-normalised, cut at the end of the last whole word
that fits the window, embedded one text per call after a warm-up call, and
L2-normalised in float64. The same text gives bit-identical vectors across
loads, calls and neighbouring texts, on one machine and build flavour.
Equality across CPU architectures was not tested.

## Tests

```sh
cd embedders/hugot
CGO_ENABLED=0 go test ./...                       # stock
GOEXPERIMENT=simd CGO_ENABLED=0 go test ./...     # simd
go test -run '^$' -bench . -benchtime 5x          # latencies, see decision 0036
```

Tests that need model files skip with instructions when they are missing.
They look in `$CHANDA_HUGOT_MODELS/<name>`, default `testdata/models/<name>`
(git-ignored; a symlink to an existing download works):

```sh
go run ./cmd/chanda-hugot models pull e5-small testdata/models/e5-small
go run ./cmd/chanda-hugot models pull paraphrase-minilm-l12 testdata/models/paraphrase-minilm-l12
go run ./cmd/chanda-hugot models pull mmarco-minilm-l12 testdata/models/mmarco-minilm-l12
```

The tests compare token ids with HF `tokenizers` and vectors and logits with
ONNX Runtime (`testdata/golden_*.json`, made by `testdata/golden.py` from
`testdata/sentences.txt`).
