#!/usr/bin/env bash
# The conflict study's test run, as pre-registered in
# eval/conflicts/PROTOCOL.md (commits 2c2dde3, 62dc279), on the 107 test
# questions of sets.json with the settings frozen in eval/conflicts/frozen.set
# (tag conflicts-freeze). Per test question, in sets order:
#   1. ingest the haystack into the base, conflict detection on (C0's database);
#   2. copy the base to C1; sleep C1 in maintenance mode with conflicts on
#      (proxy tag C1-sleep-<qid>);
#   3. leak check both (hard stop): no question text, no gold answer outside
#      the turns, no retrieval;
#   4. ask C0 on the base and C1 on the copy, once each; list the queues;
# then the official LongMemEval reader and judge on both (tags C0-reader,
# C0-judge, C1-reader, C1-judge), and report.py -> results/conflicts.{md,json}.
#
#   eval/conflicts/run_test.sh                               refuse checks and status (no LLM call)
#   eval/conflicts/run_test.sh --smoke 10 --paid --start-proxy   the smoke run (first 10), its projection for 107
#   eval/conflicts/run_test.sh --paid --start-proxy           the full run (refused unless the smoke projection is within the cap)
#   eval/conflicts/run_test.sh --paid                        through a proxy already at $PROXY_URL
#   eval/conflicts/run_test.sh --dry [--limit 3]             hash embedder, no LLM (phase 2 skipped), fake answers
#
# Cost: eval/conflicts/cost.json (cap CAP_USD, 2), call log
# eval/conflicts/llm_calls.jsonl, call store eval/lme/llmstore (replay).
# Environment: PROXY_URL, EMBEDDER (e5), EMBED_URL, LME_DATA, CAP_USD (2),
# OFFPEAK_ONLY (0), WORK (eval/conflicts/work/test). Every step is
# resumable: finished outputs are kept, outputs go through .part files.
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
. eval/conflicts/lib.sh

PROTOCOL_COMMITS="2c2dde3 62dc279"
PAID=0 DRY=0 START_PROXY=0 SMOKE=0 LIMIT=0 SHARD=""
while [ $# -gt 0 ]; do
  case "$1" in
    --paid) PAID=1 ;;
    --dry) DRY=1 ;;
    --start-proxy) START_PROXY=1 ;;
    --smoke) SMOKE=$2; shift ;;
    --limit) LIMIT=$2; shift ;;
    --shard) SHARD=$2; shift ;;  # i/N: only the per-history steps of every N-th history from i; no reader, judge or report
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    *) die "unknown flag $1" ;;
  esac
  shift
done
FROZEN=$CONF_DIR/frozen.set
TOTAL=107
REQUIRED="sleep.conflicts.min_cosine sleep.conflicts.context_turns sleep.conflicts.min_confidence"

if [ $DRY = 1 ]; then
  [ $PAID = 0 ] || die "--dry and --paid are exclusive"
  EMBEDDER=hash
  WORK=${WORK:-$CONF_DIR/work/test-dry}
  [ "$LIMIT" -gt 0 ] || LIMIT=3
  if [ -f "$FROZEN" ]; then read_conf_set "$FROZEN"; else
    CONF_SETS=(-set sleep.conflicts.min_cosine=0.8 -set sleep.conflicts.context_turns=1 -set sleep.conflicts.min_confidence=0.7)
    log "dry run without $FROZEN: placeholder settings ${CONF_SETS[*]}"
  fi
else
  WORK=${WORK:-$CONF_DIR/work/test}
  [ "$LIMIT" = 0 ] || die "--limit is for --dry only (the smoke run is --smoke N)"
  # The protocol and the freeze must precede this run.
  for c in $PROTOCOL_COMMITS; do
    git merge-base --is-ancestor "$c" HEAD || die "the protocol commit $c is not an ancestor of HEAD"
  done
  git rev-parse -q --verify refs/tags/conflicts-freeze >/dev/null || die "tag conflicts-freeze not found (the protocol runs the settings frozen there)"
  git merge-base --is-ancestor conflicts-freeze HEAD || die "tag conflicts-freeze is not an ancestor of HEAD"
  [ -f "$FROZEN" ] || die "$FROZEN not found: freeze the tuned values first"
  git cat-file -e "conflicts-freeze:$FROZEN" 2>/dev/null || die "$FROZEN is not in the conflicts-freeze commit"
  git diff --quiet conflicts-freeze -- "$FROZEN" "$CONF_DIR/PROTOCOL.md" "$CONF_DIR/kuturns.py" "$SETS" ||
    die "frozen.set, PROTOCOL.md, kuturns.py or sets.json differ from the conflicts-freeze commit"
  read_conf_set "$FROZEN"
  # The suite runs before the test, on the frozen settings (PROTOCOL.md, Stops).
  "$PY" -c 'import json,sys
