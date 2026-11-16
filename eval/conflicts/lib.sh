# Shared helpers of the conflict study's drivers (precheck.sh, tune.sh,
# suite/run_suite.sh, run_test.sh); sourced (bash) after cd to the
# repository root. Builds on eval/sleep/lib.sh with this study's own cost
# file (eval/conflicts/cost.json), call log (eval/conflicts/llm_calls.jsonl)
# and the shared call store eval/lme/llmstore (replay on).

STUDY_DIR=eval/conflicts
CAP_USD=${CAP_USD:-2}
. eval/sleep/lib.sh
CONF_DIR=eval/conflicts
LME_DATA=${LME_DATA:-eval/lme/data/longmemeval_s_cleaned.json}
SETS=$CONF_DIR/sets.json

# Every step of the study runs with conflict detection on (decision 0148);
# sleep adds maintenance mode.
CONF_ON=(-set sleep.conflicts.enabled=true)
MAINT=(-set sleep.mode=maintenance)

lme_data_abs() {
  [ -f "$LME_DATA" ] || die "$LME_DATA not found (cd eval/lme && bin/lme fetch)"
  LME_DATA=$(cd "$(dirname "$LME_DATA")" && pwd)/$(basename "$LME_DATA")
}

# set_ids SET [knowledge_update|other|all]: question ids in sets.json order
# (knowledge-update first, then the others).
set_ids() {
  "$PY" - "$SETS" "$1" "${2:-all}" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))[sys.argv[2]]
kind = sys.argv[3]
ids = (s["knowledge_update"] if kind in ("all", "knowledge_update") else []) + (s["other"] if kind in ("all", "other") else [])
print(" ".join(ids))
PY
}

# write_sample SET FILE: an answer.py --sample file ({"questions": [...]})
# holding the set's ids in sets order.
write_sample() {
  "$PY" - "$SETS" "$1" "$2" <<'PY'
import json, os, sys
s = json.load(open(sys.argv[1]))[sys.argv[2]]
os.makedirs(os.path.dirname(os.path.abspath(sys.argv[3])), exist_ok=True)
json.dump({"set": sys.argv[2], "questions": s["knowledge_update"] + s["other"]}, open(sys.argv[3], "w"), indent=1)
PY
}

# read_conf_set FILE: the conflict keys of a key=value file into CONF_SETS.
# Allowed: sleep.conflicts.{min_cosine,max_per_record,context_turns,user_only,require_entity,
# min_confidence,max_pairs}; REQUIRED (space-separated) must all be given.
CONF_SETS=()
read_conf_set() {
  local f=$1 line k v seen=""
  CONF_SETS=()
  while IFS= read -r line || [ -n "$line" ]; do
    line=${line%%#*}
    line=$(printf '%s' "$line" | tr -d '[:space:]')
    [ -n "$line" ] || continue
    case "$line" in *=*) ;; *) die "$f: '$line' is not key=value" ;; esac
    k=${line%%=*} v=${line#*=}
    [ -n "$v" ] || die "$f: $k has no value"
    case "$k" in
      sleep.conflicts.min_cosine|sleep.conflicts.max_per_record|sleep.conflicts.context_turns|sleep.conflicts.min_confidence|sleep.conflicts.max_pairs|sleep.conflicts.user_only|sleep.conflicts.require_entity)
        CONF_SETS+=(-set "$k=$v") ;;
      *) die "$f: $k is not a conflict key of this study (sleep.conflicts.min_cosine, max_per_record, context_turns, min_confidence, max_pairs, user_only, require_entity)" ;;
    esac
    case " $seen " in *" $k "*) die "$f: $k given twice" ;; esac
    seen="$seen $k"
  done <"$f"
  for k in ${REQUIRED:-}; do
    case " $seen " in *" $k "*) ;; *) die "$f: $k is missing (every tuned key is given explicitly)" ;; esac
  done
}

# retrievals DB: the number of retrievals logged in DB/chanda.db.
retrievals() {
  "$PY" -c 'import os,sqlite3,sys; db=sys.argv[1]; m="ro" if os.path.exists(db+"-wal") else "ro&immutable=1"
print(sqlite3.connect("file:"+db+"?mode="+m, uri=True).execute("SELECT count(*) FROM retrievals").fetchone()[0])' "$1/chanda.db"
}

# ingest_base QID DIR SETS...: the haystack into DIR as condition D does,
# conflict detection on. Kept when DIR/study.json exists.
ingest_base() {
  local q=$1 d=$2; shift 2
  [ -f "$d/study.json" ] && return 0
  rm -rf "$d"
  "$LME_BIN" study ingest -db "$d" -data "$LME_DATA" -question "$q" "${EMB[@]}" "${CONF_ON[@]}" "$@" >&2
}

# ask_once DB OUT SETS...: the directory's question once (as_of = the
# question date, D2 selection), into OUT via OUT.part.
ask_once() {
  local db=$1 out=$2; shift 2
  [ -f "$out" ] && return 0
  "$LME_BIN" study ask -db "$db" -condition D "${EMB[@]}" "$@" -out "$out.part" >&2
  mv "$out.part" "$out"
}

# leak DB OUT: leakcheck -protocol conflicts (hard stop), kept when done.
leak() {
  local db=$1 out=$2
  [ -f "$out" ] && return 0
  "$LME_BIN" study leakcheck -protocol conflicts -db "$db" -out "$out.part" >&2 ||
    die "leak check failed on $db (see $out.part): hard stop (PROTOCOL.md, Stops)"
  mv "$out.part" "$out"
}

# conflicts DB ACTION OUT SETS...: the conflicts admin step into OUT.
conflicts_step() {
  local db=$1 action=$2 out=$3; shift 3
  [ -f "$out" ] && return 0
  "$LME_BIN" study conflicts -db "$db" -action "$action" "${EMB[@]}" "${CONF_ON[@]}" "$@" -out "$out.part" >/dev/null ||
    die "conflicts $action on $db failed"
  mv "$out.part" "$out"
}

# conf_check WHAT SETS...: this build accepts the configuration (and has
# the conflicts tool: check_tool).
conf_check() {
  local what=$1; shift
  check_keys "$what" "${CONF_ON[@]}" "$@"
}

# check_tool: the conflicts admin tool exists on this build (a throwaway
# hash-embedder suite database, no LLM).
check_tool() {
  local tmp
  tmp=$(mktemp -d)
  "$LME_BIN" study ingest -db "$tmp/db" -suite "$CONF_DIR/suite/pairs.json" -pair u01 -embedder hash "${CONF_ON[@]}" >/dev/null 2>&1 ||
    { rm -rf "$tmp"; die "ingest with sleep.conflicts.enabled=true failed on this build (decision 0148 not implemented?)"; }
  if ! "$LME_BIN" study conflicts -db "$tmp/db" -action stats -embedder hash "${CONF_ON[@]}" >/dev/null 2>"$tmp/err"; then
    local msg
    msg=$(tail -1 "$tmp/err"); rm -rf "$tmp"
    die "the conflicts tool is not usable on this build: $msg"
  fi
  rm -rf "$tmp"
}
