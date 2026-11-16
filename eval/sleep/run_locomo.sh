#!/usr/bin/env bash
# The LoCoMo sleep transfer test, exactly as pre-registered in
# eval/sleep/PROTOCOL.md (commit dfb68b0): conversations conv-30, conv-47,
# conv-49; conditions B, S0, S1, S2, then S3 last; test half per condition;
# use half again per sleeping condition as a ceiling. See eval/sleep/README.md.
#
#   eval/sleep/run_locomo.sh                 refuse checks + cost projection, then stop (no LLM call)
#   eval/sleep/run_locomo.sh --paid --start-proxy     the study (starts lmeproxy itself)
#   eval/sleep/run_locomo.sh --paid          the study through a proxy already at $PROXY_URL
#   eval/sleep/run_locomo.sh --dry [--limit 3]        hash embedder, fake answers, no LLM, no proxy
#
# Environment: PROXY_URL (http://127.0.0.1:8787), EMBEDDER (e5), EMBED_URL
# (http://127.0.0.1:8089, a running 'lme embedserve -embedder e5'), CAP_USD
# (2), LOCOMO (eval/locomo/data/locomo10.json), WORK.
# Every step is resumable: an existing output is kept and the step skipped.
# (No set -u: bash 3.2, macOS's, treats an empty array as unbound.)
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
. eval/sleep/lib.sh

PAID=0 DRY=0 START_PROXY=0 LIMIT=0 SKIP_S3=0
CONVS="conv-30 conv-47 conv-49"
while [ $# -gt 0 ]; do
  case "$1" in
    --paid) PAID=1 ;;
    --dry) DRY=1 ;;
    --start-proxy) START_PROXY=1 ;;
    --limit) LIMIT=$2; shift ;;
    --convs) CONVS=$2; shift ;;
    --skip-s3) SKIP_S3=1 ;;
    -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
    *) die "unknown flag $1" ;;
  esac
  shift
done

# The protocol must precede this code.
git merge-base --is-ancestor dfb68b0 HEAD || die "the protocol commit dfb68b0 is not an ancestor of HEAD"

LOCOMO=${LOCOMO:-eval/locomo/data/locomo10.json}
SPLIT=$SLEEP_DIR/split-locomo.json
FROZEN=$SLEEP_DIR/frozen.set
[ -f "$LOCOMO" ] || die "$LOCOMO not found (fetch it per eval/locomo/SOURCE.md)"

if [ $DRY = 1 ]; then
  [ $PAID = 0 ] || die "--dry and --paid are exclusive"
  EMBEDDER=hash
  WORK=${WORK:-$SLEEP_DIR/work/locomo-dry}
  [ "$LIMIT" -gt 0 ] || LIMIT=3
  SET_FILE=()
  [ -f "$FROZEN" ] && SET_FILE=(-set-file "$FROZEN")
else
  WORK=${WORK:-$SLEEP_DIR/work/locomo}
  [ "$LIMIT" = 0 ] || die "--limit is for --dry only"
  [ "$CONVS" = "conv-30 conv-47 conv-49" ] || die "--convs is for --dry only: the protocol runs the three conversations"
  # The frozen configuration of the bench (decision 0144, tag sleep-freeze):
  # one key=value per line, applied to every Chanda step.
  [ -f "$FROZEN" ] || die "$FROZEN not found: freeze the bench's configuration first (decision 0144; one key=value per line, may be empty)"
  if git rev-parse -q --verify refs/tags/sleep-freeze >/dev/null; then
    git merge-base --is-ancestor sleep-freeze HEAD || die "tag sleep-freeze is not an ancestor of HEAD"
  else
    die "tag sleep-freeze not found (the protocol runs the configuration frozen there)"
  fi
  SET_FILE=(-set-file "$FROZEN")
fi
LIMIT_FLAG=()
[ "$LIMIT" -gt 0 ] && LIMIT_FLAG=(-limit "$LIMIT")

build_bins
read -r -a EMB <<<"$(embed_flags)"
GUIDED_X=(-set sleep.mode=guided -set sleep.guided.digest=extractive)
GUIDED_L=(-set sleep.mode=guided -set sleep.guided.digest=llm)
UNIFORM=(-set sleep.mode=uniform)
check_keys base "${SET_FILE[@]}"
check_keys S1 "${SET_FILE[@]}" "${GUIDED_X[@]}" -admin
check_keys S2 "${SET_FILE[@]}" "${GUIDED_L[@]}" -admin -llm-url "$PROXY_URL/tag/check/v1"
check_keys S3 "${SET_FILE[@]}" "${UNIFORM[@]}" -admin -llm-url "$PROXY_URL/tag/check/v1"

