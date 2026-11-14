"""answer.py and judge.py end to end against a fake OpenAI-compatible server
standing in for lmeproxy (zero paid calls)."""

from __future__ import annotations

import json
import os

import pytest

import answer
import common
import data
import judge
import prompts
import report
from fakes import FakeServer


@pytest.fixture
def fake():
    def reply(body):
        p = body["messages"][0]["content"]
        if p.startswith("I will give you an unanswerable question"):
            return "yes"
        if p.startswith("I will give you a question"):
            return "Yes." if "Golden" in p and "golden retriever" in p.lower().split("model response:")[-1] else "no"
        return "Your dog is a golden retriever." if "puppy" in p else "I don't know."

    s = FakeServer(chat_reply=reply)
    yield s
    s.close()


def test_answer_and_judge_conditions_A_and_B(tmp_path, fake):
    ds, sample = data.write(str(tmp_path))
    raw = os.path.join(tmp_path, "raw")
    ctx = os.path.join(tmp_path, "B.jsonl")
    for qid, c in (("q1", "[2023/05/20 (Sat) 02:21] I just adopted a golden retriever puppy named Max."),
                   ("q2_abs", "[2023/05/25] running shoes"), ("q3", "")):
        common.append_jsonl(ctx, {"question_id": qid, "condition": "B", "context": c,
                                  "context_tokens": common.est_tokens(c), "ingest_seconds": 0.5,
                                  "retrieval_latency_ms": 3.0, "extra": 1})

    assert answer.main(["--condition", "A", "--dataset", ds, "--sample", sample, "--proxy", fake.url,
                        "--out-dir", raw, "--workers", "2"]) == 0
    assert answer.main(["--condition", "B", "--contexts", ctx, "--dataset", ds, "--sample", sample,
                        "--proxy", fake.url, "--out-dir", raw, "--limit", "2"]) == 0
    a = common.read_jsonl(os.path.join(raw, "A.answers.jsonl"))
    b = common.read_jsonl(os.path.join(raw, "B.answers.jsonl"))
    assert {r["question_id"] for r in a} == {"q1", "q2_abs", "q3"}
    assert {r["question_id"] for r in b} == {"q2_abs", "q1"}  # --limit 2 follows the sample order
    assert all(r["context"] == "" for r in a)
    rb = {r["question_id"]: r for r in b}["q1"]
    assert rb["answer"] == "Your dog is a golden retriever."
    assert rb["reader"]["tokens"]["prompt"] > 0 and rb["reader"]["max_tokens"] == 800
    assert rb["context_meta"] == {"ingest_seconds": 0.5, "retrieval_latency_ms": 3.0, "extra": 1}
    assert rb["gold_answer"] == "Golden retriever"
    # q3's numeric gold answer is kept as text
    assert {r["question_id"]: r for r in a}["q3"]["gold_answer"] == "42"

    # Requests: tagged paths, temperature 0, official reader prompt with the date and context.
    reader_bodies = [(p, bd) for p, bd in zip(fake.chat_paths, fake.chat_bodies)]
    assert {p for p, _ in reader_bodies} == {"/tag/A-reader/v1/chat/completions", "/tag/B-reader/v1/chat/completions"}
    for _, bd in reader_bodies:
        assert bd["temperature"] == 0 and bd["max_tokens"] == 800 and bd["model"] == "deepseek-flash"
        assert len(bd["messages"]) == 1 and bd["messages"][0]["role"] == "user"
    q1 = data.QUESTIONS[0]
    want = prompts.reader_prompt(rb["context"], q1["question_date"], q1["question"])
    assert any(bd["messages"][0]["content"] == want for _, bd in reader_bodies)

    # Rerun: nothing left to do, no new calls.
    n = len(fake.chat_bodies)
    assert answer.main(["--condition", "A", "--dataset", ds, "--sample", sample, "--proxy", fake.url, "--out-dir", raw]) == 0
    assert len(fake.chat_bodies) == n

    assert judge.main(["--condition", "A", "--raw-dir", raw, "--proxy", fake.url]) == 0
    assert judge.main(["--condition", "B", "--raw-dir", raw, "--proxy", fake.url]) == 0
    ja = {r["question_id"]: r for r in common.read_jsonl(os.path.join(raw, "A.jsonl"))}
    jb = {r["question_id"]: r for r in common.read_jsonl(os.path.join(raw, "B.jsonl"))}
    judge_bodies = fake.chat_bodies[n:]
    assert all(bd["max_tokens"] == 10 and bd["temperature"] == 0 for bd in judge_bodies)
    assert jb["q1"]["verdict"] is True and jb["q1"]["judge"]["template"] == "single-session-user"
    assert jb["q2_abs"]["judge"]["template"] == "abstention" and jb["q2_abs"]["verdict"] is True
    assert ja["q1"]["verdict"] is False
    assert ja["q3"]["judge"]["template"] == "knowledge-update"
    for r in list(ja.values()) + list(jb.values()):
        for k in ("question", "gold_answer", "question_type", "condition", "context", "answer", "verdict"):
            assert k in r
    s = report.summarize(report.load_raw(raw))
    assert s["conditions"]["B"]["total"]["correct"] == 2


def test_answer_refuses_over_budget_and_missing_context(tmp_path, fake):
    ds, sample = data.write(str(tmp_path))
    ctx = os.path.join(tmp_path, "D.jsonl")
    common.append_jsonl(ctx, {"question_id": "q1", "condition": "D", "context": "x" * 8001, "context_tokens": 2001})
    with pytest.raises(SystemExit):
        answer.main(["--condition", "D", "--contexts", ctx, "--dataset", ds, "--sample", sample,
                     "--proxy", fake.url, "--out-dir", str(tmp_path), "--questions", "q1"])
    with pytest.raises(SystemExit):  # q2_abs has no context line
        answer.main(["--condition", "D", "--contexts", ctx, "--dataset", ds, "--sample", sample,
                     "--proxy", fake.url, "--out-dir", str(tmp_path)])
    assert fake.chat_bodies == []


def test_proxy_refusal_stops_the_run(tmp_path):
    """A 402 from the proxy is not retried and the script exits non-zero."""
    import threading
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    hits = []

    class H(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def do_POST(self):
            hits.append(self.path)
            self.rfile.read(int(self.headers.get("Content-Length", 0)))
            b = json.dumps({"error": {"code": "cap_reached", "message": "spending cap reached"}}).encode()
            self.send_response(402)
            self.send_header("Content-Length", str(len(b)))
            self.end_headers()
            self.wfile.write(b)

    srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        ds, sample = data.write(str(tmp_path))
        rc = answer.main(["--condition", "A", "--dataset", ds, "--sample", sample, "--workers", "1",
                          "--proxy", f"http://127.0.0.1:{srv.server_address[1]}", "--out-dir", str(tmp_path)])
        assert rc == 1
        assert len(hits) == 1  # no retry, remaining questions not sent
        assert not os.path.exists(os.path.join(tmp_path, "A.answers.jsonl"))
    finally:
        srv.shutdown()
        srv.server_close()
