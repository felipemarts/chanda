// Package conflicts is conflict resolution between raw records
// (docs/decisions/0148-conflict-resolution-in-sleep.md), off by default.
//
//   - Detection (Detect, no LLM) runs on the write path right after the
//     near-duplicate guard, over the guard's own neighbours and cosines, and
//     fills the conflict_candidates queue (Enqueue). It never changes a
//     record.
//   - Sleep phase 2 asks an LLM for a verdict on each pending pair; Decide
//     (no LLM) then decides whether the verdict takes effect.
//   - An applied verdict is a supersessions row (old -> new from ends_at),
//     staged and promoted through the sleep gate. Reads honour active rows.
package conflicts

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/dedup"
	"github.com/felipemarts/chanda/internal/facts/norm"
)

// Signature components a candidate pair can differ in (the differs column).
const (
	CompNumbers   = "numbers"
	CompDates     = "dates"
	CompEntities  = "entities"
	CompNegations = "negations"
)

// Components lists the components in Sig.Compare order.
func Components() []string { return []string{CompNumbers, CompDates, CompEntities, CompNegations} }

// RefusalReasons lists the near-duplicate guard's refusal reasons that scan
// counts (dedup's Compare reasons other than near_duplicate).
func RefusalReasons() []string {
	return []string{dedup.ReasonNumbersDiffer, dedup.ReasonDatesDiffer, dedup.ReasonEntitiesDiffer, dedup.ReasonNegationDiffers}
}

// component maps a Sig.Compare reason to its component.
func component(reason string) string {
	switch reason {
	case dedup.ReasonNumbersDiffer:
		return CompNumbers
	case dedup.ReasonDatesDiffer:
		return CompDates
	case dedup.ReasonEntitiesDiffer:
		return CompEntities
	case dedup.ReasonNegationDiffers:
		return CompNegations
	}
	return ""
}

// Pair statuses (conflict_candidates.status).
const (
	StatusPending = "pending"
	StatusJudged  = "judged"
)

// Supersession statuses (supersessions.status).
const (
	SupStaged = "staged"
	SupActive = "active"
)

// Record is what detection reads of an event.
type Record struct {
	ID, Content, ValidFrom, RecordedAt string
}

// Effective is the record's effective time: valid_from when set, else
// recorded_at. ok is false when neither parses.
func (r Record) Effective() (time.Time, bool) { return EffectiveTime(r.ValidFrom, r.RecordedAt) }

// EffectiveTime parses valid_from when set, else recorded_at.
func EffectiveTime(validFrom, recordedAt string) (time.Time, bool) {
	s := validFrom
	if strings.TrimSpace(s) == "" {
		s = recordedAt
	}
	t, err := core.ParseTime(s)
	return t, err == nil
}

// Neighbour is one of the guard's candidates with its cosine to the new
// record.
type Neighbour struct {
	Record
	Cosine float64
}

// Params are the detection settings: sleep.conflicts.min_cosine and
// max_per_record, and the near-duplicate guard of the embedding model.
type Params struct {
	MinCosine    float64
	MaxPerRecord int // 0: no cap
	Threshold    float64
	Lexical      dedup.Lexical
}

// Pair is one candidate pair. OlderID and NewerID are ordered by effective
// time.
type Pair struct {
	OlderID string   `json:"older_id"`
	NewerID string   `json:"newer_id"`
	Cosine  float64  `json:"cosine"`
	Differs string   `json:"differs"` // numbers, dates, entities or negations
	Shared  []string `json:"shared"`  // shared entities and content terms, sorted
}

// Refusal is a neighbour pair that passed the guard's cosine stage (above
// the plain threshold, or on the lexical path) and that the guard refused
// because the signatures differ. OlderID is the existing neighbour and
// NewerID the new record, as the guard sees them (insertion order).
type Refusal struct {
	OlderID string  `json:"older_id"`
	NewerID string  `json:"newer_id"`
	Cosine  float64 `json:"cosine"`
	Reason  string  `json:"reason"` // numbers_differ, dates_differ, entities_differ, negation_differs
}

// maxShared bounds the shared list stored with a pair.
const maxShared = 32

