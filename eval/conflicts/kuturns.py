"""Outdated and current evidence turns of LongMemEval knowledge-update
questions (eval/conflicts/PROTOCOL.md). LongMemEval does not label them
(its README only says has_answer marks "the turns that contain the required
evidence"); the rule here infers them from the session dates:

- a question qualifies when it is not an abstention question and has exactly
  two has_answer turns in sessions with different timestamps;
- the turn of the earlier session is the OUTDATED one, the later the CURRENT;
- when the two sessions fall on the same calendar day, the order is not
  trusted (in 618f13b2 the earlier turn holds the gold value): both turns
  are needed and the question is left out of the outdated-delivery measure;
- a question "asks for the old value" when its text matches OLD_RE
  (previous, before, initially, originally, used to, first ...); then the
  outdated turn is the needed evidence (and the current one too when the
  question also asks for "now"/"currently").
"""
import re

OLD_RE = re.compile(r"\b(previous|previously|before|initially|originally|used to|at first|first)\b", re.I)
NOW_RE = re.compile(r"\b(now|currently|current)\b", re.I)


def ku_turns(q):
    """(outdated, current) as (session, turn) or None when the question does not qualify."""
    if q["question_id"].endswith("_abs") or q["question_type"] != "knowledge-update":
        return None
    ev = [(q["haystack_dates"][s], s, t) for s, sess in enumerate(q["haystack_sessions"])
          for t, tr in enumerate(sess) if tr.get("has_answer")]
    if len(ev) != 2 or ev[0][0] == ev[1][0]:
        return None
    ev.sort()
    return (ev[0][1], ev[0][2]), (ev[1][1], ev[1][2])


def needed(q):
    """The evidence turns the question needs: the current one, or the outdated
    one (and the current one) when it asks for the old value."""
    k = ku_turns(q)
    if k is None:
        return None
    old, cur = k
    if same_day(q):
        return [old, cur]
    if OLD_RE.search(q["question"]):
        return [old, cur] if NOW_RE.search(q["question"]) else [old]
    return [cur]


def same_day(q):
    k = ku_turns(q)
    return k is not None and q["haystack_dates"][k[0][0]][:10] == q["haystack_dates"][k[1][0]][:10]


def stale_measure(q):
    """True when the question counts in the outdated-delivery measure: it
    qualifies, needs only the current turn, and its sessions are on
    different days."""
    k = ku_turns(q)
    return k is not None and not same_day(q) and needed(q) == [k[1]]
