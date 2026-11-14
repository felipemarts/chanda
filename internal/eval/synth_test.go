package eval

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const committedSynthetic = "../../eval/data/synthetic-pt"

func TestMoney(t *testing.T) {
	cases := map[int]string{0: "R$ 0,00", 1990: "R$ 19,90", 123456: "R$ 1.234,56", 100000000: "R$ 1.000.000,00"}
	for in, want := range cases {
		if got := money(in); got != want {
			t.Errorf("money(%d) = %q, want %q", in, got, want)
		}
	}
}

// Principle 11 applied to the evaluation set: same seed, same bytes.
func TestSyntheticIsDeterministic(t *testing.T) {
	a, b := GenerateSyntheticPT(SyntheticSeed), GenerateSyntheticPT(SyntheticSeed)
	if a.Hash() != b.Hash() {
		t.Fatal("two generations with the same seed differ")
	}
	if c := GenerateSyntheticPT("other-seed"); c.Hash() == a.Hash() {
		t.Fatal("seed has no effect")
	}
}

func TestSyntheticIsValidAndCoversEveryCase(t *testing.T) {
	ds := GenerateSyntheticPT(SyntheticSeed)
	if err := ds.Validate(); err != nil {
		t.Fatal(err)
	}
	tags := map[string]int{}
	for _, q := range ds.Questions {
		for _, tag := range q.Tags {
			tags[tag]++
		}
	}
	for _, tag := range []string{TagAbstention, TagTemporal, TagUpdate, TagInjection, TagDedup, TagSession} {
		if tags[tag] < 3 {
			t.Errorf("tag %q has %d questions, want >= 3", tag, tags[tag])
		}
	}
	sources, versions := map[string]bool{}, 0
	for _, it := range ds.Corpus {
		sources[it.Source] = true
		versions = max(versions, it.Version)
	}
	for _, s := range []string{"document", "session", "web", "tool"} {
		if !sources[s] {
			t.Errorf("no item from source %q", s)
		}
	}
	if versions < 3 {
		t.Errorf("max document version %d, want >= 3", versions)
	}
}

// The committed set must be exactly what the generator produces, so the
// report's dataset hash can be reproduced from a clean clone.
func TestCommittedSyntheticMatchesGenerator(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDataset(dir, GenerateSyntheticPT(SyntheticSeed)); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, f := range []string{ManifestFile, CorpusFile, QuestionsFile} {
		want, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(committedSynthetic, f))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the generator output; run: go run ./eval/cmd/gensynth", f)
		}
		total += len(got)
	}
	if total > 1<<20 {
		t.Errorf("committed set is %d bytes, keep it under 1 MiB", total)
	}
}