if [ $DRY = 0 ]; then
  "$PY" "$SLEEP_DIR/project_cost.py" locomo --work "$WORK" --cost "$COST_FILE" --cap "$CAP_USD" ||
    die "the projection with the bench's spend exceeds the US\$$CAP_USD cap: ask before running (PROTOCOL.md, Stops)"
  [ $PAID = 1 ] || { log "no LLM call made; pass --paid to run (off-peak only, enforced by the proxy)"; exit 0; }
  check_embed
  if [ $START_PROXY = 1 ]; then start_proxy; else check_proxy; fi
fi

# qa ASK ANSWERS TAG: reader + judge (or fake answers in a dry run).
qa() {
  local fake=()
  [ $DRY = 1 ] && fake=(--fake)
  "$PY" "$SLEEP_DIR/locomo_qa.py" --ask "$1" --out "$2" --tag "$3" --proxy "$PROXY_URL" "${fake[@]}" ||
    die "reader/judge for $1 did not finish (rerun resumes)"
}

# cond_sets C: the overrides of condition C (frozen file, then the sleep keys).
cond_sets() {
  case "$1" in
    S0|base) COND=("${SET_FILE[@]}") ;;
    S1) COND=("${SET_FILE[@]}" "${GUIDED_X[@]}") ;;
    S2) COND=("${SET_FILE[@]}" "${GUIDED_L[@]}") ;;
    S3) COND=("${SET_FILE[@]}" "${UNIFORM[@]}") ;;
  esac
}

# sleep_cond CONV C [budget flags]: copy base, sleep, leak check (before the test half).
sleep_cond() {
  local conv=$1 c=$2; shift 2
  local d=$WORK/$conv out=$WORK/$conv/out
  cond_sets "$c"
  [ -d "$d/$c" ] || "$LME_BIN" study copy -from "$d/base" -to "$d/$c"
  if [ ! -f "$out/$c.sleep.json" ]; then
    local phases=1,2 llm=()
    [ "$c" = S0 ] && phases=1
    case "$c" in S2|S3) llm=(-llm-url "$PROXY_URL/tag/$c-sleep-$conv/v1" -allow-llm) ;; esac
    [ $DRY = 1 ] && [ ${#llm[@]} -gt 0 ] && die "dry runs never configure an LLM"
    "$LME_BIN" study sleep -db "$d/$c" -condition "$c" -phases "$phases" "${EMB[@]}" "${COND[@]}" "${llm[@]}" "$@" \
      -out "$out/$c.sleep.json.part"
    mv "$out/$c.sleep.json.part" "$out/$c.sleep.json"
  fi
  if ! "$LME_BIN" study leakcheck -db "$d/$c" -split "$SPLIT" -before-test -out "$out/$c.leak.json" >&2; then
    [ -f "$out/$c.test.ask.jsonl" ] && return 0
    # Hard stops: tampered outcomes, test questions in the retrievals, judge
    # words in the database. A gold string in a derived text that its origin
    # turns do not hold literally (an LLM rewrite in the third person, as on
    # conv-30 S2) is reviewed by hand after the run (eval/sleep/leak-review.md),
    # not a stop: deviation 2 of RESULTS.md.
    $PY -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if not (d["bad_outcomes"] or d["test_questions_in_retrievals"] or d["judge_words"]) else 1)' \
      "$out/$c.leak.json" || die "leak check failed on $d/$c before the test half (PROTOCOL.md, Stops)"
    log "leak check on $d/$c: only unexplained gold hits ($($PY -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["unexplained_gold_hits"]))' "$out/$c.leak.json")); continuing, reviewed by hand after the run"
  fi
}

# ask_cond CONV C HALF LABEL: ask on condition C's database, then read + judge.
ask_cond() {
  local conv=$1 c=$2 half=$3 label=$4
  local d=$WORK/$conv out=$WORK/$conv/out
  cond_sets "$c"
  local db=$d/$c
  [ "$c" = base ] && db=$d/base
  if [ ! -f "$out/$c.$label.ask.jsonl" ]; then
    "$LME_BIN" study ask -db "$db" -split "$SPLIT" -half "$half" -condition "$c" "${EMB[@]}" "${COND[@]}" "${LIMIT_FLAG[@]}" \
      -out "$out/$c.$label.ask.jsonl.part"
    mv "$out/$c.$label.ask.jsonl.part" "$out/$c.$label.ask.jsonl"
  fi
  qa "$out/$c.$label.ask.jsonl" "$out/$c.$label.answers.jsonl" "$c-$label"
}

