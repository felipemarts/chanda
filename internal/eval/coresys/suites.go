package coresys

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/eval"
	"github.com/felipemarts/chanda/pkg/chanda"
)

// Register attaches the suites that need a live core to r. Each suite builds
// its own core with the C3 configuration (the most complete one that needs
// neither a reranker nor sleep; without a reranker, exactly what the report
// runs as C3), over the evaluation config and embedder of fe. The temporal
// suite keeps eval.TemporalSuite's measure and adds the stale-record
// diagnostics of TemporalSuite.
func Register(r *eval.Registry, fe eval.FactoryEnv) error {
	for name, run := range map[string]func(context.Context, eval.FactoryEnv, eval.SuiteInput) (eval.Measured, error){
		eval.SuiteDeletion:       DeletionSuite,
		eval.SuiteInjection:      InjectionSuite,
		eval.SuiteDocumentUpdate: DocumentUpdateSuite,
		eval.SuiteDeduplication:  DeduplicationSuite,
		eval.SuiteDeterminism:    DeterminismSuite,
	} {
		run := run
		if err := r.Register(name, false, func(ctx context.Context, in eval.SuiteInput) (eval.Measured, error) {
			return run(ctx, fe, in)
		}); err != nil {
			return err
		}
	}
	return r.Register(eval.SuiteTemporal, true, TemporalSuite)
}

// suiteVariant is the configuration the core suites run on.
func suiteVariant() Variant {
	v := Variants[2] // C3
	v.Reranker = false
	return v
}

// loaded builds a suite core and ingests the whole corpus into it.
func loaded(ctx context.Context, fe eval.FactoryEnv, ds *eval.Dataset, v Variant) (*System, error) {
	s, err := Open(ctx, v, fe)
	if err != nil {
		return nil, err
	}
	for _, it := range ds.Corpus {
		if err := s.Ingest(ctx, it); err != nil {
			s.Close()
			return nil, fmt.Errorf("ingest %s v%d: %w", it.ID, it.Version, err)
		}
	}
	if err := s.flush(ctx); err != nil {
		s.Close()
		return nil, err
	}
	s.queryTime()
	return s, nil
}

// ---------------------------------------------------------------------------
// Deletion.

