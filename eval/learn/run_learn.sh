#!/usr/bin/env bash
# The learning study's test run, exactly as pre-registered in
# eval/learn/PROTOCOL.md (commit 0fa457e): the 7 LoCoMo test conversations,
# conditions B, U0, U1, K; the use half once on the base database (U0 with
# write.learned_keys on), its outcomes, one copy per condition with its
# learning step (U1, K: sleep phase 1 only), the leak check (hard stop),
# then the test half once per condition. See eval/learn/README.md.
#
#   eval/learn/run_learn.sh                       refuse checks + cost projection, then stop (no LLM call)
#   eval/learn/run_learn.sh --paid --start-proxy  the study (starts lmeproxy itself)
#   eval/learn/run_learn.sh --paid                the study through a proxy already at $PROXY_URL
#   eval/learn/run_learn.sh --dry [--limit 3] [--convs "conv-30"] [--skip-keys]
#                                                 hash embedder, fake answers, no LLM, no proxy
#
# Environment: PROXY_URL (http://127.0.0.1:8787), EMBEDDER (e5), EMBED_URL
# (http://127.0.0.1:8089, a running 'lme embedserve -embedder e5'), CAP_USD
# (4), OFFPEAK_ONLY (1; 0 lets the proxy call in peak hours, a deviation),
# LOCOMO (eval/locomo/data/locomo10.json), WORK; SPLIT for dry runs only.
# Every step is resumable: a finished output is kept and the step skipped;
# outputs are written to .part files and renamed after a count check.
# (No set -u: bash 3.2, macOS's, treats an empty array as unbound.)
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
STUDY_DIR=eval/learn
CAP_USD=${CAP_USD:-4}
. eval/sleep/lib.sh
LEARN_DIR=eval/learn

PAID=0 DRY=0 START_PROXY=0 LIMIT=0 SKIP_KEYS=0
TEST_CONVS="conv-26 conv-41 conv-42 conv-43 conv-44 conv-48 conv-50"
CONVS=$TEST_CONVS
while [ $# -gt 0 ]; do
  case "$1" in
    --paid) PAID=1 ;;
    --dry) DRY=1 ;;
    --start-proxy) START_PROXY=1 ;;
    --limit) LIMIT=$2; shift ;;
    --convs) CONVS=$2; shift ;;
    --skip-keys) SKIP_KEYS=1 ;;
    -h|--help) sed -n '2,21p' "$0"; exit 0 ;;
    *) die "unknown flag $1" ;;
  esac
  shift
done

# The protocol must precede this code.
git merge-base --is-ancestor 0fa457e HEAD || die "the protocol commit 0fa457e is not an ancestor of HEAD"

LOCOMO=${LOCOMO:-eval/locomo/data/locomo10.json}
FROZEN=$LEARN_DIR/frozen.set
[ -f "$LOCOMO" ] || die "$LOCOMO not found (fetch it per eval/locomo/SOURCE.md)"

if [ $DRY = 1 ]; then
  [ $PAID = 0 ] || die "--dry and --paid are exclusive"
  EMBEDDER=hash
  WORK=${WORK:-$LEARN_DIR/work/dry}
  SPLIT=${SPLIT:-$LEARN_DIR/split-locomo.json}
  [ "$LIMIT" -gt 0 ] || LIMIT=3
else
  WORK=${WORK:-$LEARN_DIR/work/test}
  [ -z "$SPLIT" ] || die "SPLIT is for --dry only: the protocol's split is $LEARN_DIR/split-locomo.json"
  SPLIT=$LEARN_DIR/split-locomo.json
  [ "$LIMIT" = 0 ] || die "--limit is for --dry only"
  [ "$CONVS" = "$TEST_CONVS" ] || die "--convs is for --dry only: the protocol runs the 7 test conversations"
  [ $SKIP_KEYS = 0 ] || die "--skip-keys is for --dry only: K is a pre-registered condition"
  [ -f "$FROZEN" ] || die "$FROZEN not found: freeze the tuned values first (decision 0147; see eval/learn/README.md)"
  if git rev-parse -q --verify refs/tags/learn-freeze >/dev/null; then
    git merge-base --is-ancestor learn-freeze HEAD || die "tag learn-freeze is not an ancestor of HEAD"
  else
    die "tag learn-freeze not found (the protocol runs the configuration frozen there)"
  fi
fi
LIMIT_FLAG=()
[ "$LIMIT" -gt 0 ] && LIMIT_FLAG=(-limit "$LIMIT")

