"""Reader and judge prompts of the LoCoMo sleep transfer test, VERBATIM from
eval/sleep/PROTOCOL.md ("Reader and judge", pre-registered in dfb68b0).
tests/test_locomo_prompts.py checks them against the protocol's code blocks;
do not edit them here without changing the protocol.

Both go as one user message, no system message, temperature 0, through
lmeproxy: the reader with max_tokens 200, the judge with max_tokens 5.
"""

from __future__ import annotations

import re

READER_MAX_TOKENS = 200
JUDGE_MAX_TOKENS = 5

READER = (
    "Below are memories retrieved from a long conversation between {speaker_a} and {speaker_b}. "
    "Each memory may carry a date.\n"
    "\n"
    "{context}\n"
    "\n"
    "Answer the question using only these memories. When the question is about time, use the dates "
    "shown to give the date or period as precisely as the memories allow. Answer with a short phrase. "
    "If the memories do not contain the answer, answer \"Not mentioned\".\n"
    "\n"
    "Question: {question}\n"
    "Answer:"
)

JUDGE = (
    "You are grading an answer to a question about a conversation.\n"
    "\n"
    "Question: {question}\n"
    "Gold answer: {gold}\n"
    "Generated answer: {answer}\n"
    "\n"
    "The generated answer is CORRECT if it states the same thing as the gold answer, even if it is longer "
    "or worded differently. For a date or a time, it is CORRECT if it refers to the same date or period, "
    "in any format. It is WRONG if it contradicts the gold answer, leaves out its key content, or says the "
    "information is not available.\n"
    "\n"
    "Reply with one word: CORRECT or WRONG."
)


def _fill(template: str, **kw: str) -> str:
    # Plain substitution, not str.format: the context and the answer may hold
    # braces, and every placeholder appears exactly once.
    out = template
    for k, v in kw.items():
        key = "{" + k + "}"
        if out.count(key) != 1:
            raise ValueError(f"placeholder {key} appears {out.count(key)} times")
        out = out.replace(key, v)
    return out


def reader_prompt(speaker_a: str, speaker_b: str, context: str, question: str) -> str:
    return _fill(READER, speaker_a=speaker_a, speaker_b=speaker_b, context=context, question=question)


def judge_prompt(question: str, gold: str, answer: str) -> str:
    return _fill(JUDGE, question=question, gold=gold, answer=answer)


_WORD = re.compile(r"[A-Z]+")


def parse_verdict(raw: str) -> tuple[bool, str]:
    """(correct, how) from the judge's reply.

    how is "exact" when the first word is CORRECT or WRONG (INCORRECT counts
    as WRONG), "fallback" when only one of the two words appears somewhere,
    and "unparsed" otherwise (scored WRONG, and counted in the report).
    """
    words = _WORD.findall((raw or "").upper())
    if words:
        if words[0] == "CORRECT":
            return True, "exact"
        if words[0] in ("WRONG", "INCORRECT"):
            return False, "exact"
    has_c = "CORRECT" in words
    has_w = "WRONG" in words or "INCORRECT" in words
    if has_c and not has_w:
        return True, "fallback"
    if has_w and not has_c:
        return False, "fallback"
    return False, "unparsed"
