# Shared helpers of run_bench.sh and run_locomo.sh, and of eval/learn/run_learn.sh
# (sourced, bash). A driver of another study sets STUDY_DIR (its cost file,
# call log and proxy log live there) and CAP_USD before sourcing.
# Paths are relative to the repository root; the drivers cd there first.

SLEEP_DIR=eval/sleep
LME_BIN=eval/lme/bin/lme
PROXY_BIN=eval/lme/bin/lmeproxy
STUDY_DIR=${STUDY_DIR:-$SLEEP_DIR}
COST_FILE=$STUDY_DIR/cost.json
CALL_LOG=$STUDY_DIR/llm_calls.jsonl
STORE_DIR=eval/lme/llmstore
CAP_USD=${CAP_USD:-2}
PROXY_URL=${PROXY_URL:-http://127.0.0.1:8787}
EMBEDDER=${EMBEDDER:-e5}
EMBED_URL=${EMBED_URL:-http://127.0.0.1:8089}
PY=${PY:-python3}

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
die() { log "STOP: $*"; exit 1; }

build_bins() {
  log "building $LME_BIN and $PROXY_BIN"
  (cd eval/lme && CGO_ENABLED=0 GOEXPERIMENT=simd go build -o bin/lme ./cmd/lme) || die "build lme"
  CGO_ENABLED=0 go build -o "$PROXY_BIN" ./eval/cmd/lmeproxy || die "build lmeproxy"
}

# embed_flags prints the lme study embedder flags.
embed_flags() {
  if [ "$EMBEDDER" = e5 ]; then
    printf -- '-embedder e5 -embed-url %s' "$EMBED_URL"
  else
    printf -- '-embedder %s' "$EMBEDDER"
  fi
}

check_embed() {
  [ "$EMBEDDER" = e5 ] || return 0
  curl -fsS "$EMBED_URL/v1/models" >/dev/null 2>&1 ||
    die "no embed server at $EMBED_URL: start one e5 process, e.g. '$LME_BIN embedserve -embedder e5 -e5-dir \$CHANDA_HUGOT_MODELS/e5-small -addr ${EMBED_URL#http://}'"
}

PROXY_PID=
# start_proxy starts lmeproxy on the study's cost file with the call store
# and replay on, off-peak only, the cap at $CAP_USD. The API key is read
# from .env into the proxy's environment only; it is never printed.
start_proxy() {
  [ -f .env ] || die ".env not found (DEEPSEEK_API_KEY)"
  local addr=${PROXY_URL#http://}
  log "starting lmeproxy on $addr: cost $COST_FILE, cap \$$CAP_USD, store $STORE_DIR (replay), off-peak only: ${OFFPEAK_ONLY:-1}"
  (
    set -a; . ./.env; set +a
    exec "$PROXY_BIN" -listen "$addr" -cap-usd "$CAP_USD" -cost "$COST_FILE" -log "$CALL_LOG" \
      -store "$STORE_DIR" -replay $( [ "${OFFPEAK_ONLY:-1}" = 1 ] && echo -offpeak-only )
  ) 2>>"$STUDY_DIR/lmeproxy.log" &
  PROXY_PID=$!
  trap 'stop_proxy' EXIT
  for _ in $(seq 1 50); do
    curl -s -o /dev/null "$PROXY_URL/" 2>/dev/null && return 0
    kill -0 "$PROXY_PID" 2>/dev/null || die "lmeproxy exited (see $STUDY_DIR/lmeproxy.log)"
    sleep 0.2
  done
  die "lmeproxy did not come up at $PROXY_URL"
}

stop_proxy() {
  if [ -n "$PROXY_PID" ] && kill -0 "$PROXY_PID" 2>/dev/null; then
    kill -TERM "$PROXY_PID" 2>/dev/null
    wait "$PROXY_PID" 2>/dev/null
  fi
}

# check_proxy: a proxy started elsewhere must answer at $PROXY_URL and hold
# the lock of this study's cost file (lmeproxy locks <cost>.lock), which is
# the sign it runs with -cost $COST_FILE. Its other flags (-store,
# -replay, -offpeak-only, the cap) cannot be checked from here.
check_proxy() {
  curl -s -o /dev/null "$PROXY_URL/" 2>/dev/null || die "no proxy at $PROXY_URL (pass --start-proxy, or start lmeproxy with -cost $COST_FILE -store $STORE_DIR -replay -offpeak-only -cap-usd $CAP_USD)"
  [ -e "$COST_FILE.lock" ] || die "$COST_FILE.lock is missing: the proxy at $PROXY_URL does not run on $COST_FILE"
  log "using the proxy at $PROXY_URL (assumed: -cost $COST_FILE -store $STORE_DIR -replay -offpeak-only -cap-usd $CAP_USD)"
}

# spent prints the US$ spent so far on the study's cost file.
spent() {
  "$PY" -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get("spent_usd") or 0)
except FileNotFoundError: print(0)' "$COST_FILE"
}

# check_keys validates a configuration (Chanda's own loader refuses unknown
# keys) before any step, so a missing feature stops the driver at once.
check_keys() {
  local what=$1; shift
  local err
  err=$(mktemp)
  if ! "$LME_BIN" study check -embedder hash "$@" >/dev/null 2>"$err"; then
    local msg
    msg=$(tail -1 "$err"); rm -f "$err"
    die "configuration for $what is refused by this build: $msg"
  fi
  rm -f "$err"
}
