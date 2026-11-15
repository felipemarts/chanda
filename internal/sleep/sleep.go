// Package sleep is offline consolidation (BASE.md "Sleep"): the only place an
// LLM touches memory.
//
// Phase 1 always runs and is deterministic: integrity check, pending
// embeddings, utility, entity-merge proposals (never applied here),
// stale_source marking and page rebuilds. Phase 2 runs only with an LLM
// provider and an evaluator: fact extraction from unprocessed events,
// verdicts on pending items, lesson pages by addition. Phase 2 output is
// staged inside one transaction, measured by the evaluation gate and either
// committed or rolled back to the snapshot state (docs/decisions/0061).
//
// sleep depends on other areas only through the small interfaces below;
// integration wires the real implementations.
package sleep

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/store"
)

// Actor recorded in the audit log for mutations made by sleep.
const Actor = "sleep"

// Audit actions of sleep (registered in internal/audit).
const (
	ActMergePropose = audit.ActMergePropose
	ActMergeVerdict = audit.ActMergeVerdict
	ActFactVerdict  = audit.ActFactVerdict
	ActEventsMarked = audit.ActEventsProcessed
	ActGatePropose  = audit.ActGatePropose
)

// event_processing steps written by sleep.
const (
	StepExtractFacts = "extract_facts"
	StepLessonReview = "lesson_review"
)

// Run statuses (sleep_runs.status).
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusPartial   = "partial"
	StatusReverted  = "reverted"
	StatusFailed    = "failed"
)

// ---------------------------------------------------------------------------
// Consumer interfaces, implemented by other areas.

// Candidate is a fact extracted in phase 2.
type Candidate struct {
	Subject        string // entity name as written; the sink resolves it (write step 8)
	SubjectType    string // entity type, may be empty
	Predicate      string // snake_case
	Value          string
	ValidFrom      string   // core.TimeLayout; LLM date, else event valid_from, document date, recorded_at
	OriginEventIDs []string // sorted, non-empty; every id is a live event the LLM was shown
}

// FactSink runs write-path steps 8 to 10 for one candidate inside tx: it
// resolves the entity, reconciles with the decision table, marks
// near-duplicates, adds derived_from edges to OriginEventIDs, sets the zone
// to the lowest origin zone and audits, all through tx. action is a short
// label ("inserted", "confirmed", "superseded", "history", "quarantined",
// "near_duplicate", ...) used for the report.
type FactSink interface {
	AssertCandidate(ctx context.Context, tx *sql.Tx, ns string, c Candidate) (action string, err error)
}

// Integrity is the provenance checker: it returns how many derived records
// in ns do not reach an event. It must only use q (it runs inside the gate
// transaction).
type Integrity interface {
	Check(ctx context.Context, q store.Queryer, ns string) (violations int, err error)
}

// Embeddings drains the embedding queue of ns (phase 1 "finishes pending
// embeddings"). Called outside any sleep transaction.
type Embeddings interface {
	Flush(ctx context.Context, ns string) error
}

// Utility recomputes the utility table of ns from retrievals and outcomes.
// Called outside any sleep transaction.
type Utility interface {
	RecomputeUtility(ctx context.Context, ns string) error
}

// Evaluator scores ns on its fixed gate set, in points (0-100). It must be
// read-only and must use only q: it runs inside the gate transaction, which
// holds the single database connection.
type Evaluator interface {
	Score(ctx context.Context, q store.Queryer, ns string) (float64, error)
}

// GateSizer is optionally implemented by an Evaluator: how many questions
// it scores in ns. With none, the gate measures nothing and rests on the
// integrity check alone; the run says so in a notice.
type GateSizer interface {
	GateSize(ctx context.Context, q store.Queryer, ns string) (int, error)
}

// Learning trains the stage-2 ranker and attempts its promotion through
// the gate set (phase 1; BASE.md "Ranking that learns from use"). Called
// outside any sleep transaction, after the utility recompute.
type Learning interface {
	Learn(ctx context.Context, ns string) (LearnResult, error)
}

