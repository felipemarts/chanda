package prov_test

import (
	"context"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/prov"
)

// BASE.md "Trust zones": only the promote tool, called by a human actor,
// raises a record's zone. The action is audited.
func TestPromoteOnlyByHuman(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		zone    core.Zone
		human   bool
		code    core.Code // "" means success
		changed bool
	}{
		{"non-human actor is denied", "fct_2", core.ZoneUser, false, core.CodePermissionDenied, false},
		{"human raises a fact", "fct_2", core.ZoneUser, true, "", true},
		{"human raises a page", "pg_1", core.ZoneUser, true, "", true},
		{"never lowers", "pgi_2", core.ZoneExternal, true, core.CodeInvalidArgument, false},
		{"same zone is a no-op", "fct_1", core.ZoneExternal, true, "", false},
		{"events are raw and cannot be promoted", "evt_3", core.ZoneUser, true, core.CodeInvalidArgument, false},
		{"unknown record", "fct_none", core.ZoneUser, true, core.CodeNotFound, false},
		{"unknown zone", "fct_2", core.Zone("root"), true, core.CodeInvalidArgument, false},
		{"other namespace is not found", "fct_2", core.ZoneUser, true, core.CodeNotFound, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _ := graph(t)
			ctx := context.Background()
			ns := "ns"
			if c.name == "other namespace is not found" {
				ns = "other"
			}
			res, err := prov.Promote(ctx, e, ns, c.id, c.zone, "alice", c.human)
			if c.code != "" {
				if core.CodeOf(err) != c.code {
					t.Fatalf("err = %v, want %s", err, c.code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Changed != c.changed {
				t.Fatalf("changed = %v", res.Changed)
			}
			var n int
			e.Store.DB().QueryRow(`SELECT count(*) FROM audit_log WHERE action = ?`, audit.ActPromote).Scan(&n)
			if want := map[bool]int{true: 1, false: 0}[c.changed]; n != want {
				t.Fatalf("promote audit entries = %d, want %d", n, want)
			}
			if z, _ := zoneOf(e, c.id); z != string(c.zone) {
				t.Fatalf("zone = %s", z)
			}
			rep, err := prov.Check(ctx, e.Store.DB(), "ns")
			if err != nil || !rep.OK {
				t.Fatalf("checker must accept a promotion: %+v %v", rep.Violations, err)
			}
			if v, _ := audit.Verify(ctx, e.Store.DB()); !v.OK {
				t.Fatalf("audit chain broken: %+v", v)
			}
		})
	}
}

func zoneOf(e *env.Env, id string) (string, error) {
	var z string
	err := e.Store.DB().QueryRow(`SELECT trust_zone FROM facts WHERE id = ?1 UNION ALL SELECT trust_zone FROM pages WHERE id = ?1
		UNION ALL SELECT trust_zone FROM page_items WHERE id = ?1`, id).Scan(&z)
	return z, err
}
