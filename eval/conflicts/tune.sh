#!/usr/bin/env bash
# Development tuning of the conflict study (LLM verdicts, small). Needs
# Part 0 first (precheck.sh: work/dev/<qid>/base and its D2 block, the C0
# reference). Per grid point and development history: copy the base; when
# the point's min_cosine is not the ingestion's (BASE_MIN_COSINE), reset
# the queue and rescan it with -write at that cosine; sleep phases 1,2 in
# maintenance mode with conflicts on, through the proxy (tag
# conflicts-dev-<point>-sleep); leak check (hard stop); ask once; list the
# queue (verdicts, supersessions). tune.py then writes
# eval/conflicts/tune/grid.{md,json}: stale delivered (stale-measure
# questions), hidden needed evidence (superseded drops only), verdict tally
# and verdict correctness on the knowledge-update (outdated, current) pairs.
#
#   eval/conflicts/tune.sh                        projection only (no LLM call)
#   eval/conflicts/tune.sh --paid --start-proxy   the grid (starts lmeproxy on eval/conflicts/cost.json)
#   eval/conflicts/tune.sh --paid                 through a proxy already at $PROXY_URL
#   eval/conflicts/tune.sh --paid --qa            also the official reader + judge (tags conflicts-dev-*)
#
# Environment: GRID (space-separated min_cosine:context_turns:min_confidence
# points, default "0.80:1:0.7 0.75:1:0.7"), IDS (default: the 52
# development questions), BASE_MIN_COSINE (0.80, the cosine the bases were
# ingested with), TUNE_SET (extra key=value overrides, e.g.
# sleep.conflicts.max_pairs=40), PROXY_URL, EMBEDDER (e5), EMBED_URL,
# LME_DATA, CAP_USD (2), OFFPEAK_ONLY (0), KEEP_DBS (0). Resumable.
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
. eval/conflicts/lib.sh

PAID=0 START_PROXY=0 QA=0
while [ $# -gt 0 ]; do
  case "$1" in
    --paid) PAID=1 ;;
    --start-proxy) START_PROXY=1 ;;
    --qa) QA=1 ;;
    -h|--help) sed -n '2,25p' "$0"; exit 0 ;;
    *) die "unknown flag $1" ;;
  esac
  shift
done
DEV_WORK=${DEV_WORK:-$CONF_DIR/work/dev}
TUNE=${TUNE:-$CONF_DIR/work/tune}
OUT=$CONF_DIR/tune
GRID=${GRID:-0.80:1:0.7 0.75:1:0.7}
BASE_MIN_COSINE=${BASE_MIN_COSINE:-0.80}
IDS=${IDS:-$(set_ids development)}
lme_data_abs
EXTRA=()
for kv in ${TUNE_SET:-}; do EXTRA+=(-set "$kv"); done

for q in $IDS; do
  [ -f "$DEV_WORK/$q/base/study.json" ] && [ -f "$DEV_WORK/$q/D2.ask.jsonl" ] ||
    die "$DEV_WORK/$q is not complete: run eval/conflicts/precheck.sh first (the bases and the C0 blocks)"
done
build_bins
read -r -a EMB <<<"$(embed_flags)"

# point_sets MC CT CF: the point's overrides into PS.
point_sets() {
  PS=(-set "sleep.conflicts.min_cosine=$1" -set "sleep.conflicts.context_turns=$2" -set "sleep.conflicts.min_confidence=$3" "${EXTRA[@]}")
}
for pt in $GRID; do
  IFS=: read -r mc ct cf <<<"$pt"
  [ -n "$cf" ] || die "GRID point '$pt' is not min_cosine:context_turns:min_confidence"
  point_sets "$mc" "$ct" "$cf"
  conf_check "tune $pt" "${PS[@]}" "${MAINT[@]}" -admin -llm-url "$PROXY_URL/tag/check/v1"
done
check_tool

WINDOW=offpeak
[ "${OFFPEAK_ONLY:-0}" = 1 ] || WINDOW=peak
"$PY" "$CONF_DIR/project_cost.py" tune --precheck "$CONF_DIR/precheck/precheck.json" --grid "$GRID" --ids "$IDS" \
  --tune "$TUNE" --cost "$COST_FILE" --cap "$CAP_USD" --window "$WINDOW" ${TUNE_SET:+--extra "$TUNE_SET"} $( [ $QA = 1 ] && echo --qa ) ||
  die "the projection exceeds the US\$$CAP_USD cap: shrink GRID or IDS, or ask"
