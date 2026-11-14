package embed

import (
	"bytes"
	"math"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// BASE.md data model: embeddings.vector is little-endian float32 x dim.
func TestVectorEncoding(t *testing.T) {
	v := []float32{1, -0.5, 0, float32(math.SmallestNonzeroFloat32), 3.25}
	b := Encode(v)
	want := []byte{0, 0, 0x80, 0x3f, 0, 0, 0, 0xbf, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0x50, 0x40}
	if !bytes.Equal(b, want) {
		t.Fatalf("encode = % x", b)
	}
	got, err := Decode(b, len(v))
	if err != nil {
		t.Fatal(err)
	}
	for i := range v {
		if math.Float32bits(got[i]) != math.Float32bits(v[i]) {
			t.Fatalf("round trip differs at %d", i)
		}
	}
	if _, err := Decode(b, 4); core.CodeOf(err) != core.CodeIntegrity {
		t.Fatalf("wrong-dim blob: err = %v", err)
	}
}

func TestCheckDim(t *testing.T) {
	nan := float32(math.NaN())
	cases := []struct {
		name string
		v    []float32
		dim  int
		ok   bool
	}{
		{"matching dim", []float32{1, 0}, 2, true},
		{"shorter vector", []float32{1}, 2, false},
		{"longer vector", []float32{1, 0, 0}, 2, false},
		{"non-finite value", []float32{nan, 0}, 2, false},
		{"zero dim", nil, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckDim(c.v, c.dim)
			if (err == nil) != c.ok {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestNormalized(t *testing.T) {
	v := Normalized([]float32{3, 4})
	if math.Abs(Norm(v)-1) > 1e-6 {
		t.Fatalf("norm %v", Norm(v))
	}
	unit := []float32{0.6, 0.8}
	if &Normalized(unit)[0] != &unit[0] {
		t.Fatal("unit vector should be returned unchanged")
	}
	if Norm(Normalized([]float32{0, 0})) != 0 {
		t.Fatal("zero vector")
	}
}
