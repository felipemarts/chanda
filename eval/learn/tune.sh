#!/usr/bin/env bash
# Development tuning of the learning study (no LLM): the grid of
# eval/learn/README.md on the dev conversations conv-30, conv-47 and
# conv-49, using the sleep study's base databases (use-half retrievals and
# outcomes, no keys). Per variant and conversation: copy base, keys rebuild
# (K variants), sleep phase 1 (all but U0), leak check, then the dev test
# half (eval/sleep/split-locomo.json) asked once. Variants run one after
# the other; finished ones are kept (resumable). Output:
# eval/learn/tune/grid.{md,json}.
#
#   eval/learn/tune.sh            the whole grid
#   KEEP_DBS=1 eval/learn/tune.sh keep each variant's database
#
# Grid: U0 (no phase 1); U1 with ranking.utility_lambda in {0.1, 0.2, 0.3}
# x ranking.min_exposures in {1, 2, 5}; K = the best U1 plus
# read.learned_keys_weight in {0.5, 1, 2} x read.learned_keys_top_k in
# {10, 20}; K-alone = keys with ranking.utility_lambda 0, default weight
# and top k.
#
# Environment: EMBED_URL (http://127.0.0.1:8089, 'lme embedserve -embedder
# e5', built with GOEXPERIMENT=simd as lib.sh does), SLEEP_WORK
# (eval/sleep/work/locomo), TUNE (eval/learn/work/tune).
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
STUDY_DIR=eval/learn
. eval/sleep/lib.sh
LEARN_DIR=eval/learn
SLEEP_WORK=${SLEEP_WORK:-$SLEEP_DIR/work/locomo}
TUNE=${TUNE:-$LEARN_DIR/work/tune}
OUT=$LEARN_DIR/tune
SPLIT=$SLEEP_DIR/split-locomo.json
DEV="conv-30 conv-47 conv-49"
[ $# -eq 0 ] || { sed -n '2,24p' "$0"; exit 0; }
[ "$EMBEDDER" = e5 ] || die "the tuning grid runs e5 (the sleep study's base databases were ingested with it)"

for conv in $DEV; do
  [ -f "$SLEEP_WORK/$conv/base/chanda.db" ] || die "$SLEEP_WORK/$conv/base not found (the sleep study's base database; set SLEEP_WORK)"
done
build_bins
read -r -a EMB <<<"$(embed_flags)"
check_keys "K" -set read.learned_keys=true -set read.learned_keys_weight=1 -set read.learned_keys_top_k=20 -set ranking.utility_lambda=0.1
check_keys "keys rebuild" -set write.learned_keys=true -admin
check_embed
mkdir -p "$TUNE" "$OUT"

llm_free() {
  "$PY" -c 'import json,sys; c=(json.load(open(sys.argv[1])).get("output") or {}).get("cost") or {}; sys.exit(0 if int(c.get("llm_calls", 0)) == 0 else 1)' "$1"
}

# variant NAME PHASE1(0|1) KEYS(0|1) key=value...
variant() {
  local name=$1 phase1=$2 keys=$3; shift 3
  local vd=$TUNE/$name sets=() kv conv
  mkdir -p "$vd"
  : >"$vd/sets.txt.part"
  for kv in "$@"; do sets+=(-set "$kv"); echo "$kv" >>"$vd/sets.txt.part"; done
  if [ -f "$vd/sets.txt" ] && ! cmp -s "$vd/sets.txt" "$vd/sets.txt.part"; then
    die "$vd was run with other overrides ($(tr '\n' ' ' <"$vd/sets.txt")): delete it to rerun"
  fi
  mv "$vd/sets.txt.part" "$vd/sets.txt"
  for conv in $DEV; do
    local db=$vd/$conv out=$vd/$conv.test.ask.jsonl
    [ -f "$out" ] && continue
    log "== $name $conv: $*"
    rm -rf "$db"
    "$LME_BIN" study copy -from "$SLEEP_WORK/$conv/base" -to "$db"
    if [ "$keys" = 1 ]; then
      "$LME_BIN" study keys -db "$db" -action rebuild "${EMB[@]}" "${sets[@]}" -set write.learned_keys=true -out "$vd/$conv.keys.json"
    fi
    if [ "$phase1" = 1 ]; then
      "$LME_BIN" study sleep -db "$db" -condition "$name" -phases 1 "${EMB[@]}" "${sets[@]}" -out "$vd/$conv.sleep.json"
      llm_free "$vd/$conv.sleep.json" || die "$name $conv: phase 1 reports LLM calls"
    fi
    "$LME_BIN" study leakcheck -protocol learn -db "$db" -split "$SPLIT" -out "$vd/$conv.leak.json" >&2 ||
      die "$name $conv: leak check failed (see $vd/$conv.leak.json)"
    "$LME_BIN" study ask -db "$db" -split "$SPLIT" -half test -condition "$name" "${EMB[@]}" "${sets[@]}" -out "$out.part"
    mv "$out.part" "$out"
    [ "${KEEP_DBS:-0}" = 1 ] || rm -rf "$db"
  done
}

variant U0 0 0 ranking.utility_lambda=0
for lam in 0.1 0.2 0.3; do
  for me in 1 2 5; do
    variant "U1-l$lam-m$me" 1 0 "ranking.utility_lambda=$lam" "ranking.min_exposures=$me"
  done
done
read -r BL BM <<<"$("$PY" "$LEARN_DIR/tune_summary.py" --tune "$TUNE" --best-u1)" || die "no best U1"
log "best U1: ranking.utility_lambda=$BL ranking.min_exposures=$BM"
for w in 0.5 1 2; do
  for k in 10 20; do
    variant "K-w$w-k$k" 1 1 "ranking.utility_lambda=$BL" "ranking.min_exposures=$BM" read.learned_keys=true \
      "read.learned_keys_weight=$w" "read.learned_keys_top_k=$k"
  done
done
variant K-alone 1 1 ranking.utility_lambda=0 read.learned_keys=true

"$PY" "$LEARN_DIR/tune_summary.py" --tune "$TUNE" --md "$OUT/grid.md" --json "$OUT/grid.json"
log "grid: $OUT/grid.md, $OUT/grid.json"