for conv in $CONVS; do
  d=$WORK/$conv out=$WORK/$conv/out
  mkdir -p "$out"
  log "== $conv"
  if [ ! -f "$d/base/chanda.db" ]; then
    cond_sets base
    "$LME_BIN" study ingest -db "$d/base" -locomo "$LOCOMO" -conv "$conv" "${EMB[@]}" "${COND[@]}"
  fi
  ask_cond "$conv" base use use
  if [ ! -f "$out/base.use.outcomes.jsonl" ]; then
    cond_sets base
    "$LME_BIN" study outcome -db "$d/base" -ask "$out/base.use.ask.jsonl" -answers "$out/base.use.answers.jsonl" \
      "${EMB[@]}" "${COND[@]}" -out "$out/base.use.outcomes.jsonl"
  fi
  for c in S0 S1 S2; do
    [ $DRY = 1 ] && [ $c = S2 ] && { log "dry run: S2 (LLM digest) skipped"; continue; }
    sleep_cond "$conv" "$c"
    ask_cond "$conv" "$c" test test
  done
  if [ ! -f "$out/B.test.ask.jsonl" ]; then
    "$LME_BIN" study rag -locomo "$LOCOMO" -conv "$conv" -split "$SPLIT" -half test "${EMB[@]}" "${LIMIT_FLAG[@]}" \
      -out "$out/B.test.ask.jsonl.part"
    mv "$out/B.test.ask.jsonl.part" "$out/B.test.ask.jsonl"
  fi
  qa "$out/B.test.ask.jsonl" "$out/B.test.answers.jsonl" B-test
  for c in S0 S1 S2; do
    [ $DRY = 1 ] && [ $c = S2 ] && continue
    ask_cond "$conv" "$c" use ceiling
  done
done

# S3 last: uniform sleep with S2's tokens as its budget, only with room under the cap.
if [ $DRY = 0 ] && [ $SKIP_S3 = 0 ]; then
  room=$("$PY" - "$COST_FILE" "$CAP_USD" "$WORK" $CONVS <<'PY'
import json, os, sys
sys.path.insert(0, "eval/sleep")
import project_cost as pc
cost, cap, work, convs = sys.argv[1], float(sys.argv[2]), sys.argv[3], sys.argv[4:]
spent = json.load(open(cost)).get("spent_usd", 0) if os.path.exists(cost) else 0
r = pc.rates("deepseek-flash")
need = 0.0
for conv in convs:
    t = pc.sleep_tokens(os.path.join(work, conv, "out", "S2.sleep.json")) or 0
    need += pc.usd(r, t * (1 - pc.SLEEP_OUT_SHARE), t * pc.SLEEP_OUT_SHARE)
    # test + ceiling reader/judge, upper bound
    need += 160 * (2300 * r["input_cache_miss"] + 205 * r["output"]) / 1e6
print("yes" if spent + need <= cap else f"no: spent {spent:.4f} + S3 upper {need:.4f} > cap {cap}")
PY
)
  if [ "$room" = yes ]; then
    for conv in $CONVS; do
      out=$WORK/$conv/out
      tok=$("$PY" -c 'import json,sys; c=json.load(open(sys.argv[1]))["output"]["cost"]; print(c["input_tokens"]+c["output_tokens"])' "$out/S2.sleep.json")
      if [ "$tok" -le 0 ]; then log "S3 $conv skipped: S2 spent no tokens (a zero budget would mean Chanda's default)"; continue; fi
      sleep_cond "$conv" S3 -budget-tokens "$tok" -budget-events 1000000
      ask_cond "$conv" S3 test test
      ask_cond "$conv" S3 use ceiling
    done
  else
    log "S3 skipped: $room"
  fi
fi

RES=$SLEEP_DIR/results
[ $DRY = 1 ] && RES=$WORK
mkdir -p "$RES"
"$PY" "$SLEEP_DIR/report.py" --work "$WORK" --cost "$COST_FILE" --md "$RES/locomo.md" --json "$RES/locomo.json"
log "report: $RES/locomo.md, $RES/locomo.json"
