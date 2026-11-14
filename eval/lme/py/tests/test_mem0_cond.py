"""Condition C end to end with the real mem0ai package against a fake
OpenAI-compatible LLM and a fake embedding server (zero paid calls), and
once more through the real lmeproxy binary when Go is available."""

from __future__ import annotations

import json
import os
import shutil
import socket
import subprocess
import time

import pytest

import common
import data
import mem0_cond
from fakes import FakeServer, mem0_extraction_reply

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", ".."))


@pytest.fixture
def servers():
    llm = FakeServer(chat_reply=mem0_extraction_reply)
    emb = FakeServer(dims=256)
    yield llm, emb
    llm.close()
    emb.close()


def _run(tmp, llm_url, emb, extra=()):
    ds, sample = data.write(str(tmp))
    out = os.path.join(tmp, "C.jsonl")
    rc = mem0_cond.main(["--dataset", ds, "--sample", sample, "--proxy", llm_url,
                         "--embed-url", emb.url + "/v1", "--embed-model", "chanda-hash-256", "--embed-dims", "256",
                         "--work-dir", os.path.join(tmp, "work"), "--out", out, *extra])
    return rc, out


def test_config_builds():
    cfg = mem0_cond.mem0_config("/x", llm_base="http://127.0.0.1:8787/", tag="C-ingest", model="deepseek-flash",
                                embed_url="http://127.0.0.1:8790/v1", embed_model="e5", dims=384)
    from mem0.configs.base import MemoryConfig

    mc = MemoryConfig(**cfg)
    assert mc.llm.provider == "deepseek"
    assert mc.llm.config["deepseek_base_url"] == "http://127.0.0.1:8787/tag/C-ingest/v1"
    assert mc.llm.config["temperature"] == 0.0
    assert mc.embedder.config["embedding_dims"] == 384
    assert mc.vector_store.provider == "qdrant" and mc.vector_store.config.path == "/x/qdrant"


def test_end_to_end_with_fakes(tmp_path, servers):
    llm, emb = servers
    rc, out = _run(str(tmp_path), llm.url, emb)
    assert rc == 0
    recs = {r["question_id"]: r for r in common.read_jsonl(out)}
    assert set(recs) == {"q1", "q2_abs", "q3"}
    r1 = recs["q1"]
    assert r1["condition"] == "C"
    assert "golden retriever" in r1["context"]
    assert "[2023/05/20 (Sat) 02:21] User said: I just adopted" in r1["context"]
    assert r1["context_tokens"] == common.est_tokens(r1["context"]) <= 2000
    assert r1["sessions"] == 2 and r1["memories_total"] == 2 and r1["extraction_parse_errors"] == 0
    assert r1["ingest_seconds"] > 0 and r1["retrieval_latency_ms"] > 0 and r1["db_bytes"] > 0
    assert r1["mem0_version"] == "2.2.1"
    # chronological order of the packed lines
    lines = r1["context"].splitlines()
    assert lines[0].startswith("[2023/05/20") and lines[1].startswith("[2023/05/22")

    # One extraction call per session, through the tagged base URL, temperature 0, JSON mode.
    assert len(llm.chat_bodies) == 4  # 2 + 1 + 1 sessions
    assert set(llm.chat_paths) == {"/tag/C-ingest/v1/chat/completions"}
    for b in llm.chat_bodies:
        assert b["model"] == "deepseek-flash" and b["temperature"] == 0
        assert b["response_format"] == {"type": "json_object"}
    # Observation Date = the session date, not today.
    q1_first = [b["messages"][-1]["content"] for b in llm.chat_bodies
                if "## New Messages\nuser: I just adopted" in b["messages"][-1]["content"]]
    assert len(q1_first) == 1
    assert "## Observation Date\n2023-05-20" in q1_first[0] and "## Current Date\n2023-05-20" in q1_first[0]
    # Mem0 also sends the previous session's last messages with each add.
    q1_second = [b["messages"][-1]["content"] for b in llm.chat_bodies
                 if "## New Messages\nuser: Can you suggest a pasta" in b["messages"][-1]["content"]]
    assert "golden retriever puppy" in q1_second[0].split("## Recently Extracted")[0]
    # Embeddings: our server, with the configured dimension.
    assert emb.embed_bodies and all(b.get("dimensions") == 256 for b in emb.embed_bodies)
    # Per-question databases are removed after measuring.
    assert os.listdir(os.path.join(tmp_path, "work", "db")) == []

    # Resume: nothing is re-ingested.
    n = len(llm.chat_bodies)
    rc, _ = _run(str(tmp_path), llm.url, emb)
    assert rc == 0 and len(llm.chat_bodies) == n


