# LoCoMo dataset: source note

## Origin

- Repository: https://github.com/snap-research/locomo (default branch `main`).
- Commit: `3eb6f2c585f5e1699204e3c3bdf7adc5c28cb376` ("Updated Readme", 2024-08-13). This is the latest commit; the repo has not been pushed since.
- File: `data/locomo10.json`, fetched from `https://raw.githubusercontent.com/snap-research/locomo/3eb6f2c585f5e1699204e3c3bdf7adc5c28cb376/data/locomo10.json` into `eval/locomo/data/locomo10.json`.
- Download date: 2026-11-15.
- Size: 2,805,274 bytes. sha256: `79fa87e90f04081343b8c8debecb80a9a6842b76a7aa537dc9fdf651ea698ff4`. The git blob hash (`d95b872480b413d935821fdc3c84f8a8f5f29e73`) matches the blob listed by the GitHub API at that commit.
- Paper: Maharana et al., "Evaluating Very Long-Term Conversational Memory of LLM Agents", ACL 2024, arXiv:2402.17753.

## License

`LICENSE.txt` in the repo is Creative Commons Attribution-NonCommercial 4.0 International (CC BY-NC 4.0). GitHub reports it as "Other" (NOASSERTION). Research use is allowed with attribution; commercial use is not. Redistribution is allowed only under the same non-commercial terms with attribution. For this reason the data directory is git-ignored (`/eval/locomo/data/`) and the file is not committed; fetch it again from the URL above and check the sha256.

## Format

The file is a JSON list of 10 samples (conversations). Each sample has the keys `sample_id`, `conversation`, `qa`, `observation`, `session_summary`, `event_summary`.

`conversation` holds `speaker_a`, `speaker_b`, the sessions `session_<n>` (a list of turns, n from 1, contiguous) and `session_<n>_date_time` (a string such as `1:56 pm on 8 May, 2023`; sessions are in chronological order). In conv-26 there are 16 extra keys `session_20_date_time` to `session_35_date_time` that have no matching session; only sessions 1 to 19 exist there.

A turn has `speaker`, `dia_id` (`D<session>:<turn>`, for example `D1:3`, unique within a conversation) and `text`. Turns that share a photo also have `blip_caption` (a BLIP caption of the image), and usually `img_url` (a list of URLs) and `query` (the image search query). Some have `re-download: true`. Some turns have `blip_caption` without `img_url`. The images themselves are not released.

| sample_id | sessions | turns | speakers | QA cat 1 | cat 2 | cat 3 | cat 4 | cat 5 | QA total |
|---|---|---|---|---|---|---|---|---|---|
| conv-26 | 19 | 419 | Caroline, Melanie | 32 | 37 | 13 | 70 | 47 | 199 |
| conv-30 | 19 | 369 | Jon, Gina | 11 | 26 | 0 | 44 | 24 | 105 |
| conv-41 | 32 | 663 | John, Maria | 31 | 27 | 8 | 86 | 41 | 193 |
| conv-42 | 29 | 629 | Joanna, Nate | 37 | 40 | 11 | 111 | 61 | 260 |
| conv-43 | 29 | 680 | Tim, John | 31 | 26 | 14 | 107 | 64 | 242 |
| conv-44 | 28 | 675 | Audrey, Andrew | 30 | 24 | 7 | 62 | 35 | 158 |
| conv-47 | 31 | 689 | James, John | 20 | 34 | 13 | 83 | 40 | 190 |
| conv-48 | 30 | 681 | Deborah, Jolene | 21 | 42 | 10 | 118 | 48 | 239 |
| conv-49 | 25 | 509 | Evan, Sam | 37 | 33 | 13 | 73 | 40 | 196 |
| conv-50 | 30 | 568 | Calvin, Dave | 32 | 32 | 7 | 87 | 46 | 204 |
| total | 272 | 5882 | | 282 | 321 | 96 | 841 | 446 | 1986 |

A QA item has `question`, `answer`, `evidence` (a list of `dia_id` strings) and `category` (integer 1 to 5). Category 5 items have `adversarial_answer` (a plausible but wrong answer) and, in 444 of 446 cases, no `answer` key; the expected behaviour is to say the information is not in the conversation. Two category 5 items in conv-26 have both `answer` ("No") and `adversarial_answer` ("Yes"). Six answers are integers rather than strings (4 in category 2, 2 in category 1).

## Category mapping

Neither the README nor the paper states which integer belongs to which category, and the paper's numbered list (single-hop, multi-hop, temporal, open-domain, adversarial) does not follow the integers in the data. The mapping used here is:

- 1: multi-hop. Source: comment in `task_eval/evaluation.py` (`elif line['category'] in [1]:` under "multi-hop eval"). In the data, 95% of these questions have evidence in more than one session.
- 2: temporal. Source: `task_eval/gpt_utils.py` appends "Use DATE of CONVERSATION to answer with an approximate date." only to category 2. In the data, 77% of these questions start with "When".
- 3: open-domain (commonsense or world knowledge inference). Source: inference from the data; the code only groups 2, 3 and 4 under the comment "single-hop, temporal, open-domain". Questions are of the form "Would X likely ...", and `evaluation.py` keeps only the part of the answer before `;` for this category.
- 4: single-hop. Source: inference from the data (95% have exactly one evidence turn, none span sessions).
- 5: adversarial. Source: comment in `task_eval/evaluation.py` ("adversarial eval"), which scores an output as correct when it contains "not mentioned" or "no information available".

The same mapping is given in open GitHub issue #6 and in issue #29 (closed by its author as a duplicate). The maintainers have not answered either. Categories 3 and 4 are the least certain.

## Evidence marking

Every QA has an `evidence` field. Four category 3 items have an empty list (two in conv-26, two in conv-50). Nine evidence entries do not resolve to an existing `dia_id`: some are malformed (`D8:6; D9:17`, `D:11:26`, `D`, `D30:05`, and three entries in conv-49 with several ids in one string separated by spaces) and some point to turns that do not exist (`D10:19` in conv-42, `D4:36` in conv-47). Several open issues on the repo (#27, #35, #43, #44) report wrong ground-truth answers and mislabeled category 5 questions; none of these corrections are in this commit.

## Dataset-provided summaries (not for memory systems)

`observation` (`session_<n>_observation`: per-speaker lists of [fact, dia_id] pairs), `session_summary` (`session_<n>_summary`: one prose summary per session) and `event_summary` (`events_session_<n>`: per-speaker event lists plus a `date`) are produced by the dataset authors. The first two were generated with gpt-3.5-turbo and the third was annotated. A memory system under evaluation must not read them. Its input is limited to `conversation`, meaning the turns, speaker names and session date_time values.
