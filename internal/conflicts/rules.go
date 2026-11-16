package conflicts

import (
	"time"

	"github.com/felipemarts/chanda/internal/core"
)

// Verdicts the model may give (the fixed schema of the verdict prompt).
const (
	VerdictSupersedes = "supersedes"
	VerdictCoexist    = "coexist"
	VerdictUnrelated  = "unrelated"
	VerdictUncertain  = "uncertain"
)

// Verdicts lists the model's verdicts.
func Verdicts() []string {
	return []string{VerdictSupersedes, VerdictCoexist, VerdictUnrelated, VerdictUncertain}
}

// Labels of the two records in the prompt: A is the older record (by
// effective time), B the newer.
const (
	LabelA = "A"
	LabelB = "B"
)

// Reasons stored with a judged pair. A verdict other than supersedes keeps
// its own name as the reason.
const (
	ReasonApplied           = "applied"
	ReasonNotLater          = "uncertain:not_later"          // the record named current is not the later one
	ReasonLowConfidence     = "uncertain:low_confidence"     // confidence below sleep.conflicts.min_confidence
	ReasonLowerZone         = "rejected:lower_zone"          // the current record's zone is lower than the outdated one's
	ReasonAssistantOverUser = "rejected:assistant_over_user" // an assistant turn would supersede a user turn
)

// Answer is the model's verdict on a pair.
type Answer struct {
	Verdict    string  `json:"verdict"`
	Current    string  `json:"current"`
	Confidence float64 `json:"confidence"`
}

// Side is what the rules read of one record of a pair.
type Side struct {
	ID        string
	Effective time.Time
	Zone      core.Zone
	Role      string // the session turn's role; "" for other records
}

// Decision is the outcome of the rules: the verdict stored on the pair
// (supersedes becomes uncertain under the later-date and confidence
// rules), the reason, and, when Apply, the supersession old -> new.
type Decision struct {
	Verdict string
	Reason  string
	Apply   bool
	OldID   string
	NewID   string
}

// Decide applies the rules made without an LLM (decision 0148) to the
// model's answer on the pair (a = older, b = newer). A supersedes verdict
// takes effect only if, in this order:
//
//   - the record named current is the later one in effective time
//     (otherwise uncertain, uncertain:not_later);
//   - the current record's zone is not lower than the outdated record's
//     (user > internal > external; rejected:lower_zone);
//   - an assistant turn does not supersede a user turn
//     (rejected:assistant_over_user);
//   - confidence >= minConfidence (otherwise uncertain,
//     uncertain:low_confidence).
func Decide(ans Answer, a, b Side, minConfidence float64) Decision {
	if ans.Verdict != VerdictSupersedes {
		return Decision{Verdict: ans.Verdict, Reason: ans.Verdict}
	}
	cur, old := b, a
	if ans.Current == LabelA {
		cur, old = a, b
	}
	switch {
	case !cur.Effective.After(old.Effective):
		return Decision{Verdict: VerdictUncertain, Reason: ReasonNotLater}
	case cur.Zone.Rank() < old.Zone.Rank():
		return Decision{Verdict: VerdictSupersedes, Reason: ReasonLowerZone}
	case cur.Role == "assistant" && old.Role == "user":
		return Decision{Verdict: VerdictSupersedes, Reason: ReasonAssistantOverUser}
	case !(ans.Confidence >= minConfidence):
		return Decision{Verdict: VerdictUncertain, Reason: ReasonLowConfidence}
	}
	return Decision{Verdict: VerdictSupersedes, Reason: ReasonApplied, Apply: true, OldID: old.ID, NewID: cur.ID}
}
