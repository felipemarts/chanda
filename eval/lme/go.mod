module github.com/felipemarts/chanda-mcp/eval/lme

go 1.27.0

require (
	github.com/felipemarts/chanda-mcp v0.0.0
	github.com/felipemarts/chanda-mcp/embedders/hugot v0.0.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.62.0
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/eliben/go-sentencepiece v0.7.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/gofrs/flock v0.13.2 // indirect
	github.com/gomlx/compute v0.1.28 // indirect
	github.com/gomlx/compute-onnx v0.1.28 // indirect
	github.com/gomlx/exceptions v0.0.3 // indirect
	github.com/gomlx/go-huggingface v0.4.32 // indirect
	github.com/gomlx/go-xla v0.4.28 // indirect
	github.com/gomlx/gomlx v0.28.34 // indirect
	github.com/gomlx/onnx-gomlx v0.5.31 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/knights-analytics/hugot v0.8.6 // indirect
	github.com/knights-analytics/ortgenai v0.3.5 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/microsoft/onnxruntime/go v0.0.0-20261111070922-6cc3c09506ec // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/exp v0.0.0-20261109185158-a52e3aef5920 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace (
	// The main module and the hugot embedder, from this repository.
	github.com/felipemarts/chanda-mcp => ../..
	github.com/felipemarts/chanda-mcp/embedders/hugot => ../../embedders/hugot
	// Replace directives of a dependency are not inherited: repeat the
	// patched tokenizer the hugot module uses (its PATCH.md).
	github.com/gomlx/go-huggingface => ../../embedders/hugot/third_party/go-huggingface-patched
)
