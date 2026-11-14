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
#   make release   static binaries for linux/darwin/windows (amd64, arm64) + SHA256SUMS in dist/

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -X github.com/felipemarts/chanda-mcp/internal/cli.Version=$(VERSION)
EVAL_ARGS ?=

export CGO_ENABLED = 0

.PHONY: all build test lint eval dataset hugot e2e perf release clean

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

PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

release:
	mkdir -p dist
	rm -f dist/chanda-* dist/SHA256SUMS
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ "$$os" = windows ] && ext=.exe; \
		out=dist/chanda-$(VERSION)-$$os-$$arch$$ext; \
		echo "build $$out"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o $$out ./cmd/chanda; \
	done
	cd dist && (command -v sha256sum >/dev/null && sha256sum chanda-* || shasum -a 256 chanda-*) > SHA256SUMS

clean:
	rm -rf bin dist
