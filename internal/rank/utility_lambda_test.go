package rank

import (
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
)

// Decision 0146: a fixed λ applies the stage-1 utility without a model.
func TestUtilityLambdaAppliesStageOneUtility(t *testing.T) {
	r, e, _ := newTest(t, func(c *config.RankingConfig) { c.UtilityLambda, c.MinExposures = 0.2, 2 })
	if got, _ := r.Lambda(bg, "ns"); got != 0.2 {
		t.Fatalf("λ without a promoted model: %v, want 0.2", got)
	}
	for i := 0; i < 2; i++ {
		ret := seedRetrieval(t, e, "ns", cands("evt_a", "evt_b"), []string{"evt_a", "evt_b"})
		mustOutcome(t, r, "ns", ret, 1, "evt_a")
	}
	terms, err := r.Terms(bg, "ns", cands("evt_a", "evt_b", "evt_new"))
	if err != nil {
		t.Fatal(err)
	}
	us, _ := r.Scores(bg, "ns", []string{"evt_a", "evt_b"})
	if terms["evt_a"] != us["evt_a"] || terms["evt_b"] != us["evt_b"] || terms["evt_new"] != Neutral {
		t.Fatalf("terms %v, utility %v", terms, us)
	}
	if !(terms["evt_a"] > Neutral && terms["evt_b"] < Neutral) {
		t.Fatalf("used record must rise, unused one fall: %v", terms)
	}
	// The runtime switch still turns it off.
	if _, err := r.SetEnabled(bg, "ns", false, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Lambda(bg, "ns"); got != 0 {
		t.Fatalf("λ with learning switched off: %v", got)
	}
}

func TestUtilityLambdaCappedAndValidated(t *testing.T) {
	r, _, _ := newTest(t, func(c *config.RankingConfig) { c.UtilityLambda, c.LambdaMax = 0.25, 0.3 })
	if got, _ := r.Lambda(bg, "ns"); got != 0.25 {
		t.Fatalf("λ %v", got)
	}
	c := config.Default()
	c.Ranking.UtilityLambda = 0.5 // above lambda_max 0.3
	if c.Validate() == nil {
		t.Fatal("utility_lambda above lambda_max must be invalid")
	}
	c.Ranking.UtilityLambda = -0.1
	if c.Validate() == nil {
		t.Fatal("negative utility_lambda must be invalid")
	}
	d, x := config.Default(), config.Default()
	x.Ranking.UtilityLambda = 0.1
	if d.Hash() == x.Hash() {
		t.Fatal("utility_lambda must change the hash")
	}
}