[ $PAID = 1 ] || { log "no LLM call made; pass --paid to run the grid"; exit 0; }
check_embed
if [ $START_PROXY = 1 ]; then start_proxy; else check_proxy; fi

for pt in $GRID; do
  IFS=: read -r mc ct cf <<<"$pt"
  point_sets "$mc" "$ct" "$cf"
  label="mc$mc-ct$ct-cf$cf"
  vd=$TUNE/$label
  mkdir -p "$vd"
  printf '%s\n' "${PS[@]}" | grep -v '^-set$' >"$vd/sets.txt.part"
  if [ -f "$vd/sets.txt" ] && ! cmp -s "$vd/sets.txt" "$vd/sets.txt.part"; then
    die "$vd was run with other overrides ($(tr '\n' ' ' <"$vd/sets.txt")): delete it to rerun"
  fi
  mv "$vd/sets.txt.part" "$vd/sets.txt"
  for q in $IDS; do
    d=$vd/$q db=$vd/$q/db
    [ -f "$d/list.json" ] && continue
    log "== $label $q"
    mkdir -p "$d"
    if [ ! -f "$d/sleep.json" ]; then
      rm -rf "$db" "$d/rescan.json" "$d/reset.json"
      "$LME_BIN" study copy -from "$DEV_WORK/$q/base" -to "$db" >&2
      if [ "$mc" != "$BASE_MIN_COSINE" ]; then
        conflicts_step "$db" reset "$d/reset.json" "${PS[@]}"
        conflicts_step "$db" scan "$d/rescan.json" -write -min-cosine "$mc" "${PS[@]}"
      fi
      "$LME_BIN" study sleep -db "$db" -condition "dev-$label" -phases 1,2 "${EMB[@]}" "${CONF_ON[@]}" "${PS[@]}" "${MAINT[@]}" \
        -llm-url "$PROXY_URL/tag/conflicts-dev-$label-sleep/v1" -allow-llm -out "$d/sleep.json.part" >&2 ||
        die "sleep $label $q failed (rerun resumes)"
      mv "$d/sleep.json.part" "$d/sleep.json"
    fi
    [ -d "$db" ] || die "$db is gone but $d/list.json is missing: delete $d and rerun"
    if [ ! -f "$d/ask.jsonl" ]; then
      leak "$db" "$d/leak.json"
      ask_once "$db" "$d/ask.jsonl" "${CONF_ON[@]}" "${PS[@]}"
    fi
    conflicts_step "$db" list "$d/list.json" "${PS[@]}"
    [ "${KEEP_DBS:-0}" = 1 ] || rm -rf "$db"
  done
  if [ $QA = 1 ]; then
    for side in C0 "$label"; do
      qd=$TUNE/qa-$side
      [ -f "$qd/D.jsonl" ] && [ "$side" = C0 ] && continue
      mkdir -p "$qd"
      : >"$qd/contexts.jsonl.part"
      for q in $IDS; do
        if [ "$side" = C0 ]; then cat "$DEV_WORK/$q/D2.ask.jsonl"; else cat "$vd/$q/ask.jsonl"; fi >>"$qd/contexts.jsonl.part"
      done
      mv "$qd/contexts.jsonl.part" "$qd/contexts.jsonl"
      write_sample development "$qd/sample.json"
      "$PY" eval/lme/py/answer.py --condition D --contexts "$qd/contexts.jsonl" --dataset "$LME_DATA" --sample "$qd/sample.json" \
        --questions $IDS --proxy "$PROXY_URL" --out-dir "$qd" --tag "conflicts-dev-$side-reader" || die "reader $side did not finish (rerun resumes)"
      "$PY" eval/lme/py/judge.py --condition D --raw-dir "$qd" --proxy "$PROXY_URL" --tag "conflicts-dev-$side-judge" ||
        die "judge $side did not finish (rerun resumes)"
    done
  fi
done

mkdir -p "$OUT"
"$PY" "$CONF_DIR/tune.py" --tune "$TUNE" --dev "$DEV_WORK" --data "$LME_DATA" --ids "$IDS" --grid "$GRID" --cost "$COST_FILE" \
  --md "$OUT/grid.md" --json "$OUT/grid.json"
log "grid: $OUT/grid.md, $OUT/grid.json"
