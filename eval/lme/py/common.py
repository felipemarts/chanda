"""Shared helpers for the LongMemEval battery scripts (stdlib only).

Token estimator: ceil(chars / 4) over Unicode code points, the same as
Chanda's llm.EstimateTokens ((runes + 3) / 4), used for every condition so
that the 2,000-token context budgets are comparable.
"""

from __future__ import annotations

import datetime as _dt
import json
import os
import time
import urllib.error
import urllib.request
from typing import Any, Iterable, Iterator

TAG_HEADER = "X-Chanda-Eval-Tag"
DEFAULT_MODEL = "deepseek-flash"
CONDITIONS = ("A", "B", "C", "D", "E")


def est_tokens(text: str) -> int:
    """ceil(len(text) / 4) over code points (Chanda's estimator)."""
    return (len(text) + 3) // 4


# ---------------------------------------------------------------- JSONL


def read_jsonl(path: str) -> list[dict]:
    out = []
    with open(path, encoding="utf-8") as f:
        for n, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except json.JSONDecodeError as e:
                raise ValueError(f"{path}:{n}: {e}") from e
    return out


def append_jsonl(path: str, obj: dict) -> None:
    """Append one line and fsync, so a crash never loses a paid result."""
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    with open(path, "a", encoding="utf-8") as f:
        f.write(json.dumps(obj, ensure_ascii=False) + "\n")
        f.flush()
        os.fsync(f.fileno())


def done_ids(path: str) -> set[str]:
    """question_ids already present in an output file (for resuming)."""
    if not os.path.exists(path):
        return set()
    return {r["question_id"] for r in read_jsonl(path)}


# ---------------------------------------------------------------- dataset


def load_dataset(path: str) -> dict[str, dict]:
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    return {q["question_id"]: q for q in data}


def load_sample_ids(path: str) -> list[str]:
    """Question ids of eval/sample.json in file order (the order is the
    sample's: "the first 10 questions" are the first 10 entries)."""
    with open(path, encoding="utf-8") as f:
        s = json.load(f)
    qs = s["questions"] if isinstance(s, dict) else s
    return [q["question_id"] if isinstance(q, dict) else q for q in qs]


def select_ids(sample_ids: list[str], limit: int | None, only: Iterable[str] | None) -> list[str]:
    ids = list(sample_ids)
    if only:
        wanted = set(only)
        ids = [i for i in ids if i in wanted]
    if limit is not None:
        ids = ids[:limit]
    return ids


def gold_text(answer: Any) -> str:
    return answer if isinstance(answer, str) else json.dumps(answer)


def is_abstention(question_id: str) -> bool:
    # The official evaluate_qa.py tests the substring: '_abs' in question_id.
    return "_abs" in question_id


def parse_lme_date(s: str) -> _dt.datetime:
    """'2023/05/20 (Sat) 02:21' -> datetime(2023, 5, 20, 2, 21)."""
    day, _, rest = s.partition(" (")
    hhmm = rest.partition(") ")[2] or "00:00"
    return _dt.datetime.strptime(f"{day} {hhmm}", "%Y/%m/%d %H:%M")


# ---------------------------------------------------------------- LLM calls


class ProxyRefused(RuntimeError):
    """The proxy refused the call (cap reached, outside the time window, or
    a rejected request). Never retried; the run must stop."""


def chat_url(base: str, tag: str) -> str:
    return f"{base.rstrip('/')}/tag/{tag}/v1/chat/completions"


def chat(
    base: str,
    tag: str,
    messages: list[dict],
    *,
    model: str = DEFAULT_MODEL,
    max_tokens: int,
    timeout: float = 600.0,
    retries: int = 3,
) -> dict:
    """One chat completion through lmeproxy at temperature 0.

    Returns {"content", "usage", "latency_ms", "finish_reason", "cost_usd"}.
    429/5xx and connection errors are retried (with backoff); 400/402/403 are
    raised as ProxyRefused immediately.
    """
    body = json.dumps(
        {"model": model, "messages": messages, "temperature": 0, "max_tokens": max_tokens, "stream": False}
    ).encode()
    url = chat_url(base, tag)
    last: Exception | None = None
    for attempt in range(retries + 1):
        req = urllib.request.Request(
            url,
            data=body,
            method="POST",
            headers={"Content-Type": "application/json", TAG_HEADER: tag},
        )
        t0 = time.perf_counter()
        try:
            with urllib.request.urlopen(req, timeout=timeout) as res:
                raw = res.read()
                cost = res.headers.get("X-Lmeproxy-Cost-Usd")
            latency = (time.perf_counter() - t0) * 1000
            r = json.loads(raw.decode().strip())
            ch = (r.get("choices") or [{}])[0]
            content = ((ch.get("message") or {}).get("content")) or ""
            return {
                "content": content,
                "usage": r.get("usage") or {},
                "latency_ms": round(latency, 1),
                "finish_reason": ch.get("finish_reason"),
                "cost_usd": float(cost) if cost else None,
            }
        except urllib.error.HTTPError as e:
            detail = e.read().decode(errors="replace")[:500]
            if e.code in (400, 401, 402, 403, 404, 413):
                raise ProxyRefused(f"HTTP {e.code} from {url}: {detail}") from e
            last = RuntimeError(f"HTTP {e.code} from {url}: {detail}")
        except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
            last = e
        if attempt < retries:
            time.sleep(min(30.0, 2.0 * 2**attempt))
    raise RuntimeError(f"chat failed after {retries + 1} attempts: {last}")


def usage_tokens(u: dict) -> dict:
    """Normalise a usage block to prompt / cache_hit / completion counts."""
    hit = u.get("prompt_cache_hit_tokens")
    if hit is None:
        hit = (u.get("prompt_tokens_details") or {}).get("cached_tokens", 0)
    return {
        "prompt": int(u.get("prompt_tokens", 0)),
        "cache_hit": int(hit or 0),
        "completion": int(u.get("completion_tokens", 0)),
    }


def today() -> str:
    return _dt.date.today().isoformat()
