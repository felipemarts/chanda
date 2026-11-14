package eval

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  Ação  É\tdo\n\nCoração ": "acao e do coracao",
		"R$ 1.234,00":               "r$ 1.234,00",
		"Café":                     "cafe", // NFD combining accent
		"":                          "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCountTokens(t *testing.T) {
	cases := map[string]int{"": 0, "a": 1, "abcd": 1, "abcde": 2, "ação": 1, "12345678": 2}
	for in, want := range cases {
		if got := CountTokens(in); got != want {
			t.Errorf("CountTokens(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestContentTermsDropStopwords(t *testing.T) {
	got := strings.Join(contentTerms("Qual é o preço do Plano Essencial?"), " ")
	if got != "preco plano essencial" {
		t.Fatalf("got %q", got)
	}
}

func validDataset() *Dataset {
	return &Dataset{
		Manifest: Manifest{Name: "tiny"},
		Corpus: []Item{
			{Type: ItemDocument, ID: "d1", URI: "file:///d1.md", Version: 1, Date: "2026-01-01", Source: "document", Text: "# Preços\n\nO plano A custa R$ 10."},
			{Type: ItemSession, ID: "s1", URI: "session://s1", Date: "2026-02-01", Source: "session", Turns: []Turn{{Role: "user", Text: "O ramal do Rui é 1234."}}},
			{Type: ItemDocument, ID: "d1", URI: "file:///d1.md", Version: 2, Date: "2026-03-01", Source: "document", Text: "# Preços\n\nO plano A custa R$ 12."},
		},
		Questions: []Question{
			{ID: "q1", Question: "Quanto custa o plano A?", Gold: []Gold{{DocID: "d1", Span: "custa R$ 12"}}, Answers: []string{"R$ 12"}, MustNotContain: []string{"R$ 10"}, Tags: []string{"update"}},
			{ID: "q2", Question: "Quanto custava o plano A?", AsOf: "2026-01-15", Gold: []Gold{{DocID: "d1", Span: "custa R$ 10"}}, Answers: []string{"R$ 10"}, Tags: []string{"temporal"}},
			{ID: "q3", Question: "Qual é o ramal do Rui?", Gold: []Gold{{DocID: "s1"}}, Answers: []string{"1234"}, Tags: []string{"session"}},
			{ID: "q4", Question: "Quem é o CEO?", Tags: []string{"abstention"}},
		},
	}
}

// BASE.md "Evaluation": the dataset is the fixed input of a reproducible report,
// so the loader rejects anything that would make a gold label unreachable.
func TestDatasetValidation(t *testing.T) {
	if err := validDataset().Validate(); err != nil {
		t.Fatalf("valid dataset rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Dataset)
		want string
	}{
		{"unknown type", func(d *Dataset) { d.Corpus[0].Type = "blob" }, "unknown type"},
		{"unknown source", func(d *Dataset) { d.Corpus[0].Source = "email" }, "unknown source"},
		{"version gap", func(d *Dataset) { d.Corpus[2].Version = 3 }, "version 3, want 2"},
		{"dates out of order", func(d *Dataset) { d.Corpus[1].Date = "2025-12-01" }, "before previous item"},
		{"uri reused", func(d *Dataset) { d.Corpus[1].URI = "file:///d1.md" }, "already used"},
		{"session with text", func(d *Dataset) { d.Corpus[1].Text = "x" }, "a session has turns"},
		{"duplicate question", func(d *Dataset) { d.Questions[1].ID = "q1" }, "duplicate id"},
		{"gold doc missing", func(d *Dataset) { d.Questions[0].Gold[0].DocID = "nope" }, "not in corpus"},
		{"gold span missing", func(d *Dataset) { d.Questions[0].Gold[0].Span = "custa R$ 99" }, "gold span"},
		{"gold after as_of", func(d *Dataset) { d.Questions[0].AsOf = "2026-02-15" }, "gold span"},
		{"answer not in gold", func(d *Dataset) { d.Questions[0].Answers = []string{"R$ 77"} }, "answer"},
		{"abstention with gold", func(d *Dataset) { d.Questions[0].Tags = []string{"abstention"} }, "abstention"},
		{"no gold without abstention tag", func(d *Dataset) { d.Questions[3].Tags = nil }, "abstention"},
		{"bad tag", func(d *Dataset) { d.Questions[0].Tags = []string{"Bad Tag"} }, "bad tag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := validDataset()
			c.mut(d)
			err := d.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want error containing %q", err, c.want)
			}
		})
	}
}

func TestDatasetRoundTripAndHash(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tiny")
	d := validDataset()
	if err := WriteDataset(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDataset(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash() != d.Hash() || len(got.Corpus) != 3 || len(got.Questions) != 4 {
		t.Fatalf("round trip changed the dataset: %s vs %s", got.Hash(), d.Hash())
	}
	d.Questions[0].Answers = []string{"R$ 12,"}
	if got.Hash() == d.Hash() {
		t.Fatal("hash ignores question content")
	}
}

func TestReadJSONLRejectsUnknownFields(t *testing.T) {
	_, err := ReadJSONL[Question](strings.NewReader("{\"id\":\"q\",\"question\":\"x\"}\n\n{\"id\":\"q2\",\"oops\":1}\n"))
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("got %v", err)
	}
}