r = json.load(open(sys.argv[1]))
sys.exit(0 if r.get("verdicts_ran") and not r.get("stop") and r.get("pairs") == 40 else 1)' "$CONF_DIR/suite/results.json" 2>/dev/null ||
    die "eval/conflicts/suite/results.json is missing, incomplete or a stop: run suite/run_suite.sh --paid on the frozen settings first"
fi
log "settings: ${CONF_SETS[*]}"

lme_data_abs
IDS=$(set_ids test)
[ "$(echo $IDS | wc -w | tr -d ' ')" = $TOTAL ] || die "sets.json test holds $(echo $IDS | wc -w) questions, the protocol says $TOTAL"
RUN_IDS=$IDS
if [ "$SMOKE" -gt 0 ]; then
  RUN_IDS=$(echo $IDS | tr ' ' '\n' | head -n "$SMOKE" | tr '\n' ' ')
elif [ "$LIMIT" -gt 0 ]; then
  RUN_IDS=$(echo $IDS | tr ' ' '\n' | head -n "$LIMIT" | tr '\n' ' ')
fi
# --shard i/N runs the per-history steps of histories i, i+N, ... in
# parallel with other shards (disjoint sets); a final run without --shard
# finds them done, then runs the reader, the judge and the report.
LOOP_IDS=$RUN_IDS
if [ -n "$SHARD" ]; then
  [ "$SMOKE" = 0 ] || die "--shard is for the full run"
  si=${SHARD%/*} sn=${SHARD#*/}
  LOOP_IDS=$(echo $RUN_IDS | tr ' ' '\n' | awk -v i="$si" -v n="$sn" '(NR-1)%n==i' | tr '\n' ' ')
fi

build_bins
read -r -a EMB <<<"$(embed_flags)"
SET=("${CONF_ON[@]}" "${CONF_SETS[@]}")
SLEEP_SET=("${SET[@]}" "${MAINT[@]}")
conf_check "C0 (ingest, ask)" "${CONF_SETS[@]}"
conf_check "C1 (maintenance sleep)" "${CONF_SETS[@]}" "${MAINT[@]}" -admin -llm-url "$PROXY_URL/tag/check/v1"
check_tool

if [ $DRY = 0 ]; then
  WINDOW=offpeak
  [ "${OFFPEAK_ONLY:-0}" = 1 ] || WINDOW=peak
  if [ "$SMOKE" = 0 ]; then
    # The full run needs the smoke run's projection within the cap.
    mkdir -p "$WORK"
    set +e
    "$PY" "$CONF_DIR/project_cost.py" test --work "$WORK" --smoke 10 --cost "$COST_FILE" --cap "$CAP_USD" --window "$WINDOW" \
      --calls "$CALL_LOG" --md "$WORK/smoke.md" --json "$WORK/smoke.json" >/dev/null 2>"$WORK/projection.err"
    st=$?
    set -e
    case $st in
      0) log "smoke projection within the US\$$CAP_USD cap (see $WORK/smoke.md)" ;;
      3) die "the smoke projection exceeds the US\$$CAP_USD cap (see $WORK/smoke.md): stop, the options go to the user (PROTOCOL.md)" ;;
      *) die "no smoke projection: run --smoke 10 first ($(tail -1 "$WORK/projection.err"))" ;;
    esac
  fi
  [ $PAID = 1 ] || { log "no LLM call made; pass --paid (with --smoke 10 first)"; exit 0; }
  check_embed
  if [ $START_PROXY = 1 ]; then start_proxy; else check_proxy; fi
fi
mkdir -p "$WORK"
write_sample test "$WORK/sample.json"

