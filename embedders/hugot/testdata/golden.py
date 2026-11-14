# Generates reference token ids, embeddings and reranker logits with HF tokenizers + ONNX Runtime.
# usage: python -I golden.py <models-dir> <sentences-file> <out-dir>
import json, sys
import numpy as np, onnxruntime as ort
from tokenizers import Tokenizer

models, sents, out = sys.argv[1:4]
texts = [l.rstrip("\n") for l in open(sents, encoding="utf-8") if l.strip()]

def session(d):
    o = ort.SessionOptions(); o.intra_op_num_threads = 1; o.inter_op_num_threads = 1
    return ort.InferenceSession(d + "/model.onnx", sess_options=o, providers=["CPUExecutionProvider"])

def feed(sess, enc):
    f = {"input_ids": np.array([e.ids for e in enc], dtype=np.int64),
         "attention_mask": np.array([e.attention_mask for e in enc], dtype=np.int64)}
    if "token_type_ids" in [i.name for i in sess.get_inputs()]:
        f["token_type_ids"] = np.zeros_like(f["input_ids"])
    return f

for name, dirname, prefix in [("e5-small", "intfloat_multilingual-e5-small", "passage: "),
                              ("paraphrase-minilm-l12", "sentence-transformers_paraphrase-multilingual-MiniLM-L12-v2", "")]:
    d = models + "/" + dirname
    tok = Tokenizer.from_file(d + "/tokenizer.json")
    sess = session(d)
    ids, vecs = [], []
    for t in texts:  # one text per call, like the Go embedder
        enc = tok.encode_batch([prefix + t])
        f = feed(sess, enc)
        h = sess.run(None, f)[0]
        m = f["attention_mask"][..., None].astype(np.float32)
        e = (h * m).sum(1) / m.sum(1)
        e = e / np.linalg.norm(e, axis=1, keepdims=True)
        ids.append(enc[0].ids)
        vecs.append([float(x) for x in e[0]])
    json.dump({"model": name, "prefix": prefix, "texts": texts, "ids": ids, "vectors": vecs},
              open(out + "/golden_" + name + ".json", "w", encoding="utf-8"), ensure_ascii=False)

d = models + "/cross-encoder_mmarco-mMiniLMv2-L12-H384-v1"
tok = Tokenizer.from_file(d + "/tokenizer.json")
sess = session(d)
query = "Quanto custa o plano premium?"
docs = ["O gato dorme no sofá da sala.",
        "O plano premium custa R$ 49,90 por mês, com cobrança mensal.",
        "A reunião com o cliente foi remarcada para sexta-feira.",
        "Os planos de assinatura incluem o básico, o premium e o empresarial.",
        "Maria trabalha como engenheira de dados em Lisboa.",
        "The premium plan costs 49.90 per month."]
logits = []
for doc in docs:
    enc = tok.encode_batch([(query, doc)])
    logits.append(float(sess.run(None, feed(sess, enc))[0].ravel()[0]))
json.dump({"model": "mmarco-minilm-l12", "query": query, "docs": docs, "logits": logits},
          open(out + "/golden_mmarco-minilm-l12.json", "w", encoding="utf-8"), ensure_ascii=False)
print("ok", len(texts), logits)
