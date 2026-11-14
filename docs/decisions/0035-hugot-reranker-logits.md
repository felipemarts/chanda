# 0035 hugot reranker returns raw logits, one pair per call

`hugot.Reranker` implements `core.Reranker` with
cross-encoder/mmarco-mMiniLMv2-L12-H384-v1 (`mmarco-minilm-l12`).

- hugot's `CrossEncoderPipeline` returns a float32 sigmoid of the logit, which
  saturates at 1.0: the two relevant documents of the test query both score
  1.0000 and tie. `Score` therefore runs each pair through hugot's exported
  backend steps (`TokenizeInputs`, `CreateInputTensors`, `RunSessionOnBatch`)
  and returns the logit (10.84 and 9.98 there; higher is more relevant). A
  test checks that sigmoid(logit) equals hugot's own score.
- The pair is `<s> query </s></s> doc </s>`, the XLM-R template hugot builds;
  models with another separator are refused at load. Query and document are
  NFKC-normalised; a pair longer than 512 tokens loses the end of the document.
- One pair per call, in input order: a document's score does not depend on
  the other candidates (bit-identical alone or in a list). Cost: about 2 times
  a batched call (30 pairs: 0.63 s simd, 2.4 s stock, measured under load).
  Callers break equal scores by record id.
- Logits match ONNX Runtime within 6e-6 (stock) and 1.2e-4 (simd) for logits
  of magnitude up to 11.
