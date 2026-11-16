#!/usr/bin/env bash
# Part 0 of the conflict study (development set only, no LLM): for each of
# the 52 development questions (sets.json), ingest the haystack with
# sleep.conflicts.enabled=true (detection at write) into
# work/dev/<qid>/base, run the offline scan with details at each
# min_cosine of MIN_COSINES (no write), and ask the question once on a
# copy of the base (work/dev/<qid>/d2: D2 selection, as_of = question date;
# the base itself stays unasked, tune.sh copies it). precheck.py then
# writes eval/conflicts/precheck/{precheck.md,precheck.json}.
#
#   eval/conflicts/precheck.sh               every development question
#   IDS="01493427 22d2cb42" eval/conflicts/precheck.sh
#
# Environment: EMBEDDER (e5), EMBED_URL (http://127.0.0.1:8089, a running
# 'lme embedserve -embedder e5'), LME_DATA, WORK (eval/conflicts/work/dev),
# MIN_COSINES ("0.70 0.75 0.80 0.85"). Resumable: finished outputs are kept.
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
. eval/conflicts/lib.sh
[ $# -eq 0 ] || { sed -n '2,18p' "$0"; exit 0; }
WORK=${WORK:-$CONF_DIR/work/dev}
MIN_COSINES=${MIN_COSINES:-0.70 0.75 0.80 0.85}
IDS=${IDS:-$(set_ids development)}
lme_data_abs
build_bins
read -r -a EMB <<<"$(embed_flags)"
conf_check "ingest (conflicts on)"
check_tool
check_embed
mkdir -p "$WORK"

n=0
for q in $IDS; do
  n=$((n + 1))
  d=$WORK/$q
  log "== [$n] $q"
  ingest_base "$q" "$d/base"
  [ "$(retrievals "$d/base")" = 0 ] || die "$d/base holds retrievals: the base must stay unasked (delete $d and rerun)"
  for mc in $MIN_COSINES; do
    conflicts_step "$d/base" scan "$d/scan-$mc.json" -details -min-cosine "$mc"
  done
  if [ ! -f "$d/D2.ask.jsonl" ]; then
    rm -rf "$d/d2"
    "$LME_BIN" study copy -from "$d/base" -to "$d/d2" >&2
    ask_once "$d/d2" "$d/D2.ask.jsonl" "${CONF_ON[@]}"
    rm -rf "$d/d2"
  fi
done

mkdir -p "$CONF_DIR/precheck"
"$PY" "$CONF_DIR/precheck.py" --work "$WORK" --data "$LME_DATA" --min-cosines "$MIN_COSINES" --ids "$IDS" \
  --md "$CONF_DIR/precheck/precheck.md" --json "$CONF_DIR/precheck/precheck.json"
log "precheck: $CONF_DIR/precheck/precheck.md"
