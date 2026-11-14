# Patched copy of github.com/gomlx/go-huggingface v0.4.32

Upstream: https://github.com/gomlx/go-huggingface, tag v0.4.32, Apache-2.0
(`LICENSE` in this directory is the upstream licence, unchanged). Used by
`embedders/hugot/go.mod` through
`replace github.com/gomlx/go-huggingface => ./third_party/go-huggingface-patched`.

## Why

v0.4.32 tokenises SentencePiece *Unigram* models (every XLM-R model: the
multilingual e5 and MiniLM embedders and the mmarco reranker) by greedy
longest match instead of Viterbi over the vocabulary log-probabilities. On 14
Portuguese sentences 10 were tokenised differently from HF `tokenizers`, and
the embeddings drifted (min cosine 0.945 to the ONNX Runtime reference),
silently. With this patch the token ids equal HF `tokenizers` on all 14
sentences and the vectors match ONNX Runtime within 1.2e-7. Evidence:
`docs/research/embeddings.md`.

The missing `Precompiled` (NFKC) normalizer is not patched here; the Chanda
embedder applies `norm.NFKC` to its input instead.

## What changed

Only `tokenizers/hftokenizer/`:

- `types.go`: `Model` gains `Scores []float64` (Unigram log-prob by token id)
  and `MinScore float64` (`min(0, Scores...)`).
- `hftokenizer.go`: the Unigram vocab array `[[piece, score], ...]` fills
  `Scores` and `MinScore` once, at load (computing the minimum per word cost
  38 ms per paragraph on the 250k-piece XLM-R vocabulary).
- `tokenize.go`: `unigramTokenizeWithSpans` is Viterbi: best-scoring
  segmentation over pieces of at most 32 runes, an unknown single rune costs
  `MinScore - 10` (SentencePiece's `kUnkPenalty`), byte offsets are kept for
  the spans. Imports re-sorted by gofmt.

Known upstream issues left alone (worked around in `embedders/hugot`):
`EncodeOptions.MaxLen` is ignored (no truncation), and token spans are wrong
for some `tokenizer.json` files (every span `{0, 11}` on
paraphrase-multilingual-MiniLM-L12-v2).

Everything else is an unmodified copy, trimmed to the packages hugot imports
(`.`, `hub`, `internal/files`, `internal/downloader`, `tokenizers`,
`tokenizers/{api,bucket,hftokenizer,sentencepiece}`), without tests, examples,
datasets, notebooks or test data. The two `tool` directives of the upstream
`go.mod` (pointing at the removed `cmd/` packages) were dropped; the
`require` list is unchanged.

To review the change: `diff -ru $(go env GOMODCACHE)/github.com/gomlx/go-huggingface@v0.4.32/tokenizers/hftokenizer tokenizers/hftokenizer`.

## Removing it

When upstream ships a Viterbi Unigram tokenizer: delete this directory and the
`replace` line, bump go-huggingface, and run `go test ./...` in
`embedders/hugot` (the tokenizer golden test must still pass).
