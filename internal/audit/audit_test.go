package audit_test

import (
	"context"
	"testing"

	"github.com/felipemarts/chanda/internal/audit"
	"github.com/felipemarts/chanda/internal/env"
)

func TestChainVerifies(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := e.Audit.Append(ctx, e.Store.DB(), audit.Entry{Namespace: "ns", Actor: "test", Action: audit.ActWrite, Target: "evt_x", Details: map[string]any{"i": i}}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := audit.Verify(ctx, e.Store.DB())
	if err != nil || !r.OK || r.Entries != 5 {
		t.Fatalf("verify: %+v %v", r, err)
	}
}

func TestActionsSortedAndUnique(t *testing.T) {
	acts := audit.Actions()
	for i := 1; i < len(acts); i++ {
		if acts[i-1] >= acts[i] {
			t.Fatalf("actions not sorted/unique at %q, %q", acts[i-1], acts[i])
		}
	}
}

func TestTamperDetected(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	db := e.Store.DB()
	for i := 0; i < 3; i++ {
		e.Audit.Append(ctx, db, audit.Entry{Namespace: "ns", Actor: "test", Action: audit.ActWrite, Target: "t"})
	}
	// Simulate an attacker bypassing the trigger.
	if _, err := db.Exec(`DROP TRIGGER audit_no_update`); err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE audit_log SET actor='evil' WHERE id=2`)
	r, _ := audit.Verify(ctx, db)
	if r.OK || r.BrokenAt != 2 {
		t.Fatalf("tamper not detected: %+v", r)
	}
}
