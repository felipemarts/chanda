"""Condition C: Mem0 open source (mem0ai, pinned in requirements.txt).

For each sampled question:
  1. a fresh Mem0 Memory in its own directory (local Qdrant on disk + SQLite
     history; no external service), user_id = question id;
  2. every haystack session added in dataset order with one Memory.add call
     (infer=True: Mem0's single LLM extraction call per add). The LLM is
     DeepSeek through lmeproxy (tag C-ingest; the proxy forces temperature 0
     and non-thinking mode, and we also configure temperature 0). Embeddings
     come from an OpenAI-compatible endpoint (our local embedding server
     serving Chanda's own embedder);
  3. Memory.search(question), then the ranked memories are packed into a
     context block of at most 2,000 tokens (ceil(chars/4)): items are taken
     in rank order, an item that does not fit is skipped, and the chosen
     lines are then printed in chronological order (as the official reader
     pipeline sorts retrieved sessions by date). Each line is
     "[<session date>] <memory>".

Session dates (Mem0 OSS rejects add(timestamp=...): "Platform-only
temporal parameter. Not supported in OSS"):
  - always: metadata created_at = session date (ISO) and session_date /
    session_id, so stored memories carry the event date;
  - --date-mode observation (default): the extraction prompt's
    "Observation Date" and "Current Date" sections are set to the session
    date. Mem0's prompt is built to resolve relative dates against
    Observation Date, but Memory.add in OSS never passes one (it defaults to
    today); we set it by replacing mem0.configs.prompts._resolve_dates for
    the duration of each add. This patches an internal function, no prompt
    text is changed;
  - --date-mode instructions: public API only; the date goes in add(prompt=...)
    (Mem0's "Custom Instructions" section) while Observation Date stays today;
  - --date-mode none: metadata only.

Output: one JSONL line per question, same shape as the Go context generator:
  {"question_id", "condition": "C", "context", "context_tokens",
   "ingest_seconds", "retrieval_latency_ms", ...extra diagnostics}
Resumable: questions already in --out are skipped. Per-question databases
are deleted after measuring their size unless --keep-db.
"""

from __future__ import annotations

import argparse
import contextlib
import logging
import os
import shutil
import sys
import time

import common

BUDGET = 2000


def _setup_env(work_dir: str) -> None:
    # Must run before mem0 is imported: telemetry (PostHog) and the remote
    # notice config fetch are both gated on MEM0_TELEMETRY.
    os.environ["MEM0_TELEMETRY"] = "False"
    os.environ.setdefault("MEM0_DIR", os.path.join(work_dir, ".mem0"))
    os.makedirs(os.environ["MEM0_DIR"], exist_ok=True)
    for k in ("OPENROUTER_API_KEY", "OPENAI_BASE_URL", "OPENAI_API_BASE", "DEEPSEEK_API_BASE"):
        os.environ.pop(k, None)


class _Counter(logging.Handler):
    """Counts Mem0's swallowed extraction errors so they are reported."""

    def __init__(self) -> None:
        super().__init__(level=logging.WARNING)
        self.parse_errors = 0
        self.other = 0
        self.messages: list[str] = []

    def emit(self, record: logging.LogRecord) -> None:
        msg = record.getMessage()
        if "Error parsing extraction response" in msg:
            self.parse_errors += 1
        else:
            self.other += 1
        if len(self.messages) < 20:
            self.messages.append(msg[:300])


def mem0_config(qdir: str, *, llm_base: str, tag: str, model: str, embed_url: str, embed_model: str,
                dims: int, max_tokens: int = 2000) -> dict:
    return {
        "vector_store": {
            "provider": "qdrant",
            "config": {"collection_name": "lme", "path": os.path.join(qdir, "qdrant"),
                       "on_disk": True, "embedding_model_dims": dims},
        },
        "llm": {
            "provider": "deepseek",
            "config": {"model": model, "temperature": 0.0, "api_key": "via-lmeproxy",
                       "deepseek_base_url": f"{llm_base.rstrip('/')}/tag/{tag}/v1", "max_tokens": max_tokens},
        },
        "embedder": {
            "provider": "openai",
            "config": {"model": embed_model, "api_key": "local", "openai_base_url": embed_url,
                       "embedding_dims": dims},
        },
        "history_db_path": os.path.join(qdir, "history.db"),
    }


@contextlib.contextmanager
def observation_date(date_iso: str | None):
    """Set Mem0's Observation Date / Current Date to date_iso during add."""
    if not date_iso:
        yield
        return
    import mem0.configs.prompts as m0p

    orig = m0p._resolve_dates

    def resolved(current_date=None, observation_date=None):
        return (current_date or date_iso, observation_date or date_iso)

    m0p._resolve_dates = resolved
    try:
        yield
    finally:
        m0p._resolve_dates = orig


def pack(results: list[dict], budget: int) -> tuple[str, int]:
    """Greedy by rank, skip what does not fit, then chronological order."""
    chosen: list[tuple[str, int, str]] = []  # (sort key, rank, line)
    lines: list[str] = []
    for rank, r in enumerate(results):
        meta = r.get("metadata") or {}
        date = meta.get("session_date") or r.get("created_at") or ""
        line = f"[{date}] {r.get('memory', '').strip()}" if date else r.get("memory", "").strip()
        if not line:
            continue
        if common.est_tokens("\n".join(lines + [line])) > budget:
            continue
        lines.append(line)
        key = r.get("created_at") or ""
        chosen.append((key, rank, line))
    chosen.sort(key=lambda t: (t[0], t[1]))
    return "\n".join(t[2] for t in chosen), len(chosen)