// LearnResult is what Learning did: ids and numbers only.
type LearnResult struct {
	Trained   string   `json:"trained,omitempty"`   // model trained in this run
	Evaluated string   `json:"evaluated,omitempty"` // model the promotion evaluated
	Passed    bool     `json:"passed"`
	Promoted  bool     `json:"promoted"` // the model became the live one in this run
	Baseline  float64  `json:"baseline"`
	Learned   float64  `json:"learned"`
	Lambda    float64  `json:"lambda"` // λ after the run
	Notices   []string `json:"notices,omitempty"`
}

// EntityVectors optionally returns one vector per entity id (missing ids are
// simply absent), for vector-based merge proposals.
type EntityVectors interface {
	EntityVectors(ctx context.Context, ns string, ids []string) (map[string][]float32, error)
}

// Deps are the collaborators of a Sleeper. Every field is optional: a nil
// LLM or Evaluator disables phase 2 with a notice; other nil fields skip
// their step with a notice.
type Deps struct {
	LLM           core.LLMProvider
	Facts         FactSink
	Integrity     Integrity
	Embeddings    Embeddings
	Utility       Utility
	Evaluator     Evaluator
	EntityVectors EntityVectors
	Learning      Learning
	// Logf receives scheduler errors (Start); nil discards them.
	Logf func(format string, args ...any)
	// Ticks, when set, paces the scheduler loop instead of a wall-clock
	// ticker (tests drive Start with it and the fake Clock).
	Ticks <-chan time.Time
}

// Sleeper runs sleep for every namespace of one database.
type Sleeper struct {
	env     *env.Env
	d       Deps
	running sync.Mutex
}

// New returns a Sleeper over e.
func New(e *env.Env, d Deps) *Sleeper { return &Sleeper{env: e, d: d} }

// Budget is the ceiling of one run. Zero fields take the configured default.
type Budget struct {
	Tokens int `json:"tokens"`
	Events int `json:"events"`
}

// RunInput is a request to run sleep (sleep_run tool, CLI or scheduler).
type RunInput struct {
	Namespace string // empty: cfg.Namespace
	Phases    []int  // subset of {1,2}; empty: both. Phase 1 always runs.
	Budget    Budget
	Actor     string // who triggered the run; empty: Actor
}

