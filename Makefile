# Chanda build, test, lint and evaluation. Everything is CGO-free and offline.
#
#   make build     bin/chanda, stamped with the version from git describe
#   make test      go test ./... (main module)
#   make lint      go vet + gofmt check
#   make eval      regenerate eval/reports/latest.{md,json,questions.jsonl}
#   make dataset   regenerate eval/data/synthetic-pt from its seed
#   make hugot     bin/chanda-hugot (separate module, Go 1.27, GOEXPERIMENT=simd)
#   make e2e       adapters end-to-end test (needs Python with mcp installed)
#   make perf      100k-record latency measurement (BASE.md performance targets)

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -X github.com/felipemarts/chanda-mcp/internal/cli.Version=$(VERSION)
EVAL_ARGS ?=

export CGO_ENABLED = 0

.PHONY: all build test lint eval dataset hugot e2e perf clean

all: lint test build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/chanda ./cmd/chanda

test:
	go test -count=1 ./...

lint:
	go vet ./...
	@out="$$(gofmt -l . | grep -v '^embedders/hugot/third_party/' || true)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

eval: build
	./bin/chanda eval -dataset eval/data/synthetic-pt -out eval/reports -name latest $(EVAL_ARGS)

dataset:
	go run ./eval/cmd/gensynth

hugot:
	cd embedders/hugot && GOEXPERIMENT=simd go build -ldflags "$(LDFLAGS)" -o ../../bin/chanda-hugot ./cmd/chanda-hugot

perf:
	CHANDA_PERF=1 go test -count=1 -run TestPerformanceAt100k -v ./internal/app/

e2e: build
	sh adapters/e2e_test.sh

clean:
	rm -rf bin
