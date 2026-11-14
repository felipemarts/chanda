"""Local fake OpenAI-compatible servers for offline tests: chat completions
(Mem0 extraction, reader, judge) and embeddings. Loopback only."""

from __future__ import annotations

import hashlib
import json
import math
import re
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def hash_embed(text: str, dims: int) -> list[float]:
    """Deterministic bag-of-words hashing embedding (unit norm)."""
    v = [0.0] * dims
    for w in re.findall(r"[a-z0-9]+", text.lower()):
        h = int.from_bytes(hashlib.sha256(w.encode()).digest()[:8], "big")
        v[h % dims] += 1.0 if (h >> 63) == 0 else -1.0
    n = math.sqrt(sum(x * x for x in v)) or 1.0
    return [x / n for x in v]


class FakeServer:
    """chat: replies via a callable(body) -> content string.
    embeddings: hash_embed with `dims`."""

    def __init__(self, dims: int = 256, chat_reply=None):
        self.dims = dims
        self.chat_reply = chat_reply or (lambda body: "ok")
        self.chat_bodies: list[dict] = []
        self.chat_paths: list[str] = []
        self.embed_bodies: list[dict] = []
        self.lock = threading.Lock()
        outer = self

        class H(BaseHTTPRequestHandler):
            def log_message(self, *a):  # silence
                pass

            def do_POST(self):
                n = int(self.headers.get("Content-Length", 0))
                body = json.loads(self.rfile.read(n) or b"{}")
                if self.path.endswith("/embeddings"):
                    with outer.lock:
                        outer.embed_bodies.append(body)
                    inp = body["input"]
                    inp = [inp] if isinstance(inp, str) else inp
                    data = [{"object": "embedding", "index": i, "embedding": hash_embed(t, outer.dims)}
                            for i, t in enumerate(inp)]
                    out = {"object": "list", "data": data, "model": body.get("model"),
                           "usage": {"prompt_tokens": 0, "total_tokens": 0}}
                elif self.path.endswith("/chat/completions"):
                    with outer.lock:
                        outer.chat_bodies.append(body)
                        outer.chat_paths.append(self.path)
                    content = outer.chat_reply(body)
                    prompt_chars = sum(len(m.get("content") or "") for m in body.get("messages", []))
                    pt = (prompt_chars + 3) // 4
                    ct = (len(content) + 3) // 4
                    out = {"id": "fake", "object": "chat.completion", "model": body.get("model"),
                           "choices": [{"index": 0, "finish_reason": "stop",
                                        "message": {"role": "assistant", "content": content}}],
                           "usage": {"prompt_tokens": pt, "completion_tokens": ct, "total_tokens": pt + ct,
                                     "prompt_cache_hit_tokens": 0, "prompt_cache_miss_tokens": pt}}
                else:
                    self.send_response(404)
                    self.end_headers()
                    return
                b = json.dumps(out).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(b)))
                self.end_headers()
                self.wfile.write(b)

        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), H)
        self.url = f"http://127.0.0.1:{self.httpd.server_address[1]}"
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self.thread.start()

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


def mem0_extraction_reply(body: dict) -> str:
    """Fake Mem0 extraction: one memory per user message of '## New Messages'."""
    user = body["messages"][-1]["content"]
    m = re.search(r"## New Messages\n(.*?)\n\n## ", user, re.S)
    new = m.group(1) if m else ""
    mems = []
    for line in new.splitlines():
        if line.startswith("user: "):
            mems.append({"id": str(len(mems)), "text": "User said: " + line[6:].strip()[:120]})
    return json.dumps({"memory": mems})