// Cost is what the run spent on the LLM.
type Cost struct {
	LLMCalls     int `json:"llm_calls"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Tokens is input plus output tokens.
func (c Cost) Tokens() int { return c.InputTokens + c.OutputTokens }

// Counts lists what a run did. Id lists are sorted.
type Counts struct {
	IntegrityViolations int         `json:"integrity_violations"`
	MergeProposals      []string    `json:"merge_proposals"`
	StaleFacts          []string    `json:"stale_facts"`
	PagesRebuilt        []string    `json:"pages_rebuilt"`
	GateProposed        []string    `json:"gate_proposed"` // gate questions proposed (status proposed)
	Learning            LearnResult `json:"learning"`

	EventsExtracted  int            `json:"events_extracted"`
	EventsSkipped    []string       `json:"events_skipped"` // larger than the whole token budget
	FactActions      map[string]int `json:"fact_actions"`
	CandidatesDrop   int            `json:"candidates_dropped"`
	Verdicts         map[string]int `json:"verdicts"`
	FactsActivated   []string       `json:"facts_activated"`
	FactsSuperseded  []string       `json:"facts_superseded"`
	FactsHistory     []string       `json:"facts_history"`
	FactsQuarantined []string       `json:"facts_quarantined"` // active facts of a lower zone quarantined by a merge (decision 0103)
	FactsRejected    []string       `json:"facts_rejected"`
	VerdictsRefused  []string       `json:"verdicts_refused"`
	MergesApplied    []string       `json:"merges_applied"`
	MergesRejected   []string       `json:"merges_rejected"`
	PagesCreated     []string       `json:"pages_created"`
	PageItemsAdded   []string       `json:"page_items_added"`
	LessonsSkipped   int            `json:"lessons_skipped"`
	InvalidAnswers   int            `json:"invalid_answers"`
	LLMErrors        int            `json:"llm_errors"`

	// Guided is what the guided digest did (sleep.mode guided, decision
	// 0142); nil, and absent from the JSON, in uniform mode.
	Guided *GuidedCounts `json:"guided,omitempty"`
}

// Gate is the outcome of the evaluation gate.
type Gate struct {
	Ran              bool    `json:"ran"`
	Before           float64 `json:"before"`
	After            float64 `json:"after"`
	Tolerance        float64 `json:"tolerance"`
	ViolationsBefore int     `json:"violations_before"`
	ViolationsAfter  int     `json:"violations_after"`
	Reverted         bool    `json:"reverted"`
	Reason           string  `json:"reason,omitempty"`
}

// Result is the outcome of one run.
type Result struct {
	RunID     string   `json:"run_id"`
	Namespace string   `json:"namespace"`
	Status    string   `json:"status"`
	Phases    []int    `json:"phases"`
	Budget    Budget   `json:"budget"`
	Snapshot  string   `json:"snapshot,omitempty"`
	Cost      Cost     `json:"cost"`
	Counts    Counts   `json:"counts"`
	Gate      Gate     `json:"gate"`
	Notices   []string `json:"notices"`
	Report    string   `json:"report"`
}

func (r *Result) notice(s string) { r.Notices = append(r.Notices, s) }

// run is the mutable state of one Run.
type run struct {
	ns      string
	actor   string
	res     *Result
	budget  Budget
	partial bool // a ceiling was reached or the provider failed: the rest waits for the next run
	stopLLM bool

	eventsUsed    int // events sent to the LLM (extraction batches and lesson groups)
	budgetNoticed bool
	snapshotAbs   string // filesystem path of this run's snapshot
}

func (r *run) tokensLeft() int { return r.budget.Tokens - r.res.Cost.Tokens() }

func (s *Sleeper) now() string { return core.FormatTime(s.env.Clock.Now()) }

func (s *Sleeper) db() *sql.DB { return s.env.Store.DB() }

func (s *Sleeper) audit(ctx context.Context, q store.Queryer, ns, action, target string, details any) error {
	_, err := s.env.Audit.Append(ctx, q, audit.Entry{Namespace: ns, Actor: Actor, Action: action, Target: target, Details: details})
	return err
}

func normalizePhases(in []int) ([]int, error) {
	if len(in) == 0 {
		return []int{1, 2}, nil
	}
	set := map[int]bool{1: true}
	for _, p := range in {
		if p != 1 && p != 2 {
			return nil, core.Errf(core.CodeInvalidArgument, "sleep: unknown phase %d (phases are 1 and 2)", p)
		}
		set[p] = true
	}
	out := make([]int, 0, 2)
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out, nil
}

func (s *Sleeper) budgetFor(b Budget) (Budget, error) {
	if b.Tokens < 0 || b.Events < 0 {
		return b, core.Errf(core.CodeInvalidArgument, "sleep: budget must not be negative")
	}
	if b.Tokens == 0 {
		b.Tokens = s.env.Cfg.Sleep.BudgetTokens
	}
	if b.Events == 0 {
		b.Events = s.env.Cfg.Sleep.BudgetEvents
	}
	return b, nil
}

// Run executes one sleep run on demand. Phase 1 always runs; phase 2 runs
// when requested and a provider and an evaluator are configured (otherwise
// a notice says why it was skipped). A run never starts during a write burst
// or while another run is in progress (CodeConflict). The returned Result is
// non-nil whenever the run row was created, also together with an error.
func (s *Sleeper) Run(ctx context.Context, in RunInput) (*Result, error) {
	ns := in.Namespace
	if ns == "" {
		ns = s.env.Cfg.Namespace
	}
	phases, err := normalizePhases(in.Phases)
	if err != nil {
		return nil, err
	}
	budget, err := s.budgetFor(in.Budget)
	if err != nil {
		return nil, err
	}
	actor := in.Actor
	if actor == "" {
		actor = Actor
	}
	if !s.running.TryLock() {
		return nil, core.Errf(core.CodeConflict, "sleep: a run is already in progress")
	}
	defer s.running.Unlock()
	// No run of this process is in progress (the lock is held): a row still
	// "running" past the timeout belongs to a process that stopped.
	if _, err := s.RecoverStale(ctx); err != nil {
		return nil, err
	}

	st, err := s.state(ctx, ns)
	if err != nil {
		return nil, err
	}
	if PolicyFrom(s.env.Cfg.Sleep).InBurst(s.env.Clock.Now(), st.LastWriteAt) {
		return nil, core.Errf(core.CodeConflict, "sleep: write burst in progress; try again later")
	}

	r := &run{ns: ns, actor: actor, budget: budget, res: &Result{
		RunID: s.env.IDs.New(core.PrefixSleepRun), Namespace: ns, Phases: phases, Budget: budget,
		Status: StatusRunning, Notices: []string{},
		Counts: Counts{FactActions: map[string]int{}, Verdicts: map[string]int{}},
	}}
	if err := s.start(ctx, r); err != nil {
		return nil, err
	}
	err = s.phase1(ctx, r)
	if err == nil && len(phases) == 2 {
		err = s.phase2(ctx, r)
	}
	if err == nil {
		err = s.closing(ctx, r)
	}
	return r.res, s.finish(ctx, r, err)
}

func (s *Sleeper) start(ctx context.Context, r *run) error {
	phases, _ := json.Marshal(r.res.Phases)
	budget, _ := json.Marshal(r.budget)
	provider := ""
	if s.d.LLM != nil {
		provider = s.d.LLM.Name()
	}
	inputs, _ := json.Marshal(map[string]string{"actor": r.actor, "provider": provider})
	return s.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO sleep_runs(id, namespace, started_at, phases, budget, inputs, status)
			VALUES (?,?,?,?,?,?,?)`, r.res.RunID, r.ns, s.now(), string(phases), string(budget), string(inputs), StatusRunning)
		if err != nil {
			return err
		}
		_, err = s.env.Audit.Append(ctx, tx, audit.Entry{Namespace: r.ns, Actor: r.actor, Action: audit.ActSleepStart,
			Target: r.res.RunID, Details: map[string]any{"phases": r.res.Phases, "budget": r.budget}})
		return err
	})
}

