// Package hugot is Chanda's optional multilingual embedder and reranker,
// running ONNX models in pure Go through knights-analytics/hugot (GoMLX
// simplego backend, no cgo). It lives in its own Go module because hugot
// needs Go 1.27 and a patched tokenizer (third_party/go-huggingface-patched);
// the main module stays on Go 1.26 and the built-in hash embedder.
//
// Model files are read from a local directory; nothing is downloaded at read
// or write time (see Pull for the install step). Decisions 0033 to 0036.
package hugot

import (
	_ "github.com/felipemarts/chanda-mcp/internal/core"
	_ "github.com/felipemarts/chanda-mcp/pkg/chanda/embedreg"
	_ "github.com/knights-analytics/hugot"
	_ "golang.org/x/text/unicode/norm"
)
