//go:build goexperiment.simd

package hugot

// Flavour is the build flavour recorded in model ids: this binary was built
// with GOEXPERIMENT=simd, whose kernels change vectors by about 1e-7.
const Flavour = "simd"
