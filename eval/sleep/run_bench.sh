#!/usr/bin/env bash
# The diagnostic bench of the sleep study (eval/sleep/PROTOCOL.md: it repeats
# the same question after sleep, so it measures memorisation): 9 LongMemEval
# tuning questions, each with its own database.
#
#   per question: ingest (as condition D) -> ask (D2 defaults,
#   read.context_derived_max_share=0.3) -> official reader + judge (answer.py /
#   judge.py, condition D template) -> outcome -> copy to X and L ->
#   guided sleep (X: extractive digest, L: LLM digest) -> ask again -> reader + judge
#
#   eval/sleep/run_bench.sh                    checks + cost projection, then stop (no LLM call)
#   eval/sleep/run_bench.sh --paid --start-proxy
#   eval/sleep/run_bench.sh --paid             through a proxy already at $PROXY_URL
#   eval/sleep/run_bench.sh --dry              hash embedder, no LLM: X only, fake verdicts
#
# Environment: PROXY_URL, EMBEDDER (e5), EMBED_URL, CAP_USD (2), LME_DATA
# (eval/lme/data/longmemeval_s_cleaned.json), WORK, BENCH_SET (extra
# key=value overrides, space-separated). Resumable like run_locomo.sh.
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
. eval/sleep/lib.sh

IDS="a08a253f 55241a1f 51c32626 gpt4_e061b84f eac54add 6cb6f249 7527f7e2 6071bd76 gpt4_b0863698"
PAID=0 DRY=0 START_PROXY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --paid) PAID=1 ;;
    --dry) DRY=1 ;;
    --start-proxy) START_PROXY=1 ;;
    --ids) IDS=$2; shift ;;
    -h|--help) sed -n '2,19p' "$0"; exit 0 ;;
    *) die "unknown flag $1" ;;
  esac
  shift
done
LME_DATA=${LME_DATA:-eval/lme/data/longmemeval_s_cleaned.json}
TUNE=eval/split-tune.json
[ -f "$LME_DATA" ] || die "$LME_DATA not found (cd eval/lme && bin/lme fetch)"
if [ $DRY = 1 ]; then
  [ $PAID = 0 ] || die "--dry and --paid are exclusive"
  EMBEDDER=hash
  WORK=${WORK:-$SLEEP_DIR/work/bench-dry}
else
  WORK=${WORK:-$SLEEP_DIR/work/bench}
fi
LME_DATA=$(cd "$(dirname "$LME_DATA")" && pwd)/$(basename "$LME_DATA")

BASE=(-set read.context_derived_max_share=0.3)
for kv in ${BENCH_SET:-}; do BASE+=(-set "$kv"); done
X=("${BASE[@]}" -set sleep.mode=guided -set sleep.guided.digest=extractive)
LL=("${BASE[@]}" -set sleep.mode=guided -set sleep.guided.digest=llm)

build_bins
read -r -a EMB <<<"$(embed_flags)"
check_keys "the bench blocks" "${BASE[@]}"
check_keys "X (guided extractive)" "${X[@]}" -admin
check_keys "L (guided llm)" "${LL[@]}" -admin -llm-url "$PROXY_URL/tag/check/v1"

if [ $DRY = 0 ]; then
  "$PY" "$SLEEP_DIR/project_cost.py" bench --work "$WORK" --cost "$COST_FILE" --cap "$CAP_USD" \
    --questions "$(echo $IDS | wc -w | tr -d ' ')" || die "the projection exceeds the US\$$CAP_USD cap: ask before running"
  [ $PAID = 1 ] || { log "no LLM call made; pass --paid to run (off-peak only, enforced by the proxy)"; exit 0; }
  check_embed
  if [ $START_PROXY = 1 ]; then start_proxy; else check_proxy; fi
fi
mkdir -p "$WORK"

