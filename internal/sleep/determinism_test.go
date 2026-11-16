package sleep

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/env"
)

// Scenario runs a full sleep (both phases, a passing and a reverted run) on
// fixture f. Integration should append it to internal/determinism.Scenario.
func determinismScenario(f *fixture) {
	f.event("evt_1", "internal", "Ana became CTO of Acme.")
	for i := 2; i <= 4; i++ {
		f.event(fmt.Sprintf("evt_%d", i), "external", fmt.Sprintf("Deploy failed because migrations were pending, run %d.", i))
	}
	f.entity("ent_a", "Bob Stone", "person")
	f.entity("ent_b", "Bob Stone.", "person")
	harmful := false
	fake := script{
		extract: func(ids []string) string {
			p := "job_title"
			if harmful {
				p = "harmful"
			}
			return `{"facts":[` + factJSON([]string{ids[0]}, "Ana", p, "CTO", "") + `]}`
		},
		judge: func(ids []string) string {
			return fmt.Sprintf(`{"verdicts":[{"id":%q,"verdict":"supersede"}]}`, ids[0])
		},
		lesson: func(ids []string, _ string) string {
			return `{"skip":false,"page_id":"new","title":"Deploys","item":"Run pending migrations before deploying."}`
		},
	}.fake()
	s, _ := f.sleeper(fake)
	f.run(s, RunInput{Phases: []int{1}})
	f.run(s, RunInput{})
	f.event("evt_5", "internal", "Bo joined Initech.")
	harmful = true
	f.run(s, RunInput{})
}

func TestSleepIsByteDeterministic(t *testing.T) {
	run := func() []byte {
		path := filepath.Join(t.TempDir(), "chanda.db")
		e, clk := env.NewTestAt(t, path, config.Default())
		e.Cfg.Namespace = ns
		f := &fixture{t: t, e: e, clk: clk, ctx: t.Context()}
		for _, z := range []string{"user", "internal", "external"} {
			f.exec(`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES(?,?,?,?,?,?)`,
				"src_"+z, ns, map[string]string{"user": "user_statement", "internal": "session", "external": "web"}[z], "u-"+z, z, f.now())
		}
		determinismScenario(f)
		if err := e.Store.Checkpoint(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := e.Store.Close(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a, b := run(), run()
	if !bytes.Equal(a, b) {
		t.Fatalf("databases differ: %x vs %x", sha256.Sum256(a), sha256.Sum256(b))
	}
}