// Detect returns the candidate pairs of the new record n with its
// neighbours, at most p.MaxPerRecord, highest cosine first (ties by
// neighbour id), and the guard's refusals among the neighbours (neighbour
// order). A neighbour forms a pair when all of these hold:
//
//  1. cosine >= p.MinCosine;
//  2. the near-duplicate guard does not accept the pair;
//  3. the two share an entity of their dedup.Signature or a content term
//     (letters only, four or more, not a stop word, after norm.Alias);
//  4. their signatures differ (numbers, dates, entities or negations);
//  5. their effective times differ.
func Detect(n Record, nb []Neighbour, p Params) ([]Pair, []Refusal) {
	type scored struct {
		pair Pair
		nid  string
	}
	var cands []scored
	var refusals []Refusal
	nSig := dedup.Signature(n.Content)
	nTerms := Terms(n.Content)
	nT, nOK := n.Effective()
	for _, x := range nb {
		if x.ID == n.ID {
			continue
		}
		ok, reason := dedup.IsNearDuplicateOf(n.Content, x.Content, x.Cosine, p.Threshold, p.Lexical)
		if !ok && reason != dedup.ReasonBelowThreshold {
			refusals = append(refusals, Refusal{OlderID: x.ID, NewerID: n.ID, Cosine: x.Cosine, Reason: reason})
		}
		if ok || !(x.Cosine >= p.MinCosine) {
			continue
		}
		xSig := dedup.Signature(x.Content)
		same, why := nSig.Compare(xSig)
		if same {
			continue
		}
		shared := intersect(nSig.Entities, xSig.Entities)
		shared = append(shared, intersect(nTerms, Terms(x.Content))...)
		shared = uniqueSorted(shared)
		if len(shared) == 0 {
			continue
		}
		xT, xOK := x.Effective()
		if !nOK || !xOK || nT.Equal(xT) {
			continue
		}
		if len(shared) > maxShared {
			shared = shared[:maxShared]
		}
		pr := Pair{OlderID: x.ID, NewerID: n.ID, Cosine: x.Cosine, Differs: component(why), Shared: shared}
		if nT.Before(xT) {
			pr.OlderID, pr.NewerID = n.ID, x.ID
		}
		cands = append(cands, scored{pr, x.ID})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].pair.Cosine != cands[j].pair.Cosine {
			return cands[i].pair.Cosine > cands[j].pair.Cosine
		}
		return cands[i].nid < cands[j].nid
	})
	if p.MaxPerRecord > 0 && len(cands) > p.MaxPerRecord {
		cands = cands[:p.MaxPerRecord]
	}
	out := make([]Pair, len(cands))
	for i, c := range cands {
		out[i] = c.pair
	}
	return out, refusals
}

// stopwords are function words of four or more letters (after norm.Alias)
// that never make two records related.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`that this these those with without from have been were what which when where whom whose
	about into than then there their they them will would could should shall just also very some more most such only over
	after before because while being does done here your yours ours mine like even still much many each other
	isso isto este esta esse essa estes estas esses essas sobre entre quando onde quem qual quais para pela pelo pelas pelos
	como mais muito muita muitos muitas voce voces nosso nossa minha meus minhas seus suas dele dela deles delas eles elas
	tambem porque ainda estou estava estao sendo foram seria tinha tenho temos fazer feito pode podem depois antes agora`) {
		stopwords[w] = true
	}
}

// Terms are the content terms of text as condition 3 compares them:
// words after norm.Alias made of letters only, four or more of them, that
// are not stop words. Sorted, unique.
func Terms(text string) []string {
	var out []string
	for _, w := range strings.Fields(norm.Alias(text)) {
		if len([]rune(w)) < 4 || stopwords[w] || strings.ContainsFunc(w, func(r rune) bool { return !unicode.IsLetter(r) }) {
			continue
		}
		out = append(out, w)
	}
	return uniqueSorted(out)
}

// intersect returns the elements of the sorted, unique a also in b.
func intersect(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if in[x] {
			out = append(out, x)
		}
	}
	return out
}

func uniqueSorted(xs []string) []string {
	sort.Strings(xs)
	out := xs[:0]
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// IDs draws new ids (env.IDs).
type IDs interface{ New(prefix string) string }

// Enqueue stores pairs in the conflict queue of ns as pending, through q (the
// write transaction). A pair already queued (in either status) is skipped.
// It returns how many rows it inserted.
func Enqueue(ctx context.Context, q Execer, ids IDs, ns, now string, pairs []Pair) (int, error) {
	n := 0
	for _, p := range pairs {
		var one int
		err := q.QueryRowContext(ctx, `SELECT 1 FROM conflict_candidates WHERE namespace = ? AND older_id = ? AND newer_id = ?`,
			ns, p.OlderID, p.NewerID).Scan(&one)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return n, err
		}
		shared, _ := json.Marshal(p.Shared)
		if p.Shared == nil {
			shared = []byte("[]")
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO conflict_candidates(id, namespace, older_id, newer_id, cosine, differs, shared, status, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`, ids.New(core.PrefixConflict), ns, p.OlderID, p.NewerID, p.Cosine, p.Differs, string(shared),
			StatusPending, now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Execer is a transaction or a database.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
