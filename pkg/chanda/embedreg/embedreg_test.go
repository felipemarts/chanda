package embedreg

import (
	"context"
	"testing"
)

type fake struct{}

func (fake) ModelID() string { return "fake" }
func (fake) Dim() int        { return 1 }
func (fake) Embed(context.Context, []string) ([][]float32, error) {
	return nil, nil
}

func TestRegisterLookup(t *testing.T) {
	Register("embedreg-test", func(Config) (Embedder, error) { return fake{}, nil })
	f, ok := Lookup("embedreg-test")
	if !ok {
		t.Fatal("registered provider not found")
	}
	if e, err := f(Config{}); err != nil || e.ModelID() != "fake" {
		t.Fatalf("factory = %v, %v", e, err)
	}
	if _, ok := Lookup("missing"); ok {
		t.Fatal("unregistered provider found")
	}
}

func TestRegisterRejectsInvalid(t *testing.T) {
	ok := func(Config) (Embedder, error) { return fake{}, nil }
	Register("embedreg-dup", ok)
	cases := []struct {
		name     string
		provider string
		f        Factory
	}{
		{"empty name", "", ok},
		{"nil factory", "embedreg-nil", nil},
		{"duplicate name", "embedreg-dup", ok},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Register did not panic")
				}
			}()
			Register(c.provider, c.f)
		})
	}
}
