package chanda

// The conflicts admin tool (decision 0148): control of conflict resolution
// between raw records. Field names are stable: an evaluation harness calls
// the tool over MCP.

// Actions of the conflicts tool.
const (
	ConflictsList   = "list"   // every pair (pending and judged) and every supersession
	ConflictsRevert = "revert" // end a supersession's effect (kept with reverted_at)
	ConflictsReset  = "reset"  // delete the queue and its verdicts; supersessions stay
	ConflictsScan   = "scan"   // offline detection over the namespace; writes the queue only with write: true
	ConflictsStats  = "stats"  // counts only
)

// ConflictsActions lists the conflicts actions.
func ConflictsActions() []string {
	return []string{ConflictsList, ConflictsRevert, ConflictsReset, ConflictsScan, ConflictsStats}
}

// ConflictsInput controls conflict resolution in a namespace.
type ConflictsInput struct {
	Namespace string  `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Action    string  `json:"action" jsonschema:"list, revert, reset, scan or stats"`
	ID        string  `json:"id,omitempty" jsonschema:"revert: the supersession id (sup_...)"`
	Write     bool    `json:"write,omitempty" jsonschema:"scan: also queue the pairs found (default: counts only)"`
	Details   bool    `json:"details,omitempty" jsonschema:"scan: also return the pair lists (candidates, near_dup_refusals)"`
	MinCosine float64 `json:"min_cosine,omitempty" jsonschema:"scan: similarity floor instead of sleep.conflicts.min_cosine, in (0,1)"`
}

// ConflictPair is one row of the conflict queue.
type ConflictPair struct {
	ID           string   `json:"id"`
	OlderID      string   `json:"older_id"`
	NewerID      string   `json:"newer_id"`
	Cosine       float64  `json:"cosine"`
	Differs      string   `json:"differs" jsonschema:"numbers, dates, entities or negations"`
	Shared       []string `json:"shared" jsonschema:"shared entities and content terms"`
	Status       string   `json:"status" jsonschema:"pending or judged"`
	ModelVerdict string   `json:"model_verdict,omitempty" jsonschema:"supersedes, coexist, unrelated or uncertain"`
	ModelCurrent string   `json:"model_current,omitempty" jsonschema:"A (older) or B (newer)"`
	Confidence   *float64 `json:"confidence,omitempty"`
	Verdict      string   `json:"verdict,omitempty" jsonschema:"verdict after the rules"`
	Reason       string   `json:"reason,omitempty" jsonschema:"applied, uncertain:not_later, uncertain:low_confidence, rejected:lower_zone, rejected:assistant_over_user, or the verdict"`
	RunID        string   `json:"run_id,omitempty" jsonschema:"sleep run that judged it"`
	CreatedAt    string   `json:"created_at"`
	JudgedAt     string   `json:"judged_at,omitempty"`
	Supersession string   `json:"supersession_id,omitempty" jsonschema:"supersession written for this pair"`
}

// Supersession is one applied verdict: OldID is outdated by NewID from EndsAt.
type Supersession struct {
	ID         string  `json:"id"`
	OldID      string  `json:"old_id"`
	NewID      string  `json:"new_id"`
	PairID     string  `json:"pair_id,omitempty" jsonschema:"judged pair; absent after reset"`
	RunID      string  `json:"run_id"`
	EndsAt     string  `json:"ends_at"`
	Confidence float64 `json:"confidence"`
	Status     string  `json:"status" jsonschema:"active, or reverted"`
	CreatedAt  string  `json:"created_at"`
	RevertedAt string  `json:"reverted_at,omitempty"`
}

// ScanPair is a candidate pair found by scan: exactly what detection queues.
type ScanPair struct {
	OlderID string   `json:"older_id"`
	NewerID string   `json:"newer_id"`
	Cosine  float64  `json:"cosine"`
	Differs string   `json:"differs"`
	Shared  []string `json:"shared"`
}

// ScanRefusal is a neighbour pair that passed the near-duplicate guard's
// cosine stage (above the model's threshold, or on its lexical path) and
// that the guard refused because the signatures differ. OlderID is the
// earlier-written record, NewerID the later one.
type ScanRefusal struct {
	OlderID string  `json:"older_id"`
	NewerID string  `json:"newer_id"`
	Cosine  float64 `json:"cosine"`
	Reason  string  `json:"reason" jsonschema:"numbers_differ, dates_differ, entities_differ or negation_differs"`
}

// ConflictScan is the result of the offline detection.
type ConflictScan struct {
	MinCosine         float64        `json:"min_cosine" jsonschema:"similarity floor used"`
	EventsScanned     int            `json:"events_scanned"`
	NeighbourPairs    int            `json:"neighbour_pairs" jsonschema:"(event, neighbour) pairs compared"`
	CandidatePairs    int            `json:"candidate_pairs" jsonschema:"pairs detection would queue"`
	Queued            int            `json:"queued" jsonschema:"pairs written to the queue (write: true)"`
	AlreadyQueued     int            `json:"already_queued" jsonschema:"candidate pairs already in the queue (write: true)"`
	AlreadySuperseded int            `json:"already_superseded" jsonschema:"candidate pairs with a supersession row, never queued again (write: true)"`
	RefusalsByReason  map[string]int `json:"near_dup_refusals_by_reason" jsonschema:"guard refusals by reason"`
	PairsByComponent  map[string]int `json:"pairs_by_component" jsonschema:"candidate pairs by differing component"`
	Candidates        []ScanPair     `json:"candidates,omitzero" jsonschema:"details: true only"`
	NearDupRefusals   []ScanRefusal  `json:"near_dup_refusals,omitzero" jsonschema:"details: true only"`
}

// ConflictsOutput reports the action and the counts of the namespace after it.
type ConflictsOutput struct {
	Meta
	Action        string           `json:"action"`
	Enabled       bool             `json:"enabled" jsonschema:"sleep.conflicts.enabled"`
	Pending       int64            `json:"pending" jsonschema:"pairs waiting for a verdict"`
	Judged        int64            `json:"judged" jsonschema:"pairs judged"`
	Verdicts      map[string]int64 `json:"verdicts" jsonschema:"judged pairs by verdict after the rules"`
	Reasons       map[string]int64 `json:"reasons" jsonschema:"judged pairs by reason"`
	Active        int64            `json:"active" jsonschema:"supersessions in effect"`
	Reverted      int64            `json:"reverted" jsonschema:"supersessions reverted"`
	Pairs         []ConflictPair   `json:"pairs,omitzero" jsonschema:"list: every pair, by id"`
	Supersessions []Supersession   `json:"supersessions,omitzero" jsonschema:"list: every supersession, by id"`
	RevertedID    string           `json:"reverted_id,omitempty" jsonschema:"revert: the supersession reverted"`
	Removed       int64            `json:"removed,omitempty" jsonschema:"reset: pairs deleted"`
	Scan          *ConflictScan    `json:"scan,omitempty" jsonschema:"scan: the detection counts"`
}