# The frozen overrides: ranking.utility_lambda and ranking.min_exposures
# (U1 and K), read.learned_keys_weight and read.learned_keys_top_k (K only).
# Every one must be given; any other key is refused.
U_SETS=() K_SETS=()
read_frozen() {
  local f=$1 line k v seen=""
  while IFS= read -r line || [ -n "$line" ]; do
    line=${line%%#*}
    line=$(printf '%s' "$line" | tr -d '[:space:]')
    [ -n "$line" ] || continue
    case "$line" in *=*) ;; *) die "$f: '$line' is not key=value" ;; esac
    k=${line%%=*} v=${line#*=}
    [ -n "$v" ] || die "$f: $k has no value"
    case "$k" in
      ranking.utility_lambda|ranking.min_exposures) U_SETS+=(-set "$k=$v") ;;
      read.learned_keys_weight|read.learned_keys_top_k) K_SETS+=(-set "$k=$v") ;;
      *) die "$f: $k is not a frozen key of this study (ranking.utility_lambda, ranking.min_exposures, read.learned_keys_weight, read.learned_keys_top_k)" ;;
    esac
    case " $seen " in *" $k "*) die "$f: $k given twice" ;; esac
    seen="$seen $k"
  done <"$f"
  for k in ranking.utility_lambda ranking.min_exposures read.learned_keys_weight read.learned_keys_top_k; do
    case " $seen " in *" $k "*) ;; *) die "$f: $k is missing (every frozen key is given explicitly)" ;; esac
  done
}
if [ -f "$FROZEN" ]; then
  read_frozen "$FROZEN"
  log "frozen ($FROZEN): U1 ${U_SETS[*]}; K adds ${K_SETS[*]}"
else
  # Dry runs only (checked above): placeholder values.
  U_SETS=(-set ranking.utility_lambda=0.2 -set ranking.min_exposures=2)
  K_SETS=(-set read.learned_keys_weight=1 -set read.learned_keys_top_k=20)
  log "dry run without $FROZEN: placeholder values ${U_SETS[*]} ${K_SETS[*]}"
fi

CONDS="U0 U1 K"
if [ $SKIP_KEYS = 1 ]; then
  CONDS="U0 U1"
  log "dry run --skip-keys: no write.learned_keys, no K, no keys steps"
fi

build_bins
read -r -a EMB <<<"$(embed_flags)"

# cond_sets C: the overrides of condition C (base: U0 with keys written).
cond_sets() {
  case "$1" in
    base)
      COND=(-set ranking.utility_lambda=0)
      [ $SKIP_KEYS = 1 ] || COND+=(-set write.learned_keys=true -set read.learned_keys=false) ;;
    U0)
      COND=(-set ranking.utility_lambda=0)
      [ $SKIP_KEYS = 1 ] || COND+=(-set read.learned_keys=false) ;;
    U1)
      COND=("${U_SETS[@]}")
      [ $SKIP_KEYS = 1 ] || COND+=(-set read.learned_keys=false) ;;
    K) COND=("${U_SETS[@]}" -set read.learned_keys=true "${K_SETS[@]}") ;;
    *) die "unknown condition $1" ;;
  esac
}
for c in base $CONDS; do
  cond_sets "$c"
  check_keys "$c" "${COND[@]}"
done
cond_sets K
[ $SKIP_KEYS = 1 ] || check_keys "keys (admin)" "${COND[@]}" -admin

if [ $DRY = 0 ]; then
  WINDOW=offpeak
  [ "${OFFPEAK_ONLY:-1}" = 1 ] || { WINDOW=peak; log "OFFPEAK_ONLY=0: calls in peak hours are allowed; each is a deviation (PROTOCOL.md) and the report lists them"; }
  "$PY" "$LEARN_DIR/project_cost.py" --work "$WORK" --cost "$COST_FILE" --cap "$CAP_USD" --window "$WINDOW" ||
    die "the projection exceeds the US\$$CAP_USD cap: ask before running"
  [ $PAID = 1 ] || { log "no LLM call made; pass --paid to run (off-peak only unless OFFPEAK_ONLY=0, enforced by the proxy)"; exit 0; }
  check_embed
  if [ $START_PROXY = 1 ]; then start_proxy; else check_proxy; fi
fi

