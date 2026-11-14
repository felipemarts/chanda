"""A tiny LongMemEval-shaped dataset for offline tests."""

from __future__ import annotations

import json
import os

QUESTIONS = [
    {
        "question_id": "q1",
        "question_type": "single-session-user",
        "question": "What breed is my dog?",
        "answer": "Golden retriever",
        "question_date": "2023/05/30 (Tue) 23:40",
        "haystack_session_ids": ["s1", "s2"],
        "haystack_dates": ["2023/05/20 (Sat) 02:21", "2023/05/22 (Mon) 10:00"],
        "haystack_sessions": [
            [{"role": "user", "content": "I just adopted a golden retriever puppy named Max.", "has_answer": True},
             {"role": "assistant", "content": "Congratulations on adopting Max!"}],
            [{"role": "user", "content": "Can you suggest a pasta recipe with tomatoes?"},
             {"role": "assistant", "content": "Try a simple pomodoro sauce."}],
        ],
        "answer_session_ids": ["s1"],
    },
    {
        "question_id": "q2_abs",
        "question_type": "temporal-reasoning",
        "question": "How many days ago did I buy my cat?",
        "answer": "The user never mentioned buying a cat.",
        "question_date": "2023/06/01 (Thu) 09:00",
        "haystack_session_ids": ["s3"],
        "haystack_dates": ["2023/05/25 (Thu) 08:00"],
        "haystack_sessions": [
            [{"role": "user", "content": "I bought new running shoes yesterday."},
             {"role": "assistant", "content": "Nice, enjoy your runs!"}],
        ],
        "answer_session_ids": ["s3"],
    },
    {
        "question_id": "q3",
        "question_type": "knowledge-update",
        "question": "Where do I work now?",
        "answer": 42,
        "question_date": "2023/07/01 (Sat) 09:00",
        "haystack_session_ids": ["s4"],
        "haystack_dates": ["2023/06/25 (Sun) 08:00"],
        "haystack_sessions": [
            [{"role": "user", "content": "I moved from Acme to Globex last week."},
             {"role": "assistant", "content": "Good luck at Globex."}],
        ],
        "answer_session_ids": ["s4"],
    },
]


def write(tmp: str) -> tuple[str, str]:
    ds = os.path.join(tmp, "lme_s.json")
    sample = os.path.join(tmp, "sample.json")
    with open(ds, "w") as f:
        json.dump(QUESTIONS, f)
    with open(sample, "w") as f:
        json.dump({"seed": 42, "n": 3, "questions": [
            {"question_id": "q2_abs", "question_type": "temporal-reasoning", "abstention": True},
            {"question_id": "q1", "question_type": "single-session-user", "abstention": False},
            {"question_id": "q3", "question_type": "knowledge-update", "abstention": False},
        ]}, f)
    return ds, sample
