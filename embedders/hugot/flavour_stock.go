//go:build !goexperiment.simd

package hugot

// Flavour is the build flavour recorded in model ids: this binary was built
// without GOEXPERIMENT=simd (the portable kernels).
const Flavour = "stock"