# same_ids A B: the two JSONL files hold the same question ids, once each.
same_ids() {
  "$PY" - "$1" "$2" <<'PY'
import json, sys
def ids(p):
    out = [json.loads(l)["question_id"] for l in open(p) if l.strip()]
    if len(out) != len(set(out)):
        sys.exit(f"{p}: a question id appears twice")
    return set(out)
a, b = ids(sys.argv[1]), ids(sys.argv[2])
sys.exit(0 if a == b else f"{sys.argv[2]}: {len(b)} question ids, {sys.argv[1]} has {len(a)} ({len(a - b)} missing)")
PY
}

# qa ASK ANSWERS TAG: reader + judge (or fake answers in a dry run) into
# ANSWERS.part, renamed when every question of ASK is answered.
qa() {
  local ask=$1 ans=$2 tag=$3 fake=()
  [ -f "$ans" ] && return 0
  [ $DRY = 1 ] && fake=(--fake)
  "$PY" "$SLEEP_DIR/locomo_qa.py" --ask "$ask" --out "$ans.part" --tag "$tag" --proxy "$PROXY_URL" "${fake[@]}" ||
    die "reader/judge for $ask did not finish (rerun resumes)"
  same_ids "$ask" "$ans.part" || die "answers for $ask incomplete (rerun resumes)"
  mv "$ans.part" "$ans"
}

# ask_half CONV C DB HALF LABEL: context per question of HALF on DB.
ask_half() {
  local conv=$1 c=$2 db=$3 half=$4 label=$5
  local f=$WORK/$conv/out/$label.ask.jsonl
  [ -f "$f" ] && return 0
  "$LME_BIN" study ask -db "$db" -split "$SPLIT" -half "$half" -condition "$c" "${EMB[@]}" "${COND[@]}" "${LIMIT_FLAG[@]}" \
    -out "$f.part"
  mv "$f.part" "$f"
}

# retrievals DB: the number of retrievals logged in DB/chanda.db.
retrievals() {
  "$PY" -c 'import os,sqlite3,sys; db=sys.argv[1]; m="ro" if os.path.exists(db+"-wal") else "ro&immutable=1"
print(sqlite3.connect("file:"+db+"?mode="+m, uri=True).execute("SELECT count(*) FROM retrievals").fetchone()[0])' "$1/chanda.db"
}

# llm_free SLEEP_JSON: the learning step made no LLM call.
llm_free() {
  "$PY" -c 'import json,sys; c=(json.load(open(sys.argv[1])).get("output") or {}).get("cost") or {}; sys.exit(0 if int(c.get("llm_calls", 0)) == 0 else 1)' "$1"
}