def test_instructions_date_mode(tmp_path, servers):
    llm, emb = servers
    rc, _ = _run(str(tmp_path), llm.url, emb, ["--date-mode", "instructions", "--questions", "q1"])
    assert rc == 0
    body = llm.chat_bodies[0]["messages"][-1]["content"]
    assert "## Custom Instructions\nThese messages were exchanged on 2023/05/20 (Sat) 02:21" in body


def test_pack_budget():
    res = [{"memory": "m" * 4000, "created_at": "2023-01-02"},   # 1000+ tokens
           {"memory": "x" * 3990, "created_at": "2023-01-01"},
           {"memory": "too big " * 300, "created_at": "2023-01-03"},
           {"memory": "small", "created_at": "2022-12-31", "metadata": {"session_date": "2022/12/31 (Sat) 10:00"}}]
    ctx, n = mem0_cond.pack(res, 2000)
    assert common.est_tokens(ctx) <= 2000
    assert n == 3
    assert ctx.splitlines()[0] == "[2022/12/31 (Sat) 10:00] small"


def test_llm_failure_stops(tmp_path):
    emb = FakeServer(dims=256)
    try:
        # Nothing listens on this port: Mem0 raises LLMError and the run stops.
        s = socket.socket()
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
        s.close()
        rc, out = _run(str(tmp_path), f"http://127.0.0.1:{port}", emb, ["--questions", "q1"])
        assert rc == 1 and not os.path.exists(out)
    finally:
        emb.close()


@pytest.mark.skipif(shutil.which("go") is None, reason="go not installed")
def test_through_real_lmeproxy(tmp_path, servers):
    """Mem0 -> lmeproxy -> fake upstream: tags and cost accounting."""
    llm, emb = servers
    binp = os.path.join(tmp_path, "lmeproxy")
    subprocess.run(["go", "build", "-o", binp, "./eval/cmd/lmeproxy"], cwd=REPO, check=True)
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    cost = os.path.join(tmp_path, "cost.json")
    env = {k: v for k, v in os.environ.items() if k != "DEEPSEEK_API_KEY"}
    p = subprocess.Popen([binp, "-allow-no-key", "-cap-usd", "1", "-upstream", llm.url,
                          "-listen", f"127.0.0.1:{port}", "-cost", cost, "-log", os.path.join(tmp_path, "calls.jsonl")],
                         env=env, stderr=subprocess.PIPE)
    try:
        for _ in range(100):
            try:
                socket.create_connection(("127.0.0.1", port), timeout=0.2).close()
                break
            except OSError:
                time.sleep(0.05)
        rc, out = _run(str(tmp_path), f"http://127.0.0.1:{port}", emb, ["--questions", "q1"])
        assert rc == 0
        c = json.load(open(cost))
        assert c["by_tag"]["C-ingest"]["calls"] == 2
        assert c["by_tag"]["C-ingest"]["prompt_tokens"] > 0 and c["spent_usd"] > 0
        # The proxy forced non-thinking mode upstream.
        assert all(b["thinking"] == {"type": "disabled"} for b in llm.chat_bodies)
        assert set(llm.chat_paths) == {"/chat/completions"}
    finally:
        p.terminate()
        p.wait(timeout=10)
