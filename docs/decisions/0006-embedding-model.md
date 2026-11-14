# 0006 Default embedder and the hugot path

Answers, provisionally, the BASE.md open question "default multilingual
embedding model" (evidence: `docs/research/embeddings.md`).

- hugot v0.8.6 runs in pure Go (GoMLX simplego backend), but requires Go 1.27
  and its tokenizer (go-huggingface v0.4.32) does greedy Unigram instead of
  Viterbi: 10 of 14 Portuguese sentences tokenize wrong, silently. A ~60-line
  patch fixes it (vectors then match ONNX Runtime within 1.2e-7).
- Default stays the built-in deterministic `hash` embedder, so the main
  module builds with Go 1.26 and no patched dependency.
- The real model ships as an optional, separately built embedder (own Go
  module, Go 1.27, patched tokenizer via `replace`), with
  `intfloat/multilingual-e5-small` (384 dim, query:/passage: prefixes) as the
  first candidate. Choice between e5-small and paraphrase-multilingual-MiniLM
  is decided on the evaluation set, not on the smoke test.
- Determinism constraints for that embedder: one warm-up call after load,
  embed one text per call (vectors are not batch-invariant at 1e-7), record the
  build flavour (simd or not) in `model_id`, redirect stdout during model load
  (hugot prints to stdout, which would corrupt MCP stdio).