n=0
for q in $LOOP_IDS; do
  n=$((n + 1))
  d=$WORK/$q
  mkdir -p "$d"
  log "== [$n] $q"
  # 1. The base (C0's database).
  ingest_base "$q" "$d/base" "${CONF_SETS[@]}"
  # 2. C1: a fresh copy of the unasked base, then its sleep.
  if [ ! -f "$d/C1.sleep.json" ]; then
    [ ! -f "$d/C1.leak.json" ] || die "$d/C1.leak.json exists without C1.sleep.json: delete $d and rerun"
    [ "$(retrievals "$d/base")" = 0 ] || die "$d/base was asked before C1 was copied (an interrupted run): delete $d and rerun"
    rm -rf "$d/C1"
    "$LME_BIN" study copy -from "$d/base" -to "$d/C1" >&2
    if [ $DRY = 1 ]; then
      "$LME_BIN" study sleep -db "$d/C1" -condition C1 -phases 1,2 "${EMB[@]}" "${SLEEP_SET[@]}" -allow-phase2-skip \
        -out "$d/C1.sleep.json.part" >&2
    else
      "$LME_BIN" study sleep -db "$d/C1" -condition C1 -phases 1,2 "${EMB[@]}" "${SLEEP_SET[@]}" \
        -llm-url "$PROXY_URL/tag/C1-sleep-$q/v1" -allow-llm -out "$d/C1.sleep.json.part" >&2 || die "sleep C1 $q failed (rerun resumes)"
    fi
    mv "$d/C1.sleep.json.part" "$d/C1.sleep.json"
  fi
  # 3. Leak checks on both, before either is asked.
  for c in C0 C1; do
    db=$d/base
    [ $c = C1 ] && db=$d/C1
    if [ ! -f "$d/$c.leak.json" ]; then
      [ ! -f "$d/$c.ask.jsonl" ] || die "$d/$c.ask.jsonl exists without its leak check"
      [ "$(retrievals "$db")" = 0 ] || die "$db holds a retrieval before its leak check (an interrupted ask): delete $d and rerun"
    fi
    leak "$db" "$d/$c.leak.json"
  done
  # 4. Each condition asked once; an interrupted ask is not retried on the same database.
  for c in C0 C1; do
    db=$d/base
    [ $c = C1 ] && db=$d/C1
    if [ ! -f "$d/$c.ask.jsonl" ]; then
      [ "$(retrievals "$db")" = 0 ] || die "$db holds a retrieval without $c.ask.jsonl (an interrupted ask): delete $d and rerun"
    fi
    ask_once "$db" "$d/$c.ask.jsonl" "${SET[@]}"
  done
  conflicts_step "$d/C1" list "$d/C1.list.json" "${CONF_SETS[@]}"
  conflicts_step "$d/base" list "$d/C0.list.json" "${CONF_SETS[@]}"
done

[ -z "$SHARD" ] || { log "shard $SHARD done"; exit 0; }

# The official reader and judge on both conditions (resumable, per question).
for c in C0 C1; do
  qd=$WORK/llm-$c
  mkdir -p "$qd"
  : >"$qd/contexts.jsonl.part"
  for q in $RUN_IDS; do cat "$WORK/$q/$c.ask.jsonl" >>"$qd/contexts.jsonl.part"; done
  mv "$qd/contexts.jsonl.part" "$qd/contexts.jsonl"
  if [ $DRY = 1 ]; then
    "$PY" -c 'import json,sys
seen = set()
try:
    seen = {json.loads(l)["question_id"] for l in open(sys.argv[2])}
except FileNotFoundError:
    pass
with open(sys.argv[2], "a") as f:
    for l in open(sys.argv[1]):
        r = json.loads(l)
        if r["question_id"] not in seen:
            f.write(json.dumps({"question_id": r["question_id"], "question_type": r.get("question_type"), "answer": "Not mentioned", "verdict": False, "fake": True}) + "\n")' \
      "$qd/contexts.jsonl" "$qd/D.jsonl"
    continue
  fi
  "$PY" eval/lme/py/answer.py --condition D --contexts "$qd/contexts.jsonl" --dataset "$LME_DATA" --sample "$WORK/sample.json" \
    --questions $RUN_IDS --proxy "$PROXY_URL" --out-dir "$qd" --tag "$c-reader" || die "reader $c did not finish (rerun resumes)"
  "$PY" eval/lme/py/judge.py --condition D --raw-dir "$qd" --proxy "$PROXY_URL" --tag "$c-judge" || die "judge $c did not finish (rerun resumes)"
done

if [ "$SMOKE" -gt 0 ]; then
  set +e
  "$PY" "$CONF_DIR/project_cost.py" test --work "$WORK" --smoke "$SMOKE" --cost "$COST_FILE" --cap "$CAP_USD" --window "${WINDOW:-offpeak}" \
    --calls "$CALL_LOG" --md "$WORK/smoke.md" --json "$WORK/smoke.json"
  st=$?
  set -e
  [ $st = 3 ] && die "the projection for $TOTAL questions exceeds the US\$$CAP_USD cap (see $WORK/smoke.md): stop, the options go to the user (PROTOCOL.md)"
  [ $st = 0 ] || die "project_cost.py failed ($st)"
  log "smoke run done: $WORK/smoke.md; the full run is: eval/conflicts/run_test.sh --paid [--start-proxy]"
  exit 0
fi

RES=$CONF_DIR/results
[ $DRY = 1 ] && RES=$WORK
mkdir -p "$RES"
"$PY" "$CONF_DIR/report.py" --work "$WORK" --data "$LME_DATA" --ids "$RUN_IDS" --cost "$COST_FILE" --calls "$CALL_LOG" \
  --md "$RES/conflicts.md" --json "$RES/conflicts.json"
log "report: $RES/conflicts.md, $RES/conflicts.json"