// closing runs after the phases: embeddings of what phase 2 wrote, and the
// integrity check that BASE.md requires at the end of every sleep.
func (s *Sleeper) closing(ctx context.Context, r *run) error {
	if s.d.Embeddings != nil {
		if err := s.d.Embeddings.Flush(ctx, r.ns); err != nil {
			return err
		}
	}
	if s.d.Integrity != nil {
		v, err := s.d.Integrity.Check(ctx, s.db(), r.ns)
		if err != nil {
			return err
		}
		if v > r.res.Counts.IntegrityViolations {
			r.res.notice("integrity: violations rose during the run; see the provenance checker")
		}
		r.res.Counts.IntegrityViolations = v
	}
	return nil
}

// finish records the outcome. It writes with a context that ignores the
// caller's cancellation (App.Close cancels the scheduler and waits): a
// cancelled run must end as failed, never stay "running".
func (s *Sleeper) finish(ctx context.Context, r *run, runErr error) error {
	cancelled, cause := ctx.Err() != nil, context.Cause(ctx)
	ctx = context.WithoutCancel(ctx)
	res := r.res
	switch {
	case cancelled:
		res.Status = StatusFailed
		res.notice("run cancelled before it finished (shutdown); what the gate had not committed was rolled back")
		if runErr == nil {
			runErr = core.Wrap(core.CodeUnavailable, cause, "sleep: run cancelled")
		}
	case runErr != nil:
		res.Status = StatusFailed
		res.notice("run failed: " + runErr.Error())
	case res.Gate.Reverted:
		res.Status = StatusReverted
	case r.partial:
		res.Status = StatusPartial
	default:
		res.Status = StatusCompleted
	}
	sortCounts(&res.Counts)
	res.Report = renderReport(res)
	outputs, _ := json.Marshal(struct {
		Counts Counts `json:"counts"`
		Gate   Gate   `json:"gate"`
	}{res.Counts, res.Gate})
	cost, _ := json.Marshal(res.Cost)
	var snap any
	if res.Snapshot != "" {
		snap = res.Snapshot
	}
	err := s.env.Store.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE sleep_runs SET ended_at=?, status=?, outputs=?, snapshot=?, report=?, cost=? WHERE id=?`,
			s.now(), res.Status, string(outputs), snap, res.Report, string(cost), res.RunID)
		if err != nil {
			return err
		}
		_, err = s.env.Audit.Append(ctx, tx, audit.Entry{Namespace: r.ns, Actor: r.actor, Action: audit.ActSleepEnd,
			Target: res.RunID, Details: map[string]any{
				"status": res.Status, "llm_calls": res.Cost.LLMCalls,
				"input_tokens": res.Cost.InputTokens, "output_tokens": res.Cost.OutputTokens,
				"facts": sumActions(res.Counts.FactActions), "page_items": len(res.Counts.PageItemsAdded),
				"merge_proposals": len(res.Counts.MergeProposals), "stale_facts": len(res.Counts.StaleFacts),
			}})
		return err
	})
	if runErr != nil {
		return runErr
	}
	return err
}

func sumActions(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func sortCounts(c *Counts) {
	for _, l := range []*[]string{&c.MergeProposals, &c.StaleFacts, &c.PagesRebuilt, &c.GateProposed, &c.EventsSkipped,
		&c.FactsActivated, &c.FactsSuperseded, &c.FactsHistory, &c.FactsQuarantined, &c.FactsRejected, &c.VerdictsRefused,
		&c.MergesApplied, &c.MergesRejected, &c.PagesCreated, &c.PageItemsAdded} {
		if *l == nil {
			*l = []string{}
		}
		sort.Strings(*l)
	}
	if c.Guided != nil {
		sort.Strings(c.Guided.Fichas)
	}
}

// ---------------------------------------------------------------------------
// Namespace state used by the trigger policy.

// State is what the trigger policy needs to know about a namespace.
type State struct {
	LastWriteAt time.Time // latest event or fact recorded_at; zero if none
	NewEvents   int       // events recorded after the last run started
	LastRunAt   time.Time // latest sleep_runs.started_at; zero if never
}

func (s *Sleeper) state(ctx context.Context, ns string) (State, error) {
	var st State
	var lastRun, lastWrite sql.NullString
	db := s.db()
	if err := db.QueryRowContext(ctx, `SELECT MAX(started_at) FROM sleep_runs WHERE namespace=?`, ns).Scan(&lastRun); err != nil {
		return st, err
	}
	if err := db.QueryRowContext(ctx, `SELECT MAX(t) FROM (
		SELECT MAX(recorded_at) AS t FROM events WHERE namespace=?
		UNION ALL SELECT MAX(recorded_at) FROM facts WHERE namespace=?)`, ns, ns).Scan(&lastWrite); err != nil {
		return st, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE namespace=? AND recorded_at > ?`,
		ns, lastRun.String).Scan(&st.NewEvents); err != nil {
		return st, err
	}
	var err error
	if lastRun.Valid {
		if st.LastRunAt, err = core.ParseTime(lastRun.String); err != nil {
			return st, err
		}
	}
	if lastWrite.Valid {
		if st.LastWriteAt, err = core.ParseTime(lastWrite.String); err != nil {
			return st, err
		}
	}
	return st, nil
}
