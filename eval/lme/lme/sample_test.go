package lme

import (
	"fmt"
	"os"
	"reflect"
	"testing"
)

// population with the real LongMemEval S counts (DATASET.md).
func lmePopulation() map[string]string {
	counts := map[string][2]int{ // type -> {total, abstention}
		"knowledge-update": {78, 6}, "multi-session": {133, 12}, "single-session-assistant": {56, 0},
		"single-session-preference": {30, 0}, "single-session-user": {70, 6}, "temporal-reasoning": {133, 6},
	}
	pop := map[string]string{}
	for t, c := range counts {
		for i := 0; i < c[0]; i++ {
			id := fmt.Sprintf("%s-%03d", t, i)
			if i < c[1] {
				id += "_abs"
			}
			pop[id] = t
		}
	}
	return pop
}

func TestDrawDeterministicAndStratified(t *testing.T) {
	pop := lmePopulation()
	a, err := Draw(pop, 60, 6, 42)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		b, err := Draw(lmePopulation(), 60, 6, 42) // fresh maps: map order must not matter
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a.Questions, b.Questions) {
			t.Fatal("same seed, different sample")
		}
	}
	c, _ := Draw(pop, 60, 6, 43)
	if reflect.DeepEqual(a.Questions, c.Questions) {
		t.Fatal("seed ignored")
	}
	wantQuota := map[string]int{"knowledge-update": 9, "multi-session": 16, "single-session-assistant": 7,
		"single-session-preference": 4, "single-session-user": 8, "temporal-reasoning": 16}
	if !reflect.DeepEqual(a.QuotaType, wantQuota) {
		t.Fatalf("quota %v", a.QuotaType)
	}
	byType, abs := map[string]int{}, 0
	seen := map[string]bool{}
	for _, q := range a.Questions {
		if seen[q.QuestionID] {
			t.Fatalf("duplicate %s", q.QuestionID)
		}
		seen[q.QuestionID] = true
		if pop[q.QuestionID] != q.QuestionType || IsAbstention(q.QuestionID) != q.Abstention {
			t.Fatalf("bad entry %+v", q)
		}
		byType[q.QuestionType]++
		if q.Abstention {
			abs++
		}
	}
	if !reflect.DeepEqual(byType, wantQuota) || abs != 6 || len(a.Questions) != 60 {
		t.Fatalf("by type %v, abstention %d", byType, abs)
	}
	if !reflect.DeepEqual(a.QuotaAbs, map[string]int{"knowledge-update": 1, "multi-session": 3, "single-session-user": 1, "temporal-reasoning": 1}) {
		t.Fatalf("abstention quota %v", a.QuotaAbs)
	}
}

func TestDrawErrors(t *testing.T) {
	pop := map[string]string{"a": "t", "b_abs": "t"}
	if _, err := Draw(pop, 3, 0, 1); err == nil {
		t.Fatal("n > population accepted")
	}
	if _, err := Draw(pop, 2, 2, 1); err == nil {
		t.Fatal("more abstention than exist accepted")
	}
}

func TestLargestRemainder(t *testing.T) {
	got := largestRemainder(10, map[string]int{"a": 1, "b": 1, "c": 1}, nil)
	if !reflect.DeepEqual(got, map[string]int{"a": 4, "b": 3, "c": 3}) {
		t.Fatal(got)
	}
	got = largestRemainder(5, map[string]int{"a": 10, "b": 1}, map[string]int{"a": 2})
	if !reflect.DeepEqual(got, map[string]int{"a": 2, "b": 3}) {
		t.Fatal(got) // a is capped, so the remaining seats go to b
	}
	got = largestRemainder(4, map[string]int{"a": 1, "b": 1}, map[string]int{"a": 1, "b": 1})
	if !reflect.DeepEqual(got, map[string]int{"a": 1, "b": 1}) {
		t.Fatal(got) // no seat beyond every cap
	}
}

// The committed eval/sample.json must be what the sampler draws from the
// pinned dataset (skipped when the dataset is not downloaded).
func TestCommittedSampleReproduces(t *testing.T) {
	const data = "../data/" + DatasetFile
	if _, err := os.Stat(data); err != nil {
		t.Skip("dataset not downloaded (lme fetch)")
	}
	committed, err := ReadSample("../../sample.json")
	if err != nil {
		t.Fatal(err)
	}
	pop := map[string]string{}
	if err := Each(data, func(q *Question) error { pop[q.QuestionID] = q.QuestionType; return nil }); err != nil {
		t.Fatal(err)
	}
	s, err := Draw(pop, committed.N, committed.MinAbstention, committed.Seed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Questions, committed.Questions) || s.Algorithm != committed.Algorithm || committed.Revision != DatasetRevision {
		t.Fatal("eval/sample.json differs from the sampler's draw")
	}
}
