package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

type ranked struct {
	ids    []string
	scores []float64
}

func searchOrder(t *testing.T, a *App, q string) ranked {
	t.Helper()
	s := must(a.Search(agent, chanda.SearchInput{Query: q, Limit: 20}))
	var r ranked
	for _, it := range s.Items {
		r.ids, r.scores = append(r.ids, it.ID), append(r.scores, it.Score)
	}
	return r
}

// BASE.md phase 8 acceptance: "turning learning off returns exactly the
// order of C4". A model is trained from outcomes and promoted (λ > 0, the
// read path then weights the promoted model's term, decision 0070); with
// the runtime toggle off, every query returns the pre-learning order and
// scores bit for bit, the same as an App wired without learning (C4).
func TestLearningOffReturnsTheOrderOfC4(t *testing.T) {
	cfg := config.Default()
	cfg.Namespace, cfg.Ranking.MinTrainExamples, cfg.Ranking.LambdaStep = "ns", 4, 0.3
	e, _ := env.NewTestAt(t, filepath.Join(t.TempDir(), "chanda.db"), cfg)
	a, err := NewWith(e, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		must(a.Remember(agent, chanda.RememberInput{Content: fmt.Sprintf("Release note %d: the deploy pipeline changed step %d of the build.", i, i*7)}))
	}
	must(a.Flush(cli, chanda.FlushInput{}))
	queries := []string{"deploy pipeline build", "release note step", "pipeline changed"}
	before := map[string]ranked{}
	for _, q := range queries {
		before[q] = searchOrder(t, a, q)
	}

	// Outcomes that favour the last-ranked items, then train.
	for i := 0; i < 10; i++ {
		s := must(a.Search(agent, chanda.SearchInput{Query: queries[i%len(queries)], Limit: 6}))
		last := s.Items[len(s.Items)-1].ID
		must(a.Outcome(agent, chanda.OutcomeInput{RetrievalID: s.RetrievalID, Value: 1, UsedIDs: []string{last}}))
		s = must(a.Search(agent, chanda.SearchInput{Query: queries[(i+1)%len(queries)], Limit: 6}))
		must(a.Outcome(agent, chanda.OutcomeInput{RetrievalID: s.RetrievalID, Value: -1, UsedIDs: []string{s.Items[0].ID}}))
	}
	tr := must(a.ranker.Train(bg, "ns", "", "test"))
	// Promotion with an evaluator that passes (the gate itself is tested in
	// internal/rank and through sleep): λ leaves 0 and the term counts.
	pr := must(a.ranker.Promote(bg, "ns", "test", func(useLearned bool, _ float64) (float64, error) {
		if useLearned {
			return 90, nil
		}
		return 50, nil
	}))
	if !pr.Promoted || pr.LambdaTo <= 0 || pr.ModelID != tr.ModelID {
		t.Fatalf("promotion %+v", pr)
	}
	changed := false
	for _, q := range queries {
		got := searchOrder(t, a, q)
		changed = changed || !slices.Equal(got.scores, before[q].scores)
	}
	if !changed {
		t.Fatal("a promoted model with λ > 0 changed no score")
	}
	if p := str(t, a, `SELECT params FROM retrievals ORDER BY ts DESC, id DESC LIMIT 1`); !strings.Contains(p, `"lambda":0.3`) {
		t.Fatalf("λ not applied: %s", p)
	}

	must(a.RankingToggle(cli, "", false))
	c4, err := NewWith(e, Options{DisableLearning: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range queries {
		off, base := searchOrder(t, a, q), searchOrder(t, c4, q)
		if !slices.Equal(off.ids, before[q].ids) || !slices.Equal(off.scores, before[q].scores) {
			t.Errorf("%q with learning off: %v %v, before learning %v %v", q, off.ids, off.scores, before[q].ids, before[q].scores)
		}
		if !slices.Equal(base.ids, off.ids) || !slices.Equal(base.scores, off.scores) {
			t.Errorf("%q: C4 %v, learning off %v", q, base.ids, off.ids)
		}
	}
	if _, err := a.RankingToggle(agent, "", true); err == nil {
		t.Fatal("an agent toggled learning")
	}
	verifyOK(t, a)
}
