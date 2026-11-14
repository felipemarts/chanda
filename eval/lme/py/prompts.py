"""Official LongMemEval prompts, copied VERBATIM.

Source: https://github.com/xiaowu0162/LongMemEval at commit
9e0b455f4ef0e2ab8f2e582289761153549043fc (HEAD of main, committed 2026-05-11, read 2026-11-14).

- Reader templates: src/generation/run_generation.py, prepare_prompt(),
  lines 48-67 (file sha256 4f1eb3c69d7ad40f04065b9c0bc86f6582441018fc6ff751d162d66c95baf672).
  Official defaults (src/generation/run_generation.sh and README): reading
  method "con" (cot=true), gen_length 800 tokens with CoT and 500 without
  (run_generation.py line 342), temperature 0, n 1, one user message.
- Judge templates: src/evaluation/evaluate_qa.py, get_anscheck_prompt(),
  lines 24-43 (file sha256 ecce9c4c79dc89d99534ac17b383a5cbb5b9f0c69ee98adaf0684742e3d95251).
  Official call: one user message, temperature 0, max_tokens 10, n 1; the
  verdict is ``'yes' in response.lower()`` (line 113). Abstention is chosen by
  ``'_abs' in question_id`` (line 101) and then uses the abstention template
  regardless of question_type, with the gold "answer" field (an explanation)
  in the Explanation slot.

The literals below were extracted programmatically from the files above
(ast parse, repr) — do not edit them by hand.
"""

from __future__ import annotations

LONGMEMEVAL_COMMIT = "9e0b455f4ef0e2ab8f2e582289761153549043fc"
READER_SOURCE = "src/generation/run_generation.py"
JUDGE_SOURCE = "src/evaluation/evaluate_qa.py"
READER_SOURCE_SHA256 = "4f1eb3c69d7ad40f04065b9c0bc86f6582441018fc6ff751d162d66c95baf672"
JUDGE_SOURCE_SHA256 = "ecce9c4c79dc89d99534ac17b383a5cbb5b9f0c69ee98adaf0684742e3d95251"

# ----------------------------------------------------------------- reader
# run_generation.py line 48 (+ line 50 when cot): retriever_type == 'no-retrieval'
READER_NO_RETRIEVAL = '{}'
READER_NO_RETRIEVAL_COT_SUFFIX = 'Answer step by step.'
# line 55: merge_key_expansion_into_value == 'none', cot
READER_HISTORY_COT = 'I will give you several history chats between you and a user. Please answer the question based on the relevant chat history. Answer the question step by step: first extract all the relevant information, and then reason over the information to get the answer.\n\n\nHistory Chats:\n\n{}\n\nCurrent Date: {}\nQuestion: {}\nAnswer (step by step):'
# line 57: merge_key_expansion_into_value == 'none', no cot
READER_HISTORY_DIRECT = 'I will give you several history chats between you and a user. Please answer the question based on the relevant chat history.\n\n\nHistory Chats:\n\n{}\n\nCurrent Date: {}\nQuestion: {}\nAnswer:'
# line 65 / 67: merge_key_expansion_into_value == 'replace' (facts only), cot / no cot (not used by default)
READER_FACTS_COT = 'I will give you several facts extracted from history chats between you and a user. Please answer the question based on the relevant facts. Answer the question step by step: first extract all the relevant information, and then reason over the information to get the answer.\n\n\nHistory Chats:\n\n{}\n\nCurrent Date: {}\nQuestion: {}\nAnswer (step by step):'
READER_FACTS_DIRECT = 'I will give you several facts extracted from history chats between you and a user. Please answer the question based on the relevant facts.\n\n\nHistory Chats:\n\n{}\n\nCurrent Date: {}\nQuestion: {}\nAnswer:'

GEN_LENGTH_COT = 800     # line 342
GEN_LENGTH_DIRECT = 500  # line 342

