package embed

import (
	"bytes"
	"context"
	"testing"
)

func embedPair(t *testing.T, h *HashEmbedder, a, b string) float32 {
	t.Helper()
	vs, err := h.Embed(context.Background(), []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	return Dot(vs[0], vs[1])
}

// BASE.md "Embeddings": the built-in baseline must behave like a similarity
// measure — identical text ≈1, shared words/morphology high, unrelated low.
func TestHashEmbedderSimilarity(t *testing.T) {
	h, err := NewHash(256)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		a, b     string
		min, max float32
	}{
		{"identical text", "O preço do produto subiu em março.", "O preço do produto subiu em março.", 0.9999, 1.0001},
		{"case and accents folded", "Preço de São Paulo", "preco de sao paulo", 0.9999, 1.0001},
		{"punctuation and spacing ignored", "preço: 10 reais!", "  preço 10   reais ", 0.9999, 1.0001},
		{"singular vs plural (preço/preços)", "preço", "preços", 0.6, 1},
		{"paraphrase sharing morphology", "o preço do produto subiu", "os preços dos produtos subiram", 0.5, 1},
		{"paraphrase sharing words", "Maria é gerente de vendas em São Paulo", "a gerente de vendas de São Paulo é Maria", 0.75, 1},
		{"unrelated text", "o gato dormiu no sofá da sala", "relatório financeiro trimestral consolidado", -0.2, 0.2},
		{"unrelated text, other domain", "deploy do kubernetes falhou no cluster", "receita de bolo de cenoura com chocolate", -0.2, 0.2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := embedPair(t, h, c.a, c.b)
			if s < c.min || s > c.max {
				t.Fatalf("cosine(%q, %q) = %.4f, want in [%.2f, %.2f]", c.a, c.b, s, c.min, c.max)
			}
		})
	}
}

func TestHashEmbedderOrderingParaphraseAboveUnrelated(t *testing.T) {
	h, _ := NewHash(256)
	q := "qual é o preço atual do plano empresarial"
	near := embedPair(t, h, q, "preços do plano empresarial atualizados")
	far := embedPair(t, h, q, "o time de futebol venceu o campeonato")
	if near <= far+0.2 {
		t.Fatalf("paraphrase %.3f not clearly above unrelated %.3f", near, far)
	}
}

// Principle 11 (same input, same output): bit-identical vectors across runs
// and across embedder instances, and batch == one-by-one.
func TestHashEmbedderDeterministic(t *testing.T) {
	texts := []string{"O preço do produto subiu.", "", "!!!", "ação e reação", "日本語のテキスト", "x"}
	for _, dim := range []int{64, 256, 384} {
		h1, _ := NewHash(dim)
		h2, _ := NewHash(dim)
		a, err := h1.Embed(context.Background(), texts)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := h2.Embed(context.Background(), texts)
		for i := range texts {
			one, _ := h1.Embed(context.Background(), texts[i:i+1])
			if !bytes.Equal(Encode(a[i]), Encode(b[i])) || !bytes.Equal(Encode(a[i]), Encode(one[0])) {
				t.Fatalf("dim %d text %q: vectors differ between runs", dim, texts[i])
			}
			if len(a[i]) != dim {
				t.Fatalf("dim %d: got len %d", dim, len(a[i]))
			}
		}
	}
}

// Golden check: the algorithm is frozen under ModelID hash-v1-<dim>. If this
// fails, the feature extraction changed and HashVersion must be bumped.
func TestHashEmbedderGolden(t *testing.T) {
	h, _ := NewHash(16)
	v, _ := h.Embed(context.Background(), []string{"Preço do produto"})
	got := Encode(v[0])
	sum := fnv1a(string(got))
	const want = uint64(4740783060637721177)
	if sum != want {
		t.Fatalf("golden hash changed: %d (bump HashVersion if intended)", sum)
	}
}

func TestHashEmbedderNormalized(t *testing.T) {
	h, _ := NewHash(256)
	vs, _ := h.Embed(context.Background(), []string{"texto qualquer com algumas palavras", "", "---"})
	if n := Norm(vs[0]); n < 0.9999 || n > 1.0001 {
		t.Fatalf("norm = %v", n)
	}
	for _, v := range vs[1:] {
		if Norm(v) != 0 {
			t.Fatalf("text without tokens must give the zero vector")
		}
	}
}

func TestHashModelID(t *testing.T) {
	h, _ := NewHash(384)
	if h.ModelID() != "hash-v1-384" || h.Dim() != 384 {
		t.Fatalf("got %s/%d", h.ModelID(), h.Dim())
	}
	if _, err := NewHash(0); err == nil {
		t.Fatal("dim 0 accepted")
	}
}

func TestTokensUnicode61Like(t *testing.T) {
	got := Tokens("Ação-Reação: R$ 10,50 (São Paulo)!")
	want := []string{"acao", "reacao", "r", "10", "50", "sao", "paulo"}
	if len(got) != len(want) {
		t.Fatalf("got %q want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}
