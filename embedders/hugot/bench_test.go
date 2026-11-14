package hugot

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// go test -run '^$' -bench . -benchtime 10x            (stock)
// GOEXPERIMENT=simd go test -run '^$' -bench . -benchtime 10x
// Results are recorded in decision 0036.

const benchShort = "A reunião com o cliente foi remarcada para sexta-feira às 14h30."

var benchParagraph = strings.TrimSpace(strings.Repeat(
	"O contrato de prestação de serviços foi renovado por mais doze meses, com reajuste anual pelo IPCA e multa de 2% por atraso. ", 6))

func BenchmarkLoad(b *testing.B) {
	for _, name := range embedderModels {
		b.Run(name, func(b *testing.B) {
			dir := modelDir(b, name)
			for b.Loop() {
				e, err := Open(context.Background(), dir)
				if err != nil {
					b.Fatal(err)
				}
				e.Close()
			}
		})
	}
}

func BenchmarkEmbed(b *testing.B) {
	batch := make([]string, 32)
	for i := range batch {
		batch[i] = fmt.Sprintf("%s (nota %d)", benchShort, i)
	}
	for _, name := range embedderModels {
		for _, c := range []struct {
			name  string
			texts []string
		}{
			{"short", []string{benchShort}},
			{"paragraph", []string{benchParagraph}},
			{"32-short", batch},
		} {
			b.Run(name+"/"+c.name, func(b *testing.B) {
				e := sharedEmbedder(b, name)
				ctx := context.Background()
				for b.Loop() {
					if _, err := e.Embed(ctx, c.texts); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// The clipper tokenises every input once more before the model does.
func BenchmarkClip(b *testing.B) {
	c, err := newClipper(modelDir(b, "e5-small"), 512)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		c.clip("passage: " + benchParagraph)
	}
}

func BenchmarkRerank(b *testing.B) {
	docs := make([]string, 30)
	for i := range docs {
		docs[i] = fmt.Sprintf("Documento %d: o plano premium custa R$ %d,90 por mês, com cobrança mensal e suporte.", i, 40+i)
	}
	for _, n := range []int{1, 30} {
		b.Run(fmt.Sprintf("%d-docs", n), func(b *testing.B) {
			r := sharedReranker(b)
			ctx := context.Background()
			for b.Loop() {
				if _, err := r.Score(ctx, "Quanto custa o plano premium?", docs[:n]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
