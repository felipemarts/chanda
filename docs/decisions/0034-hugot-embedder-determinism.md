# 0034 hugot embedder: model ids, input handling, determinism

Implements the constraints of 0006 in `embedders/hugot`.

- Models: `e5-small` (intfloat/multilingual-e5-small, `query: ` for
  `EmbedQuery`, `passage: ` for `Embed`, window 512) and
  `paraphrase-minilm-l12` (no prefixes, window 128, its sentence-transformers
  `max_seq_length`). Both 384-dim, mean pooling.
- Model id: `<name>@hugot<version>+<flavour>`, for example
  `e5-small@hugot0.8.6+simd` or `+stock`. The flavour is a compile-time
  constant from the `goexperiment.simd` build tag; a test keeps `HugotVersion`
  equal to the `go.mod` requirement. A configured `model_id` or `dim` that
  disagrees is rejected (`invalid_argument`), as for the hash embedder.
- Model files: a local directory only. `chanda-model.json` (written by
  `chanda-hugot models pull` or `adopt`) names the model and holds the SHA-256
  of each file; every load verifies it (`integrity` error on mismatch, about
  0.3 s for 450 MB). Without a manifest the directory name must be the model
  name or hugot's download name, and nothing is verified.
- Input: NFKC (the patched tokenizer still lacks HF's `Precompiled`
  normalizer), then the role prefix, then clipping. hugot sets the tokenizer
  limit to `max_position_embeddings` (514, two too many for XLM-R) and
  go-huggingface ignores it, so a long text crashed the run. The text is cut
  after the last whole word whose prefix fits `window - 2` tokens (binary
  search over word ends; one huge word is cut on a rune boundary). Token spans
  are not used: go-huggingface reports wrong spans for MiniLM's
  `tokenizer.json`. Texts that differ only past the window embed identically.
- Determinism: one text per pipeline call (vectors are not batch-invariant at
  1e-7), calls serialised by a mutex, one warm-up call after load (e5 switches
  graph path on its first run), L2 normalisation done here in float64 instead
  of hugot's option. Tested: bit-identical across two loads, across repeated
  calls, alone versus in a list; vectors match ONNX Runtime within 1.5e-7
  (stock) and 4e-6 (simd); token ids equal HF `tokenizers` on 14 Portuguese
  sentences for both models.
- MCP stdio safety: `os.Stdout` points at `os.Stderr` while the session, the
  pipeline and the warm-up run (mutex-protected, restored on error or panic).
  The MCP transport captured its `*os.File` when it connected, so it is
  unaffected. Tested with a fake loader and with the real e5 load (which
  prints `GoMLX: dynamic axes failed ...`): nothing reaches stdout, also on
  embed calls after the warm-up.
- The patched tokenizer computed `min(score)` over the 250k vocabulary for
  every word (38 ms per paragraph); it is now computed once at load (0.25 ms).
