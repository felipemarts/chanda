module github.com/felipemarts/chanda-mcp/embedders/hugot

go 1.27.0

require (
	github.com/felipemarts/chanda-mcp v0.0.0
	github.com/knights-analytics/hugot v0.8.6
	golang.org/x/text v0.44.0
)

require (
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
	github.com/google/uuid v1.6.0 // indirect
	github.com/knights-analytics/ortgenai v0.3.5 // indirect
	github.com/microsoft/onnxruntime/go v0.0.0-20261111070922-6cc3c09506ec // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/exp v0.0.0-20261109185158-a52e3aef5920 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
)

replace (
	// The main module, from this repository (decision 0033).
	github.com/felipemarts/chanda-mcp => ../..
	// Unigram tokenizer fixed to Viterbi; see third_party/go-huggingface-patched/PATCH.md.
	github.com/gomlx/go-huggingface => ./third_party/go-huggingface-patched
)