def dir_bytes(path: str) -> int:
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            with contextlib.suppress(OSError):
                total += os.path.getsize(os.path.join(root, f))
    return total


def run_question(q: dict, args, counter: _Counter) -> dict:
    from mem0 import Memory

    qid = q["question_id"]
    qdir = os.path.join(args.work_dir, "db", qid)
    shutil.rmtree(qdir, ignore_errors=True)
    os.makedirs(qdir)
    cfg = mem0_config(qdir, llm_base=args.proxy, tag=args.tag, model=args.model, embed_url=args.embed_url,
                      embed_model=args.embed_model, dims=args.embed_dims, max_tokens=args.extract_max_tokens)
    m = Memory.from_config(cfg)
    parse0, other0 = counter.parse_errors, counter.other
    added = 0
    t0 = time.perf_counter()
    try:
        for sid, date, sess in zip(q["haystack_session_ids"], q["haystack_dates"], q["haystack_sessions"]):
            when = common.parse_lme_date(date)
            msgs = [{"role": t["role"], "content": t["content"]} for t in sess]  # has_answer dropped
            meta = {"created_at": when.isoformat(), "session_date": date, "session_id": sid}
            kw = {}
            obs = None
            if args.date_mode == "observation":
                obs = when.date().isoformat()
            elif args.date_mode == "instructions":
                kw["prompt"] = (f"These messages were exchanged on {date}. Resolve relative time references "
                                f"against {when.date().isoformat()}, not against today's date.")
            with observation_date(obs):
                res = m.add(msgs, user_id=qid, metadata=meta, **kw)
            added += len(res.get("results", []))
        ingest_s = time.perf_counter() - t0
        t1 = time.perf_counter()
        found = m.search(q["question"], top_k=args.top_k, filters={"user_id": qid}, threshold=args.threshold)
        results = found.get("results", [])
        context, n_used = pack(results, args.budget)
        retrieval_ms = (time.perf_counter() - t1) * 1000
        total_mem = len(m.get_all(filters={"user_id": qid}, top_k=100000).get("results", []))
    finally:
        with contextlib.suppress(Exception):
            m.close()
        for store in (getattr(m, "vector_store", None), getattr(m, "_entity_store", None)):
            with contextlib.suppress(Exception):
                store.client.close()
    db = dir_bytes(qdir)
    if not args.keep_db:
        shutil.rmtree(qdir, ignore_errors=True)
    return {
        "question_id": qid,
        "condition": "C",
        "context": context,
        "context_tokens": common.est_tokens(context),
        "ingest_seconds": round(ingest_s, 3),
        "retrieval_latency_ms": round(retrieval_ms, 2),
        "sessions": len(q["haystack_sessions"]),
        "memories_added": added,
        "memories_total": total_mem,
        "search_results": len(results),
        "memories_in_context": n_used,
        "extraction_parse_errors": counter.parse_errors - parse0,
        "mem0_warnings": counter.other - other0,
        "db_bytes": db,
        "date_mode": args.date_mode,
        "embed_model": args.embed_model,
        "embed_dims": args.embed_dims,
        "mem0_version": _mem0_version(),
    }


def _mem0_version() -> str:
    from importlib.metadata import version

    return version("mem0ai")


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--sample", required=True)
    ap.add_argument("--limit", type=int)
    ap.add_argument("--questions", nargs="*")
    ap.add_argument("--proxy", default="http://127.0.0.1:8787", help="lmeproxy base URL")
    ap.add_argument("--tag", default="C-ingest")
    ap.add_argument("--model", default=common.DEFAULT_MODEL)
    ap.add_argument("--extract-max-tokens", type=int, default=2000, help="Mem0's default max_tokens")
    ap.add_argument("--embed-url", required=True, help="OpenAI-compatible embeddings base URL, e.g. http://127.0.0.1:8790/v1")
    ap.add_argument("--embed-model", required=True, help="model name sent to the embedding server")
    ap.add_argument("--embed-dims", type=int, required=True, help="hash = 256, e5-small = 384")
    ap.add_argument("--work-dir", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "work", "mem0"))
    ap.add_argument("--out", required=True, help="context JSONL for condition C")
    ap.add_argument("--budget", type=int, default=BUDGET)
    ap.add_argument("--top-k", type=int, default=100)
    ap.add_argument("--threshold", type=float, default=0.0, help="Mem0 default is 0.1; 0 lets the budget decide")
    ap.add_argument("--date-mode", choices=["observation", "instructions", "none"], default="observation")
    ap.add_argument("--keep-db", action="store_true")
    args = ap.parse_args(argv)

    _setup_env(args.work_dir)
    counter = _Counter()
    logging.getLogger("mem0.memory.main").addHandler(counter)
    data = common.load_dataset(args.dataset)
    ids = common.select_ids(common.load_sample_ids(args.sample), args.limit, args.questions)
    done = common.done_ids(args.out)
    for qid in ids:
        if qid in done:
            continue
        try:
            rec = run_question(data[qid], args, counter)
        except Exception as e:  # noqa: BLE001
            # A proxy refusal surfaces as Mem0's LLMError: stop, never skip silently.
            print(f"STOP on {qid}: {type(e).__name__}: {e}", file=sys.stderr)
            return 1
        common.append_jsonl(args.out, rec)
        print(f"{qid}: {rec['memories_total']} memories, context {rec['context_tokens']} tok, "
              f"ingest {rec['ingest_seconds']}s, parse errors {rec['extraction_parse_errors']}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