# ------------------------------------------------------------------ judge
# evaluate_qa.py line 27: single-session-user, single-session-assistant, multi-session
JUDGE_DEFAULT = 'I will give you a question, a correct answer, and a response from a model. Please answer yes if the response contains the correct answer. Otherwise, answer no. If the response is equivalent to the correct answer or contains all the intermediate steps to get the correct answer, you should also answer yes. If the response only contains a subset of the information required by the answer, answer no. \n\nQuestion: {}\n\nCorrect Answer: {}\n\nModel Response: {}\n\nIs the model response correct? Answer yes or no only.'
# line 30
JUDGE_TEMPORAL = "I will give you a question, a correct answer, and a response from a model. Please answer yes if the response contains the correct answer. Otherwise, answer no. If the response is equivalent to the correct answer or contains all the intermediate steps to get the correct answer, you should also answer yes. If the response only contains a subset of the information required by the answer, answer no. In addition, do not penalize off-by-one errors for the number of days. If the question asks for the number of days/weeks/months, etc., and the model makes off-by-one errors (e.g., predicting 19 days when the answer is 18), the model's response is still correct. \n\nQuestion: {}\n\nCorrect Answer: {}\n\nModel Response: {}\n\nIs the model response correct? Answer yes or no only."
# line 33
JUDGE_KNOWLEDGE_UPDATE = 'I will give you a question, a correct answer, and a response from a model. Please answer yes if the response contains the correct answer. Otherwise, answer no. If the response contains some previous information along with an updated answer, the response should be considered as correct as long as the updated answer is the required answer.\n\nQuestion: {}\n\nCorrect Answer: {}\n\nModel Response: {}\n\nIs the model response correct? Answer yes or no only.'
# line 36
JUDGE_PREFERENCE = "I will give you a question, a rubric for desired personalized response, and a response from a model. Please answer yes if the response satisfies the desired response. Otherwise, answer no. The model does not need to reflect all the points in the rubric. The response is correct as long as it recalls and utilizes the user's personal information correctly.\n\nQuestion: {}\n\nRubric: {}\n\nModel Response: {}\n\nIs the model response correct? Answer yes or no only."
# line 41: any question whose id contains '_abs'
JUDGE_ABSTENTION = 'I will give you an unanswerable question, an explanation, and a response from a model. Please answer yes if the model correctly identifies the question as unanswerable. The model could say that the information is incomplete, or some other information is given but the asked information is not.\n\nQuestion: {}\n\nExplanation: {}\n\nModel Response: {}\n\nDoes the model correctly identify the question as unanswerable? Answer yes or no only.'

JUDGE_MAX_TOKENS = 10  # line 109


def reader_prompt(context: str, question_date: str, question: str, *, cot: bool = True,
                  no_retrieval_template: bool = False) -> str:
    """Build the reader prompt.

    Every condition uses the same history template (owner rule 3: same answer
    prompt for all conditions); the retrieved context is the History Chats
    block, verbatim. Condition A passes an empty context. With
    no_retrieval_template=True, an empty-context call uses the official
    'no-retrieval' template instead (question only), as run_generation.py does.
    """
    if no_retrieval_template:
        if context:
            raise ValueError("no_retrieval_template is only for an empty context")
        t = READER_NO_RETRIEVAL + (READER_NO_RETRIEVAL_COT_SUFFIX if cot else "")
        return t.format(question)
    t = READER_HISTORY_COT if cot else READER_HISTORY_DIRECT
    return t.format(context, question_date, question)


def judge_prompt(task: str, question: str, answer: str, response: str, abstention: bool = False) -> str:
    """Port of get_anscheck_prompt (evaluate_qa.py lines 24-43), same branches."""
    if not abstention:
        if task in ["single-session-user", "single-session-assistant", "multi-session"]:
            template = JUDGE_DEFAULT
        elif task == "temporal-reasoning":
            template = JUDGE_TEMPORAL
        elif task == "knowledge-update":
            template = JUDGE_KNOWLEDGE_UPDATE
        elif task == "single-session-preference":
            template = JUDGE_PREFERENCE
        else:
            raise NotImplementedError(task)
    else:
        template = JUDGE_ABSTENTION
    return template.format(question, answer, response)


def parse_verdict(response: str) -> bool:
    """evaluate_qa.py line 113: label = 'yes' in eval_response.lower()."""
    return "yes" in response.strip().lower()