for conv in $CONVS; do
  d=$WORK/$conv out=$WORK/$conv/out
  mkdir -p "$out"
  log "== $conv"
  # 1. Ingest into the base database.
  cond_sets base
  if [ ! -f "$d/base/study.json" ]; then
    rm -rf "$d/base"
    "$LME_BIN" study ingest -db "$d/base" -locomo "$LOCOMO" -conv "$conv" "${EMB[@]}" "${COND[@]}"
  fi
  # 2. The use half with U0 and keys written; read, judge, one outcome per retrieval.
  if [ ! -f "$out/base.use.ask.jsonl" ] && [ "$(retrievals "$d/base")" != 0 ]; then
    die "$d/base holds retrievals of an interrupted ask: delete $d and rerun"
  fi
  ask_half "$conv" base "$d/base" use base.use
  qa "$out/base.use.ask.jsonl" "$out/base.use.answers.jsonl" base-use
  if [ ! -f "$out/base.use.outcomes.jsonl" ]; then
    "$LME_BIN" study outcome -db "$d/base" -ask "$out/base.use.ask.jsonl" -answers "$out/base.use.answers.jsonl" \
      "${EMB[@]}" "${COND[@]}" -out "$out/base.use.outcomes.jsonl.part"
    [ "$(wc -l < "$out/base.use.outcomes.jsonl.part")" -eq "$(wc -l < "$out/base.use.ask.jsonl")" ] ||
      die "outcomes for $conv incomplete (rerun resumes)"
    mv "$out/base.use.outcomes.jsonl.part" "$out/base.use.outcomes.jsonl"
  fi
  # Keys created by the use half (read-only: the leak check counts them).
  if [ ! -f "$out/base.leak.json" ]; then
    "$LME_BIN" study leakcheck -protocol learn -db "$d/base" -split "$SPLIT" -out "$out/base.leak.json.part" >&2 ||
      die "leak check failed on $d/base (see $out/base.leak.json.part): hard stop"
    mv "$out/base.leak.json.part" "$out/base.leak.json"
  fi
  # 3-4. Per condition: copy, learning step, leak check, the test half once.
  for c in $CONDS; do
    cond_sets "$c"
    if [ ! -f "$d/$c/study.json" ] || { [ "$c" != U0 ] && [ ! -f "$out/$c.sleep.json" ]; }; then
      # A fresh copy for every learning step that has not finished.
      [ ! -f "$out/$c.leak.json" ] || die "$out/$c.leak.json exists but $d/$c is not finished: delete both and rerun"
      rm -rf "$d/$c"
      "$LME_BIN" study copy -from "$d/base" -to "$d/$c"
    fi
    if [ "$c" != U0 ] && [ ! -f "$out/$c.sleep.json" ]; then
      # No -llm-url: no LLM is configured, phase 1 only.
      "$LME_BIN" study sleep -db "$d/$c" -condition "$c" -phases 1 "${EMB[@]}" "${COND[@]}" -out "$out/$c.sleep.json.part"
      llm_free "$out/$c.sleep.json.part" || die "$c's learning step on $conv reports LLM calls"
      mv "$out/$c.sleep.json.part" "$out/$c.sleep.json"
    fi
    if [ ! -f "$out/$c.leak.json" ]; then
      [ ! -f "$out/$c.test.ask.jsonl" ] || die "$out/$c.test.ask.jsonl exists without its leak check"
      "$LME_BIN" study leakcheck -protocol learn -db "$d/$c" -split "$SPLIT" -out "$out/$c.leak.json.part" >&2 ||
        die "leak check failed on $d/$c before the test half (see $out/$c.leak.json.part): hard stop (PROTOCOL.md, Leak check)"
      mv "$out/$c.leak.json.part" "$out/$c.leak.json"
    fi
    if [ ! -f "$out/$c.test.ask.jsonl" ]; then
      # The test half is asked once, on the database the leak check saw.
      "$PY" -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["retrievals"] == int(sys.argv[2]) else 1)' \
        "$out/$c.leak.json" "$(retrievals "$d/$c")" ||
        die "$d/$c has retrievals the leak check did not see (an interrupted test ask): delete $d/$c and $out/$c.* and rerun"
    fi
    ask_half "$conv" "$c" "$d/$c" test "$c.test"
    qa "$out/$c.test.ask.jsonl" "$out/$c.test.answers.jsonl" "$c-test"
  done
  # Plain RAG (B) on the test half.
  if [ ! -f "$out/B.test.ask.jsonl" ]; then
    "$LME_BIN" study rag -locomo "$LOCOMO" -conv "$conv" -split "$SPLIT" -half test "${EMB[@]}" "${LIMIT_FLAG[@]}" \
      -out "$out/B.test.ask.jsonl.part"
    mv "$out/B.test.ask.jsonl.part" "$out/B.test.ask.jsonl"
  fi
  qa "$out/B.test.ask.jsonl" "$out/B.test.answers.jsonl" B-test
  # Database size without keys: a copy of K after its test half, keys reset
  # (no LLM; measured by report.py).
  if [ $SKIP_KEYS = 0 ] && [ ! -f "$out/K.nokeys.keys.json" ]; then
    cond_sets K
    rm -rf "$d/K.nokeys"
    "$LME_BIN" study copy -from "$d/K" -to "$d/K.nokeys"
    "$LME_BIN" study keys -db "$d/K.nokeys" -action reset "${EMB[@]}" "${COND[@]}" -out "$out/K.nokeys.keys.json.part"
    mv "$out/K.nokeys.keys.json.part" "$out/K.nokeys.keys.json"
  fi
done

RES=$LEARN_DIR/results
[ $DRY = 1 ] && RES=$WORK
mkdir -p "$RES"
"$PY" "$LEARN_DIR/report.py" --work "$WORK" --cost "$COST_FILE" --calls "$CALL_LOG" --convs "$CONVS" \
  --md "$RES/learn.md" --json "$RES/learn.json"
REVIEW=$LEARN_DIR/review.md
[ $DRY = 1 ] && REVIEW=$WORK/review.md
"$PY" "$LEARN_DIR/review.py" --work "$WORK" --locomo "$LOCOMO" --convs "$CONVS" --out "$REVIEW"
log "report: $RES/learn.md, $RES/learn.json; hand review: $REVIEW"
