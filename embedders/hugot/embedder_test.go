package hugot

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
	"golang.org/x/text/unicode/norm"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/embed"
)

var embedderModels = []string{"e5-small", "paraphrase-minilm-l12"}

// The patched tokenizer (third_party/go-huggingface-patched) must produce
// the same ids as HF tokenizers on Portuguese text; stock v0.4.32 differed on
// 10 of these 14 sentences.
func TestTokenizerMatchesHFTokenizers(t *testing.T) {
	for _, name := range embedderModels {
		t.Run(name, func(t *testing.T) {
			dir := modelDir(t, name)
			var g golden
			readGolden(t, name, &g)
			tk, err := hftokenizer.NewFromFile(nil, filepath.Join(dir, "tokenizer.json"))
			if err != nil {
				t.Fatal(err)
			}
			for i, text := range g.Texts {
				got := tk.Encode(norm.NFKC.String(g.Prefix + text))
				if !equalInts(got, g.IDs[i]) {
					t.Errorf("%q:\n got %v\nwant %v", text, got, g.IDs[i])
				}
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Vectors match ONNX Runtime (Python, one thread) on the same ONNX file:
// measured max abs diff 1.2e-7 on the stock build.
func TestEmbedMatchesONNXRuntime(t *testing.T) {
	for _, name := range embedderModels {
		t.Run(name, func(t *testing.T) {
			e := sharedEmbedder(t, name)
			var g golden
			readGolden(t, name, &g)
			vecs, err := e.Embed(context.Background(), g.Texts)
			if err != nil {
				t.Fatal(err)
			}
			var maxDiff float64
			for i, v := range vecs {
				if len(v) != e.Dim() {
					t.Fatalf("dim %d", len(v))
				}
				var n float64
				for j, x := range v {
					n += float64(x) * float64(x)
					maxDiff = math.Max(maxDiff, math.Abs(float64(x)-g.Vectors[i][j]))
				}
				if math.Abs(n-1) > 1e-5 {
					t.Errorf("%q: norm² %v, want 1", g.Texts[i], n)
				}
			}
			t.Logf("%s: max abs diff to ONNX Runtime %.2g", e.ModelID(), maxDiff)
			if maxDiff > 1e-5 {
				t.Fatalf("max abs diff %.3g to ONNX Runtime, want <= 1e-5", maxDiff)
			}
		})
	}
}

// Determinism: the same text gives bit-identical vectors across two loads of
// the model, across repeated calls, and whatever else is in the same call.
func TestEmbedIsBitIdenticalAcrossLoadsAndCalls(t *testing.T) {
	texts := []string{
		"Maria mudou de emprego e agora trabalha como engenheira de dados em Lisboa.",
		"Ok.",
		strings.Repeat("O contrato foi renovado por mais doze meses com reajuste anual. ", 20),
		"Ação, coração, informação, órgão, pãozinho, açaí, você, três, avô.",
	}
	for _, name := range embedderModels {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := modelDir(t, name)
			load := func() [][]float32 {
				e, err := Open(ctx, dir)
				if err != nil {
					t.Fatal(err)
				}
				defer e.Close()
				v, err := e.Embed(ctx, texts)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			a, b := load(), load()
			shared := sharedEmbedder(t, name)
			for i := range texts {
				if !bitsEqual(a[i], b[i]) {
					t.Errorf("text %d differs across loads", i)
				}
				alone, err := shared.Embed(ctx, texts[i:i+1])
				if err != nil {
					t.Fatal(err)
				}
				if !bitsEqual(a[i], alone[0]) {
					t.Errorf("text %d differs alone vs in a list of %d", i, len(texts))
				}
			}
		})
	}
}

// Sanity check in Portuguese: paraphrases are closer than unrelated
// sentences, and each question finds its statement. Scores are not
// calibrated (e5 compresses cosines into 0.8..1), only the order is checked.
func TestPortugueseSimilarity(t *testing.T) {
	statements := []string{
		"O plano premium custa R$ 49,90 por mês.",
		"A reunião com o cliente foi remarcada para sexta-feira.",
		"Maria trabalha como engenheira de dados em Lisboa.",
		"O servidor de produção ficou fora do ar por duas horas ontem.",
		"Meu gato dorme o dia inteiro no sofá da sala.",
	}
	questions := []string{
		"Quanto custa a assinatura premium?",
		"Quando vai ser o encontro com o cliente?",
		"Onde a Maria trabalha?",
		"Houve alguma queda no sistema?",
		"O que o meu gato faz durante o dia?",
	}
	for _, name := range embedderModels {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			e := sharedEmbedder(t, name)
			s, err := e.Embed(ctx, statements)
			if err != nil {
				t.Fatal(err)
			}
			q, err := e.EmbedQuery(ctx, questions)
			if err != nil {
				t.Fatal(err)
			}
			for i := range q {
				best, bestScore := -1, -2.0
				for j := range s {
					if c := cosine(q[i], s[j]); c > bestScore {
						best, bestScore = j, c
					}
				}
				if best != i {
					t.Errorf("%q matched %q (%.3f), want %q", questions[i], statements[best], bestScore, statements[i])
				}
			}
			p, err := e.Embed(ctx, []string{"O gato dorme no sofá da sala.", "Um felino descansa no sofá.", "A taxa de juros subiu no trimestre."})
			if err != nil {
				t.Fatal(err)
			}
			para, unrelated := cosine(p[0], p[1]), cosine(p[0], p[2])
			t.Logf("%s: paraphrase %.3f, unrelated %.3f", e.ModelID(), para, unrelated)
			if para <= unrelated {
				t.Errorf("paraphrase %.3f <= unrelated %.3f", para, unrelated)
			}
		})
	}
}

// e5 is asymmetric: the query and passage prefixes give different vectors;
// MiniLM has no prefixes, so EmbedQuery equals Embed.
func TestRolePrefixes(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name string
		same bool
	}{{"e5-small", false}, {"paraphrase-minilm-l12", true}} {
		t.Run(c.name, func(t *testing.T) {
			e := sharedEmbedder(t, c.name)
			p, _ := e.Embed(ctx, []string{"preço do plano"})
			q, err := e.EmbedQuery(ctx, []string{"preço do plano"})
			if err != nil {
				t.Fatal(err)
			}
			if bitsEqual(p[0], q[0]) != c.same {
				t.Fatalf("query == passage is %v, want %v", !c.same, c.same)
			}
		})
	}
}

// Inputs longer than the model window are cut at the end, not rejected (two
// texts that differ only past the window embed identically); NFKC makes
// compatibility forms (ligatures, full-width) embed like their plain form.
func TestEmbedLongTextAndNFKC(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("A equipe revisou o relatório trimestral e aprovou o orçamento. ", 400)
	for _, name := range embedderModels {
		t.Run(name, func(t *testing.T) {
			e := sharedEmbedder(t, name)
			v, err := e.Embed(ctx, []string{long, long + " Outro final totalmente diferente.", "", "ﬁnanças ＡＢＣ", "finanças ABC"})
			if err != nil {
				t.Fatal(err)
			}
			if !bitsEqual(v[0], v[1]) {
				t.Error("text past the window changed the vector")
			}
			if !bitsEqual(v[3], v[4]) {
				t.Error("NFKC: compatibility form embeds differently from its plain form")
			}
			for i, x := range v[:3] {
				var n float64
				for _, f := range x {
					n += float64(f) * float64(f)
				}
				if math.Abs(n-1) > 1e-5 {
					t.Errorf("vector %d: norm² %v", i, n)
				}
			}
		})
	}
}

func TestClipKeepsTheWindow(t *testing.T) {
	dir := modelDir(t, "e5-small")
	c, err := newClipper(dir, 16)
	if err != nil {
		t.Fatal(err)
	}
	short := "passage: O gato dorme."
	if got := c.clip(short); got != short {
		t.Fatalf("short text changed: %q", got)
	}
	long := "passage: " + strings.Repeat("Configurar o pipeline de integração contínua. ", 10)
	got := c.clip(long)
	if n := len(c.tok.EncodeWithAnnotations(got).IDs); n > 14 || n < 10 || !strings.HasPrefix(long, got) {
		t.Fatalf("clip(long) = %q (%d tokens), want a prefix of at most 14 tokens", got, n)
	}
}

// Wired through the main module: embed.provider hugot builds this embedder
// via pkg/chanda/embedreg, and the result is a core.RoleEmbedder.
func TestProviderThroughEmbedNewFromConfig(t *testing.T) {
	dir := modelDir(t, "paraphrase-minilm-l12")
	registerOnce()
	m, _ := LookupModel("paraphrase-minilm-l12")
	got, err := embed.NewFromConfig(config.EmbedConfig{Provider: "hugot", ModelPath: dir, ModelID: m.ModelID(), Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	defer got.(*Embedder).Close()
	if got.ModelID() != m.ModelID() || got.Dim() != 384 {
		t.Fatalf("got %s dim %d", got.ModelID(), got.Dim())
	}
	if _, ok := got.(core.RoleEmbedder); !ok {
		t.Fatal("not a core.RoleEmbedder")
	}
	if _, err := embed.NewFromConfig(config.EmbedConfig{Provider: "hugot", ModelPath: dir, ModelID: "hash-v1-256", Dim: 384}); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("mismatched model id: err = %v", err)
	}
}
