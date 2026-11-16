#!/usr/bin/env bash
# Part 2 of the conflict study: the Portuguese regression suite
# (suite/pairs.json, 40 scripted pairs: 20 updates, 20 controls). One
# database per pair: log_session of the older session, then the newer one
# (session ids <pair>-s1, <pair>-s2; turn times from the pair), conflict
# detection on; the queue is listed; sleep in maintenance mode with
# conflicts on through the proxy (tag conflicts-suite; call store with
# replay, so a rerun is free); the queue is listed again. score.py then
# writes suite/results.{md,json}: detected among the 20 updates, the
# verdict of each detected pair, applied supersessions, false
# supersessions among the 20 controls.
#
#   eval/conflicts/suite/run_suite.sh                        detection only (no LLM call), then the score so far
#   eval/conflicts/suite/run_suite.sh --paid --start-proxy   with the verdicts (starts lmeproxy)
#   eval/conflicts/suite/run_suite.sh --paid                 through a proxy already at $PROXY_URL
#   eval/conflicts/suite/run_suite.sh --embedder hash        the hash embedder (results-hash.{md,json})
#
# Settings: SET_FILE (default eval/conflicts/frozen.set; the protocol runs
# the suite on the frozen settings, before the test). Stop: more than 2
# false supersessions among the controls -> exit status 4.
# Environment: PROXY_URL, EMBED_URL, CAP_USD (2), OFFPEAK_ONLY (0), WORK
# (eval/conflicts/work/suite/<embedder>), PAIRS (all 40).
set -eo pipefail
cd "$(git rev-parse --show-toplevel)"
. eval/conflicts/lib.sh

PAID=0 START_PROXY=0
EMBEDDER=${SUITE_EMBEDDER:-e5}
while [ $# -gt 0 ]; do
  case "$1" in
    --paid) PAID=1 ;;
    --start-proxy) START_PROXY=1 ;;
    --embedder) EMBEDDER=$2; shift ;;
    -h|--help) sed -n '2,23p' "$0"; exit 0 ;;
    *) die "unknown flag $1" ;;
  esac
  shift
done
case "$EMBEDDER" in e5|hash) ;; *) die "--embedder: hash or e5" ;; esac
SUITE=$CONF_DIR/suite
WORK=${WORK:-$CONF_DIR/work/suite/$EMBEDDER}
SET_FILE=${SET_FILE:-$CONF_DIR/frozen.set}
[ -f "$SET_FILE" ] || die "$SET_FILE not found: freeze the tuned settings first, or pass SET_FILE=<key=value file> for a development run"
REQUIRED="" read_conf_set "$SET_FILE"
PAIRS=${PAIRS:-$("$PY" -c 'import json,sys; print(" ".join(p["id"] for p in json.load(open(sys.argv[1]))["pairs"]))' "$SUITE/pairs.json")}
SUFFIX=""
[ "$EMBEDDER" = e5 ] || SUFFIX="-$EMBEDDER"

build_bins
read -r -a EMB <<<"$(embed_flags)"
conf_check "suite (maintenance)" "${CONF_SETS[@]}" "${MAINT[@]}" -admin -llm-url "$PROXY_URL/tag/check/v1"
check_tool
check_embed
if [ $PAID = 1 ]; then
  if [ $START_PROXY = 1 ]; then start_proxy; else check_proxy; fi
fi
mkdir -p "$WORK"
printf '%s\n' "${CONF_SETS[@]}" | grep -v '^-set$' >"$WORK/sets.txt.part"
if [ -f "$WORK/sets.txt" ] && ! cmp -s "$WORK/sets.txt" "$WORK/sets.txt.part"; then
  die "$WORK was run with other settings ($(tr '\n' ' ' <"$WORK/sets.txt")): delete it to rerun"
fi
mv "$WORK/sets.txt.part" "$WORK/sets.txt"

for p in $PAIRS; do
  d=$WORK/$p
  if [ ! -f "$d/db/study.json" ]; then
    rm -rf "$d"
    mkdir -p "$d"
    "$LME_BIN" study ingest -db "$d/db" -suite "$SUITE/pairs.json" -pair "$p" "${EMB[@]}" "${CONF_ON[@]}" "${CONF_SETS[@]}" >&2
  fi
  conflicts_step "$d/db" list "$d/queue.json" "${CONF_SETS[@]}"
  [ $PAID = 1 ] || continue
  if [ ! -f "$d/sleep.json" ]; then
    "$LME_BIN" study sleep -db "$d/db" -condition suite -phases 1,2 "${EMB[@]}" "${CONF_ON[@]}" "${CONF_SETS[@]}" "${MAINT[@]}" \
      -llm-url "$PROXY_URL/tag/conflicts-suite/v1" -allow-llm -out "$d/sleep.json.part" >&2 || die "sleep on $p failed (rerun resumes)"
    mv "$d/sleep.json.part" "$d/sleep.json"
    rm -f "$d/after.json"
  fi
  conflicts_step "$d/db" list "$d/after.json" "${CONF_SETS[@]}"
done

set +e
"$PY" "$SUITE/score.py" --work "$WORK" --pairs "$SUITE/pairs.json" --embedder "$EMBEDDER" --cost "$COST_FILE" \
  --md "$SUITE/results$SUFFIX.md" --json "$SUITE/results$SUFFIX.json"
st=$?
set -e
[ $st = 0 ] || { [ $st = 4 ] && die "the suite applied more than 2 false supersessions among its 20 controls: STOP (PROTOCOL.md, Stops); see $SUITE/results$SUFFIX.md"; die "score.py failed ($st)"; }
log "suite: $SUITE/results$SUFFIX.md"
[ $PAID = 1 ] || log "detection only: pass --paid for the verdicts"
