#!/bin/sh
# Reproduce the evaluation report from a clean clone with one command:
#
#   sh scripts/reproduce.sh            # writes eval/reports/latest.{md,json,questions.jsonl}
#   sh scripts/reproduce.sh -date 2026-11-13   # pin the report date as well
#
# Needs only Go (see go.mod). No network after the module download, no CGO,
# no LLM. Everything above the "Latency" section of the report is a function
# of the dataset, the configuration (defaults unless -config is given), the
# date, the version and the hardware, so two runs on the same machine give
# the same report.
set -eu
cd "$(dirname "$0")/.."
export CGO_ENABLED=0
version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
go run ./eval/cmd/gensynth -out "${TMPDIR:-/tmp}/chanda-synthetic-check" >/dev/null
if ! diff -r "${TMPDIR:-/tmp}/chanda-synthetic-check" eval/data/synthetic-pt >/dev/null; then
	echo "eval/data/synthetic-pt differs from its generator; run: make dataset" >&2
	exit 1
fi
go build -ldflags "-X github.com/felipemarts/chanda-mcp/internal/cli.Version=$version" -o bin/chanda ./cmd/chanda
./bin/chanda eval -dataset eval/data/synthetic-pt -out eval/reports -name latest "$@"