# read_judge ROUND: the official reader and judge over the 9 blocks of ROUND
# (condition D template, tags BENCH-<ROUND>-reader / -judge).
read_judge() {
  local r=$1 dir=$WORK/llm-$1
  : >"$WORK/$r.contexts.jsonl.part"
  for q in $IDS; do cat "$WORK/$q/$r.ask.jsonl" >>"$WORK/$r.contexts.jsonl.part"; done
  mv "$WORK/$r.contexts.jsonl.part" "$WORK/$r.contexts.jsonl"
  if [ $DRY = 1 ]; then
    mkdir -p "$dir"
    "$PY" -c 'import json,sys
with open(sys.argv[2], "w") as f:
    for l in open(sys.argv[1]):
        r = json.loads(l)
        f.write(json.dumps({"question_id": r["question_id"], "answer": "Not mentioned", "verdict": False, "fake": True}) + "\n")' \
      "$WORK/$r.contexts.jsonl" "$dir/D.jsonl"
    return
  fi
  "$PY" eval/lme/py/answer.py --condition D --contexts "$WORK/$r.contexts.jsonl" --dataset "$LME_DATA" --sample "$TUNE" \
    --questions $IDS --proxy "$PROXY_URL" --out-dir "$dir" --tag "BENCH-$r-reader" || die "reader $r did not finish (rerun resumes)"
  "$PY" eval/lme/py/judge.py --condition D --raw-dir "$dir" --proxy "$PROXY_URL" --tag "BENCH-$r-judge" ||
    die "judge $r did not finish (rerun resumes)"
}

ask() { # ask QID ROUND DB SETS...
  local q=$1 r=$2 db=$3; shift 3
  if [ ! -f "$WORK/$q/$r.ask.jsonl" ]; then
    "$LME_BIN" study ask -db "$db" -condition D "${EMB[@]}" "$@" -out "$WORK/$q/$r.ask.jsonl.part"
    mv "$WORK/$q/$r.ask.jsonl.part" "$WORK/$q/$r.ask.jsonl"
  fi
}

for q in $IDS; do
  mkdir -p "$WORK/$q"
  [ -f "$WORK/$q/base/chanda.db" ] || "$LME_BIN" study ingest -db "$WORK/$q/base" -data "$LME_DATA" -question "$q" "${EMB[@]}" "${BASE[@]}"
  ask "$q" before "$WORK/$q/base" "${BASE[@]}"
done
read_judge before

for q in $IDS; do
  if [ ! -f "$WORK/$q/outcomes.jsonl" ]; then
    "$LME_BIN" study outcome -db "$WORK/$q/base" -ask "$WORK/$q/before.ask.jsonl" -answers "$WORK/llm-before/D.jsonl" \
      "${EMB[@]}" "${BASE[@]}" -out "$WORK/$q/outcomes.jsonl"
  fi
  for m in X L; do
    [ $DRY = 1 ] && [ $m = L ] && continue
    [ -d "$WORK/$q/$m" ] || "$LME_BIN" study copy -from "$WORK/$q/base" -to "$WORK/$q/$m"
    if [ ! -f "$WORK/$q/$m.sleep.json" ]; then
      if [ $m = X ]; then
        "$LME_BIN" study sleep -db "$WORK/$q/X" -condition X -phases 1,2 "${EMB[@]}" "${X[@]}" -out "$WORK/$q/X.sleep.json.part"
      else
        "$LME_BIN" study sleep -db "$WORK/$q/L" -condition L -phases 1,2 "${EMB[@]}" "${LL[@]}" \
          -llm-url "$PROXY_URL/tag/BENCH-L-sleep/v1" -allow-llm -out "$WORK/$q/L.sleep.json.part"
      fi
      mv "$WORK/$q/$m.sleep.json.part" "$WORK/$q/$m.sleep.json"
    fi
  done
  ask "$q" X "$WORK/$q/X" "${X[@]}"
  [ $DRY = 1 ] || ask "$q" L "$WORK/$q/L" "${LL[@]}"
done
read_judge X
[ $DRY = 1 ] || read_judge L

"$PY" "$SLEEP_DIR/bench_report.py" --work "$WORK" --ids "$(echo $IDS | tr ' ' ',')" --cost "$COST_FILE" \
  --md "$WORK/bench.md" --json "$WORK/bench.json"
log "bench data: $WORK/bench.md, $WORK/bench.json (blocks: $WORK/<qid>/{before,X,L}.ask.jsonl; fichas in <qid>/{X,L}.sleep.json)"
