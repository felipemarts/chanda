#!/bin/sh
# End-to-end test of the agent adapters against a real `chanda serve`.
#
# Builds chanda, seeds a document (when the core is wired), runs the Go
# example and the Python generic adapter, and checks that `context` is called
# before the first turn and its block injected. With the core not wired yet,
# every tool answers `unavailable` and the checks are that the agents still
# run, without injection, and that the calls come in the right order.
#
#   sh adapters/e2e_test.sh
#
# Needs Go and Python >= 3.11 with the `mcp` package; pick the interpreter
# with CHANDA_E2E_PYTHON (default: python3.11, else python3).
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PY=${CHANDA_E2E_PYTHON:-}
if [ -z "$PY" ]; then
	if command -v python3.11 >/dev/null 2>&1; then PY=python3.11; else PY=python3; fi
fi
if ! "$PY" -c 'import sys, mcp; assert sys.version_info >= (3, 11)' 2>/dev/null; then
	echo "e2e: $PY must be Python >= 3.11 with the mcp package installed (set CHANDA_E2E_PYTHON)" >&2
	exit 2
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/chanda-e2e.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

echo "e2e: building chanda"
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$WORK/chanda" ./cmd/chanda)
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$WORK/chanda-agent-example" ./adapters/generic/go)

cat >"$WORK/chanda.yaml" <<EOF
db_path: $WORK/chanda.db
namespace: e2e
EOF

REQUEST="What is the Project Orion launch codename?"
EXPECT="BLUEFINCH-7"
mkdir "$WORK/docs"
printf '# Project Orion\n\nThe Project Orion launch codename is %s.\n' "$EXPECT" >"$WORK/docs/orion.md"

# The core is wired when ingest succeeds; an unwired build answers unavailable.
if "$WORK/chanda" ingest --config "$WORK/chanda.yaml" "$WORK/docs" >"$WORK/ingest.json" 2>"$WORK/ingest.err"; then
	MODE=wired
elif grep -q '"code": "unavailable"' "$WORK/ingest.json"; then
	MODE=unwired
else
	echo "e2e: chanda ingest failed unexpectedly" >&2
	cat "$WORK/ingest.json" "$WORK/ingest.err" >&2
	exit 1
fi
echo "e2e: core is $MODE"

echo "e2e: Go example"
"$WORK/chanda-agent-example" --chanda "$WORK/chanda" --config "$WORK/chanda.yaml" \
	--request "$REQUEST" --session e2e-go --value 1 >"$WORK/go.json" 2>"$WORK/go.err" || {
	cat "$WORK/go.err" >&2
	exit 1
}

echo "e2e: Python generic adapter"
"$PY" "$ROOT/adapters/e2e_agent.py" --chanda "$WORK/chanda" --config "$WORK/chanda.yaml" \
	--serve-log "$WORK/serve.log" --mode "$MODE" --request "$REQUEST" --expect "$EXPECT" \
	--go-report "$WORK/go.json" || {
	echo "--- server stderr ---" >&2
	cat "$WORK/serve.log" >&2
	exit 1
}

if [ "$MODE" = unwired ]; then
	echo "e2e: NOTE the core is not wired: injection of a real block, log_session and outcome success were not checked"
fi
