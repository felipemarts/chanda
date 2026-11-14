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
	for _, tag := range []string{TagAbstention, TagTemporal, TagUpdate, TagInjection, TagDedup, TagSession, TagParaphrase} {
		if tags[tag] < 5 {
			t.Errorf("tag %q has %d questions, want >= 5", tag, tags[tag])
		}
	}
	if n := len(ds.Training()); n < 50 {
		t.Errorf("%d training questions, want >= 50", n)
	}
	if n := len(ds.Gate()); n < 20 {
		t.Errorf("%d gate questions, want >= 20", n)
	}
	// Abstention: enough unanswerable questions to measure precision and recall.
	if tags[TagAbstention] < 30 {
		t.Errorf("%d abstention questions, want >= 30", tags[TagAbstention])
	}
	// The raw history is far larger than the 800-token budget.
	tokens := 0
	for _, it := range ds.Corpus {
		tokens += CountTokens(ItemText(it))
	}
	if tokens < 20*800 {
		t.Errorf("corpus has %d tokens, want at least 20 budgets", tokens)
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
	// Temporal chains: subjects whose single-valued attribute takes three or
	// more values across document versions and session statements.
	chains := 0
	for _, q := range ds.Questions {
		if q.HasTag(TagUpdate) && len(q.MustNotContain) >= 2 && !q.HasTag(TagInjection) {
			chains++
		}
	}
	if chains < 8 {
		t.Errorf("%d current-value questions with two or more superseded values, want >= 8", chains)
	}
	// Statements asserted in sessions and tool turns are present.
	facts, tools := 0, 0
	for _, it := range ds.Corpus {
		facts += len(it.Facts)
		for _, tu := range it.Turns {
			if tu.Role == "tool" {
				tools++
			}
		}
	}
	if facts < 10 || tools < 2 {
		t.Errorf("%d asserted statements and %d tool turns", facts, tools)
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
	if total > 3<<20 {
		t.Errorf("committed set is %d bytes, keep it under 3 MiB", total)
	}
}