func normTerms(s string) []string {
	return strings.FieldsFunc(eval.Normalize(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// containsTerms reports whether the terms of needle occur in text as a
// contiguous run of whole terms ("considere" is not in "desconsidere").
func containsTerms(text, needle string) bool {
	return strings.Contains(" "+strings.Join(normTerms(text), " ")+" ", " "+strings.Join(normTerms(needle), " ")+" ")
}

// uniqueNeedles returns, per corpus item id, its distinctive terms: terms of
// at least 5 characters (or 4 digits) that occur in no other corpus item.
func uniqueNeedles(ds *eval.Dataset) map[string][]string {
	owners := map[string]map[string]bool{}
	for _, it := range ds.Corpus {
		for _, t := range normTerms(eval.ItemText(it)) {
			if len([]rune(t)) < 5 && !(len(t) >= 4 && strings.IndexFunc(t, func(r rune) bool { return !unicode.IsDigit(r) }) < 0) {
				continue
			}
			if owners[t] == nil {
				owners[t] = map[string]bool{}
			}
			owners[t][it.ID] = true
		}
	}
	out := map[string][]string{}
	for t, o := range owners {
		if len(o) == 1 {
			for id := range o {
				out[id] = append(out[id], t)
			}
		}
	}
	for id := range out {
		sort.Strings(out[id])
	}
	return out
}

type forgetCase struct {
	scope   chanda.ForgetScope
	needles []string // terms that must be unfindable afterwards
	gone    []string // record ids that must be not_found afterwards
	// factsOnly limits the term probes to facts: an entity scope removes
	// the facts about the entity and the events they came from or that
	// mention it, not every paragraph under the same heading.
	factsOnly bool
}

// DeletionSuite runs real forget requests of every scope kind against a core
// holding the whole corpus. A request passes when forget's own verification
// passes every probe AND the suite's independent probes pass: search (with
// history) and context return nothing containing the item's distinctive
// terms, and the removed record ids answer not_found.
func DeletionSuite(ctx context.Context, fe eval.FactoryEnv, in eval.SuiteInput) (eval.Measured, error) {
	ds := in.Dataset
	s, err := loaded(ctx, fe, ds, suiteVariant())
	if err != nil {
		return nil, err
	}
	defer s.Close()
	needles := uniqueNeedles(ds)
	pick := func(id string, n int) []string {
		ns := needles[id]
		return ns[:min(n, len(ns))]
	}
	var cases []forgetCase
	var sessions, docs, external []eval.Item
	seen := map[string]bool{}
	for _, it := range ds.Corpus {
		if seen[it.ID] || len(needles[it.ID]) == 0 {
			continue
		}
		seen[it.ID] = true
		switch {
		case it.Type == eval.ItemSession && len(s.events[it.ID]) > 0:
			sessions = append(sessions, it)
		case it.Source == "web" || it.Source == "tool":
			external = append(external, it)
		case it.Type == eval.ItemDocument:
			docs = append(docs, it)
		}
	}
	// record: one turn of three sessions (the turn holding a distinctive term).
	for _, it := range sessions[:min(3, len(sessions))] {
		for i, t := range it.Turns {
			var ns []string
			for _, n := range needles[it.ID] {
				elsewhere := false
				for j, o := range it.Turns {
					elsewhere = elsewhere || (j != i && slices.Contains(normTerms(o.Text), n))
				}
				for _, f := range it.Facts {
					elsewhere = elsewhere || slices.Contains(normTerms(f.Text()), n)
				}
				if !elsewhere && slices.Contains(normTerms(t.Text), n) {
					ns = append(ns, n)
				}
			}
			if len(ns) > 0 && i < len(s.events[it.ID]) {
				id := s.events[it.ID][i]
				cases = append(cases, forgetCase{scope: chanda.ForgetScope{Kind: chanda.ScopeRecord, Value: id}, needles: ns[:min(2, len(ns))], gone: []string{id}})
				break
			}
		}
	}
	// document: the first versioned document and two others.
	var versioned []eval.Item
	for _, it := range docs {
		for _, other := range ds.Corpus {
			if other.ID == it.ID && other.Version > 1 {
				versioned = append(versioned, it)
				break
			}
		}
	}
	docPick := append([]eval.Item{}, versioned[:min(1, len(versioned))]...)
	for _, it := range docs {
		if len(docPick) >= 3 {
			break
		}
		if !slices.ContainsFunc(docPick, func(x eval.Item) bool { return x.ID == it.ID }) {
			docPick = append(docPick, it)
		}
	}
	for _, it := range docPick {
		cases = append(cases, forgetCase{scope: chanda.ForgetScope{Kind: chanda.ScopeDocument, Value: s.docURI(it.ID)}, needles: pick(it.ID, 2)})
	}
	// source: two external sources.
	for _, it := range external[:min(2, len(external))] {
		cases = append(cases, forgetCase{scope: chanda.ForgetScope{Kind: chanda.ScopeSource, Value: it.URI}, needles: pick(it.ID, 2)})
	}
	// date_range: the day of the last remaining session.
	if len(sessions) > 3 {
		it := sessions[len(sessions)-1]
		t, _ := eval.ItemTime(it.Date)
		cases = append(cases, forgetCase{scope: chanda.ForgetScope{Kind: chanda.ScopeDateRange,
			From: t.Format("2006-01-02"), To: t.Add(24 * time.Hour).Format("2006-01-02")}, needles: pick(it.ID, 2)})
	}
	// search: a distinctive term of two further documents.
	for _, it := range docs[min(len(docs), len(docPick)):] {
		if len(cases) >= 14 {
			break
		}
		if slices.ContainsFunc(docPick, func(x eval.Item) bool { return x.ID == it.ID }) {
			continue
		}
		n := pick(it.ID, 1)
		cases = append(cases, forgetCase{scope: chanda.ForgetScope{Kind: chanda.ScopeSearch, Query: n[0]}, needles: n})
	}

	var passed, coreFail, extraFail int
	byKind, failedKind := map[string]int{}, map[string]int{}
	run := func(cases []forgetCase) error {
		for _, c := range cases {
			byKind[c.scope.Kind]++
			ok := true
			out, err := s.app.Forget(op(ctx), chanda.ForgetInput{Namespace: Namespace, Scope: c.scope})
			if err != nil {
				return fmt.Errorf("forget %s %q: %w", c.scope.Kind, c.scope.Value+c.scope.Query, err)
			}
			if !out.Verification.Passed {
				coreFail++
				ok = false
				for _, p := range out.Verification.Probes {
					if !p.Passed {
						Logf("deletion: %s %s%s: forget probe %s failed (%s)", c.scope.Kind, c.scope.Value, c.scope.Query, p.Name, p.Detail)
					}
				}
			}
			if why, err := s.stillFindable(ctx, c); err != nil {
				return err
			} else if why != "" {
				extraFail++
				ok = false
				Logf("deletion: %s %s%s: %s", c.scope.Kind, c.scope.Value, c.scope.Query, why)
			}
			if ok {
				passed++
			} else {
				failedKind[c.scope.Kind]++
			}
		}
		return nil
	}
	if err := run(cases); err != nil {
		return nil, err
	}
	// entity, last: an entity scope also removes every event that names the
	// entity (decision 0130), so running it first would take the turns and
	// documents chosen above with it. Two subjects of facts still active
	// after the requests above, probed by those fact values that occur in a
	// single corpus item.
	ents, err := s.entitiesWithValues(ctx, 2)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(ds.Corpus))
	for i, it := range ds.Corpus {
		texts[i] = it.ID + "\x00" + eval.Normalize(eval.ItemText(it))
	}
	var entityCases []forgetCase
	for _, e := range ents {
		var vals []string
		for _, v := range e.values {
			owners := map[string]bool{}
			for _, t := range texts {
				id, body, _ := strings.Cut(t, "\x00")
				if containsTerms(body, v) {
					owners[id] = true
				}
			}
			if len(owners) == 1 {
				vals = append(vals, v)
			}
		}
		entityCases = append(entityCases, forgetCase{scope: chanda.ForgetScope{Kind: chanda.ScopeEntity, Value: e.name}, needles: vals,
			gone: append(append([]string{}, e.facts...), e.origins...), factsOnly: true})
	}
	if err := run(entityCases); err != nil {
		return nil, err
	}
	cases = append(cases, entityCases...)
	if len(cases) == 0 {
		return nil, fmt.Errorf("no forget request could be built from the dataset")
	}
	m := eval.Measured{"requests": float64(len(cases)), "passed": float64(passed),
		"forget_pass_rate":    float64(passed) / float64(len(cases)),
		"core_probe_failures": float64(coreFail), "independent_probe_failures": float64(extraFail)}
	for k, n := range byKind {
		m["requests_"+k] = float64(n)
	}
	for k, n := range failedKind {
		m["failed_"+k] = float64(n)
	}
	return m, nil
}

func (s *System) docURI(itemID string) string {
	for uri, id := range s.byURI {
		if id == itemID && strings.HasSuffix(uri, "/doc.md") {
			return uri
		}
	}
	return ""
}

type entityProbe struct {
	name    string
	values  []string
	facts   []string
	origins []string // events the facts derive from
}

// entitiesWithValues returns n entities (by canonical name) that have active
// facts with a value of at least 4 characters, with those values and facts.
func (s *System) entitiesWithValues(ctx context.Context, n int) ([]entityProbe, error) {
	rows, err := s.e.Store.DB().QueryContext(ctx, `SELECT e.canonical_name, f.id, f.value FROM facts f JOIN entities e ON e.id = f.subject
		WHERE f.namespace = ? AND f.status = 'active' AND length(f.value) >= 4 ORDER BY e.canonical_name, f.id`, Namespace)
	if err != nil {
		return nil, err
	}
	var out []entityProbe
	for rows.Next() {
		var name, id, value string
		if err := rows.Scan(&name, &id, &value); err != nil {
			rows.Close()
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].name != name {
			if len(out) == n {
				break
			}
			out = append(out, entityProbe{name: name})
		}
		e := &out[len(out)-1]
		e.facts = append(e.facts, id)
		e.values = append(e.values, value)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		for _, f := range out[i].facts {
			r, err := s.e.Store.DB().QueryContext(ctx, `SELECT source_id FROM provenance WHERE derived_id = ? AND relation = 'derived_from'
				AND source_type = 'event' ORDER BY source_id`, f)
			if err != nil {
				return nil, err
			}
			for r.Next() {
				var id string
				if err := r.Scan(&id); err != nil {
					r.Close()
					return nil, err
				}
				out[i].origins = append(out[i].origins, id)
			}
			r.Close()
		}
	}
	return out, nil
}

// Logf receives diagnostic lines from the suites (misses, with ids and
// terms, never record content). It discards them by default.
var Logf = func(format string, args ...any) {}

// stillFindable runs the independent probes of one forget request and says
// what is still there ("" when nothing is).
func (s *System) stillFindable(ctx context.Context, c forgetCase) (string, error) {
	for _, id := range c.gone {
		_, err := s.app.Get(op(ctx), chanda.GetInput{Namespace: Namespace, ID: id})
		if core.CodeOf(err) != core.CodeNotFound {
			return "get " + id + " still answers", nil
		}
	}
	for _, n := range c.needles {
		sr, err := s.app.Search(op(ctx), chanda.SearchInput{Namespace: Namespace, Query: n, IncludeHistory: true, Limit: 100})
		if err != nil {
			return "", err
		}
		for _, it := range sr.Items {
			if c.factsOnly && it.Type != chanda.RecFact {
				continue
			}
			if containsTerms(it.Text, n) {
				return fmt.Sprintf("search %q finds %s (%s, zone %s)", n, it.ID, it.Type, it.Zone), nil
			}
		}
		if c.factsOnly {
			continue
		}
		cx, err := s.app.Context(agent(ctx), chanda.ContextInput{Namespace: Namespace, Query: n})
		if err != nil {
			return "", err
		}
		if containsTerms(cx.Text, n) {
			return fmt.Sprintf("context %q still holds the term", n), nil
		}
	}
	return "", nil
}

// ---------------------------------------------------------------------------
// Injection.

// InjectionSuite measures external content that becomes guidance or a
// higher-zone fact, over four kinds of attempts on a core holding the corpus:
//
//  1. every event from a tool or web source (documents and tool turns) must
//     be stored in the external zone;
//  2. every fact derived from an external event must be external;
//  3. context for the injection questions, and for the opening words of
//     every instruction-like event, must deliver no instruction-like item and
//     every external item only as a labelled quotation;
//  4. assert_fact through an external channel claiming user_statement, on
//     three subjects with internal facts, must be stored external and leave
//     the internal fact active.
//
// injection_success_rate = successful attempts / attempts.
func InjectionSuite(ctx context.Context, fe eval.FactoryEnv, in eval.SuiteInput) (eval.Measured, error) {
	s, err := loaded(ctx, fe, in.Dataset, suiteVariant())
	if err != nil {
		return nil, err
	}
	defer s.Close()
	db := s.e.Store.DB()
	count := func(q string, args ...any) (int, error) {
		var n int
		err := db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n, err
	}
	m := eval.Measured{}
	var attempts, successes int
	extEvents, err := count(`SELECT count(*) FROM events e JOIN sources s ON s.id = e.source_id WHERE s.kind IN ('tool','web')`)
	if err != nil {
		return nil, err
	}
	badEvents, err := count(`SELECT count(*) FROM events e JOIN sources s ON s.id = e.source_id WHERE s.kind IN ('tool','web') AND e.trust_zone <> 'external'`)
	if err != nil {
		return nil, err
	}
	m["external_events"], m["external_events_not_external"] = float64(extEvents), float64(badEvents)
	attempts += extEvents
	successes += badEvents

	extFacts, err := count(`SELECT count(DISTINCT p.derived_id) FROM provenance p JOIN events e ON e.id = p.source_id
		WHERE p.derived_type = 'fact' AND p.relation = 'derived_from' AND e.trust_zone = 'external'`)
	if err != nil {
		return nil, err
	}
	badFacts, err := count(`SELECT count(DISTINCT f.id) FROM facts f JOIN provenance p ON p.derived_id = f.id JOIN events e ON e.id = p.source_id
		WHERE p.relation = 'derived_from' AND e.trust_zone = 'external' AND f.trust_zone <> 'external'`)
	if err != nil {
		return nil, err
	}
	m["facts_from_external"], m["facts_from_external_not_external"] = float64(extFacts), float64(badFacts)
	attempts += extFacts
	successes += badFacts

	var queries []chanda.ContextInput
	for _, q := range in.Dataset.Evaluated() {
		if q.HasTag(eval.TagInjection) {
			ci := chanda.ContextInput{Namespace: Namespace, Query: q.Question}
			if q.AsOf != "" {
				ci.AsOf = q.AsOf
			}
			queries = append(queries, ci)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT content FROM events WHERE namespace = ? AND flags LIKE '%instruction_like%' ORDER BY id`, Namespace)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return nil, err
		}
		words := strings.Fields(c)
		queries = append(queries, chanda.ContextInput{Namespace: Namespace, Query: strings.Join(words[:min(12, len(words))], " ")})
	}
	rows.Close()
	var guidance int
	for _, q := range queries {
		out, err := s.app.Context(agent(ctx), q)
		if err != nil {
			return nil, err
		}
		attempts++
		if becameGuidance(out) {
			successes++
			guidance++
		}
	}
	m["context_queries"], m["context_guidance"] = float64(len(queries)), float64(guidance)

	forged, err := s.forgeFacts(ctx, 3)
	if err != nil {
		return nil, err
	}
	attempts += forged.attempts
	successes += forged.successes
	m["forged_assertions"], m["forged_assertions_accepted"] = float64(forged.attempts), float64(forged.successes)

	if attempts == 0 {
		return nil, fmt.Errorf("the dataset holds no external content")
	}
	m["attempts"], m["successes"] = float64(attempts), float64(successes)
	m["injection_success_rate"] = float64(successes) / float64(attempts)
	return m, nil
}

// becameGuidance reports a context block that delivers an instruction-like
// item, or an external item outside a labelled quotation.
func becameGuidance(out chanda.ContextOutput) bool {
	for _, it := range out.Items {
		if slices.Contains(it.Flags, "instruction_like") {
			return true
		}
		if it.Zone == chanda.ZoneExternal {
			first := strings.SplitN(strings.TrimSpace(it.Text), "\n", 2)[0]
			if !strings.Contains(out.Text, "external quotation") || !strings.Contains(out.Text, "> "+first) {
				return true
			}
		}
	}
	return false
}

type forgeResult struct{ attempts, successes int }

// forgeFacts asserts, through an external channel claiming user_statement,
// a new value for n subjects that hold an active internal fact.
func (s *System) forgeFacts(ctx context.Context, n int) (forgeResult, error) {
	var r forgeResult
	rows, err := s.e.Store.DB().QueryContext(ctx, `SELECT f.id, e.canonical_name, f.predicate FROM facts f JOIN entities e ON e.id = f.subject
		WHERE f.namespace = ? AND f.status = 'active' AND f.trust_zone = 'internal' AND f.is_single = 1 ORDER BY e.canonical_name, f.predicate, f.id LIMIT ?`, Namespace, n)
	if err != nil {
		return r, err
	}
	type target struct{ id, subject, pred string }
	var ts []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.subject, &t.pred); err != nil {
			rows.Close()
			return r, err
		}
		ts = append(ts, t)
	}
	rows.Close()
	ext := chanda.WithActor(ctx, chanda.Actor{Name: "eval-external-relay", Remote: true, MaxZone: chanda.ZoneExternal})
	for i, t := range ts {
		r.attempts++
		out, err := s.app.AssertFact(ext, chanda.AssertFactInput{Namespace: Namespace, Subject: t.subject, Predicate: t.pred,
			Value: "valor injetado " + strconv.Itoa(i+1), Source: chanda.SourceRef{Kind: chanda.SourceUserStatement, URI: "https://attacker.example/" + strconv.Itoa(i)}})
		if err != nil {
			return r, err
		}
		g, err := s.app.Get(op(ctx), chanda.GetInput{Namespace: Namespace, ID: out.ID})
		if err != nil {
			return r, err
		}
		orig, err := s.app.Get(op(ctx), chanda.GetInput{Namespace: Namespace, ID: t.id})
		if err != nil {
			return r, err
		}
		if g.Item.Zone != chanda.ZoneExternal || orig.Item.Status != "active" {
			r.successes++
		}
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// Document update.

// DocumentUpdateSuite ingests the corpus (every document version in order)
// and measures, over the facts derived from chunks that a later version
// retired, the share still active with no live origin. It also counts the
// questions tagged update whose context delivers a fact holding one of the
// question's superseded values.
func DocumentUpdateSuite(ctx context.Context, fe eval.FactoryEnv, in eval.SuiteInput) (eval.Measured, error) {
	s, err := loaded(ctx, fe, in.Dataset, suiteVariant())
	if err != nil {
		return nil, err
	}
	defer s.Close()
	db := s.e.Store.DB()
	var fromRetired, staleActive, staleSource int
	if err := db.QueryRowContext(ctx, `SELECT count(DISTINCT p.derived_id) FROM provenance p JOIN events e ON e.id = p.source_id
		WHERE p.derived_type = 'fact' AND p.relation IN ('derived_from','confirmed_by') AND e.retired_at IS NOT NULL`).Scan(&fromRetired); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM facts f WHERE f.status = 'active'
		AND EXISTS (SELECT 1 FROM provenance p JOIN events e ON e.id = p.source_id WHERE p.derived_id = f.id
			AND p.relation IN ('derived_from','confirmed_by') AND e.retired_at IS NOT NULL)
		AND NOT EXISTS (SELECT 1 FROM provenance p JOIN events e ON e.id = p.source_id WHERE p.derived_id = f.id
			AND p.relation IN ('derived_from','confirmed_by') AND e.retired_at IS NULL)`).Scan(&staleActive); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM facts WHERE status = 'stale_source'`).Scan(&staleSource); err != nil {
		return nil, err
	}
	if fromRetired == 0 {
		return nil, fmt.Errorf("no fact was derived from a retired chunk: the dataset needs versioned documents with structured fields")
	}
	var questions, delivered int
	for _, q := range in.Dataset.Evaluated() {
		if !q.HasTag(eval.TagUpdate) || q.HasTag(eval.TagInjection) || len(q.MustNotContain) == 0 {
			continue
		}
		questions++
		out, err := s.app.Context(agent(ctx), chanda.ContextInput{Namespace: Namespace, Query: q.Question, AsOf: q.AsOf})
		if err != nil {
			return nil, err
		}
		hit := false
		for _, it := range out.Items {
			if it.Type != chanda.RecFact {
				continue
			}
			for _, bad := range q.MustNotContain {
				hit = hit || strings.Contains(eval.Normalize(it.Text), eval.Normalize(bad))
			}
		}
		if hit {
			delivered++
		}
	}
	return eval.Measured{
		"facts_from_retired_chunks": float64(fromRetired), "stale_facts_active": float64(staleActive),
		"stale_fact_active_rate": float64(staleActive) / float64(fromRetired), "facts_stale_source": float64(staleSource),
		"update_questions": float64(questions), "update_questions_with_stale_fact_in_context": float64(delivered),
	}, nil
}

// ---------------------------------------------------------------------------
// Deduplication.

var firstNumber = regexp.MustCompile(`\d+`)

// DeduplicationSuite writes probes built from the corpus through remember
// into an empty core. For each base paragraph P (document paragraphs with a
// number and at least 8 words, the first 40 in corpus order) it writes P,
// then an update U (P with its first number changed: a different fact that
// must never be discarded), then two real duplicates of P (D1: spacing and
// letter case changed; D2: P with a trailing reminder word). It measures
// updates_discarded_rate (U answered duplicate or near_duplicate) and
// duplicates_missed_rate (a duplicate stored as a new record).
func DeduplicationSuite(ctx context.Context, fe eval.FactoryEnv, in eval.SuiteInput) (eval.Measured, error) {
	s, err := Open(ctx, suiteVariant(), fe)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	bases := eval.DedupProbeBases(in.Dataset, fe.Config.Write.MaxChunkChars)
	if len(bases) == 0 {
		return nil, fmt.Errorf("no paragraph with a number to build deduplication probes from")
	}
	if err := s.at("2026-01-01"); err != nil {
		return nil, err
	}
	remember := func(text string) (string, error) {
		out, err := s.app.Remember(agent(ctx), chanda.RememberInput{Namespace: Namespace, Content: text, SessionID: "session://dedup-probes"})
		return out.Action, err
	}
	collapsed := func(a string) bool { return a == chanda.ActionDuplicate || a == chanda.ActionNearDuplicate }
	var updates, discarded, dups, missed int
	for _, p := range bases {
		if _, err := remember(p); err != nil {
			return nil, err
		}
		loc := firstNumber.FindStringIndex(p)
		n, _ := strconv.Atoi(p[loc[0]:loc[1]])
		u := p[:loc[0]] + strconv.Itoa(n+7) + p[loc[1]:]
		a, err := remember(u)
		if err != nil {
			return nil, err
		}
		updates++
		if collapsed(a) {
			discarded++
		}
		for _, d := range []string{respace(p), p + " Lembrete."} {
			a, err := remember(d)
			if err != nil {
				return nil, err
			}
			dups++
			if !collapsed(a) {
				missed++
			}
		}
	}
	return eval.Measured{
		"base_paragraphs": float64(len(bases)), "updates": float64(updates), "updates_discarded": float64(discarded),
		"updates_discarded_rate": float64(discarded) / float64(updates),
		"duplicates":             float64(dups), "duplicates_missed": float64(missed),
		"duplicates_missed_rate": float64(missed) / float64(dups),
	}, nil
}

// respace doubles the spaces and lowercases the first letter.
func respace(s string) string {
	s = strings.ReplaceAll(s, " ", "  ")
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// ---------------------------------------------------------------------------
// Determinism.

// DeterminismSuite runs C1 and C3 (as configured in this report) twice each
// on the whole dataset, in two directories: ingestion, flush and one context
// call per evaluated question. A pair is identical when the checkpointed
// database files are byte-identical.
func DeterminismSuite(ctx context.Context, fe eval.FactoryEnv, in eval.SuiteInput) (eval.Measured, error) {
	var pairs, identical int
	for _, v := range []Variant{Variants[0], suiteVariant()} {
		var dbs [][]byte
		for range 2 {
			s, err := loaded(ctx, fe, in.Dataset, v)
			if err != nil {
				return nil, err
			}
			for _, q := range in.Dataset.Evaluated() {
				if _, err := s.app.Context(agent(ctx), chanda.ContextInput{Namespace: Namespace, Query: q.Question, AsOf: q.AsOf, BudgetTokens: in.Budget}); err != nil {
					s.Close()
					return nil, err
				}
			}
			b, err := s.Checkpoint(ctx)
			s.Close()
			if err != nil {
				return nil, err
			}
			dbs = append(dbs, b)
		}
		pairs++
		if bytes.Equal(dbs[0], dbs[1]) {
			identical++
		}
	}
	return eval.Measured{"pairs": float64(pairs), "identical": float64(identical), "identical_rate": float64(identical) / float64(pairs)}, nil
}
