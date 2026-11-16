package lme

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// ---------------------------------------------------------------------------
// Sleep.

// SleepRunRow is the sleep_runs row of a run, read from the closed database.
type SleepRunRow struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	StartedAt string          `json:"started_at"`
	EndedAt   string          `json:"ended_at"`
	Phases    json.RawMessage `json:"phases"`
	Budget    json.RawMessage `json:"budget"`
	Inputs    json.RawMessage `json:"inputs"`
	Outputs   json.RawMessage `json:"outputs"` // {"counts": ..., "gate": ...}; the guided sleep adds its counts there
	Cost      json.RawMessage `json:"cost"`
	Report    string          `json:"report"`
}

// FichaItem is one page item.
type FichaItem struct {
	ID         string   `json:"id"`
	Ordinal    int      `json:"ordinal"`
	Status     string   `json:"status"`
	Zone       string   `json:"zone"`
	SleepRunID string   `json:"sleep_run_id,omitempty"`
	Text       string   `json:"text"`
	Cites      []string `json:"cites"` // origin event ids (provenance)
}

// Ficha is one page with its items (any kind; guided digests are summary
// pages titled "Digest: ...").
type Ficha struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Title      string      `json:"title"`
	Status     string      `json:"status"`
	Zone       string      `json:"zone"`
	SleepRunID string      `json:"sleep_run_id,omitempty"`
	Body       string      `json:"body"`
	Items      []FichaItem `json:"items"`
}

// StudySleepResult is the saved result of a sleep step.
type StudySleepResult struct {
	DB         string             `json:"db"`
	Condition  string             `json:"condition"`
	Phases     []int              `json:"phases"`
	Budget     chanda.SleepBudget `json:"budget"`
	LLMURL     string             `json:"llm_url,omitempty"`
	ChandaSet  []string           `json:"chanda_set,omitempty"`
	ConfigHash string             `json:"chanda_config_hash"`
	StartedAt  string             `json:"started_at"`
	WallMS     float64            `json:"wall_ms"`
	Attempts   int                `json:"attempts"`
	// Output is the sleep_run data exactly as returned over MCP.
	Output      json.RawMessage `json:"output"`
	Run         *SleepRunRow    `json:"run"`
	Counts      json.RawMessage `json:"counts,omitempty"` // Run.Outputs.counts
	StatsBefore chanda.Counts   `json:"stats_before"`
	StatsAfter  chanda.Counts   `json:"stats_after"`
	Fichas      []Ficha         `json:"fichas"`
	// Phase2Skipped is the notice Chanda wrote when phase 2 was requested
	// but did not run (no LLM, no evaluator).
	Phase2Skipped string `json:"phase2_skipped,omitempty"`
}

var rePhase2Skipped = regexp.MustCompile(`(?m)^- (phase 2 skipped[^\n]*)$`)

// StudySleep runs sleep_run on the database in dir with the step's
// configuration (o.Chanda.Set, LLM endpoint), waits out Chanda's write
// burst as condition E does, flushes the index, closes the server and reads
// the run row and every page back read-only.
func StudySleep(ctx context.Context, o *StudyOptions, dir, cond string, phases []int, budget chanda.SleepBudget) (*StudySleepResult, error) {
	m, err := ReadStudyMeta(dir)
	if err != nil {
		return nil, err
	}
	srv, h, err := o.start(ctx, dir, m, true)
	if err != nil {
		return nil, err
	}
	defer srv.Close()
	res := &StudySleepResult{DB: dir, Condition: cond, Phases: phases, Budget: budget, LLMURL: o.Chanda.LLMURL,
		ChandaSet: OverrideStrings(o.Chanda.Set), ConfigHash: h, Fichas: []Ficha{}}
	var st chanda.StatsOutput
	if err := srv.Call(ctx, chanda.ToolStats, chanda.StatsInput{}, &st); err != nil {
		return nil, err
	}
	res.StatsBefore = st.Counts
	var raw json.RawMessage
	var start time.Time
	for attempt := 1; ; attempt++ {
		res.Attempts = attempt
		start = time.Now()
		err := srv.Call(ctx, chanda.ToolSleepRun, chanda.SleepRunInput{Phases: phases, Budget: budget}, &raw)
		if err == nil {
			break
		}
		if attempt >= 4 || !strings.Contains(err.Error(), "write burst") {
			return nil, err
		}
		o.logf("sleep: write burst, waiting %s", SleepBurstWait)
		if err := waitCtx(ctx, SleepBurstWait); err != nil {
			return nil, err
		}
	}
	res.WallMS = ms(time.Since(start))
	res.StartedAt = start.UTC().Format(time.RFC3339)
	res.Output = raw
	var out chanda.SleepRunOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("sleep_run output: %w", err)
	}
	var f chanda.FlushOutput
	if err := srv.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
		return nil, err
	}
	if err := srv.Call(ctx, chanda.ToolStats, chanda.StatsInput{}, &st); err != nil {
		return nil, err
	}
	res.StatsAfter = st.Counts
	if err := srv.Close(); err != nil {
		return nil, err
	}
	if mm := rePhase2Skipped.FindStringSubmatch(out.Report); mm != nil {
		res.Phase2Skipped = mm[1]
	}
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if res.Run, err = readSleepRun(ctx, db, out.RunID); err != nil {
		return nil, err
	}
	var outputs struct {
		Counts json.RawMessage `json:"counts"`
	}
	if json.Unmarshal(res.Run.Outputs, &outputs) == nil {
		res.Counts = outputs.Counts
	}
	if res.Fichas, err = ReadFichas(ctx, db); err != nil {
		return nil, err
	}
	m.addStep(StudyStep{Step: "sleep", Set: OverrideStrings(o.Chanda.Set), ConfigHash: h,
		Detail: fmt.Sprintf("%s: run %s %s, phases %v, %d LLM calls, %d+%d tokens", cond, out.RunID, out.Status, out.Phases,
			out.Cost.LLMCalls, out.Cost.InputTokens, out.Cost.OutputTokens)})
	return res, m.Write(dir)
}

func readSleepRun(ctx context.Context, db *sql.DB, id string) (*SleepRunRow, error) {
	r := &SleepRunRow{}
	var ended sql.NullString
	var phases, budget, inputs, outputs, cost string
	err := db.QueryRowContext(ctx, `SELECT id, status, started_at, ended_at, phases, budget, inputs, outputs, cost, report FROM sleep_runs WHERE id = ?`, id).
		Scan(&r.ID, &r.Status, &r.StartedAt, &ended, &phases, &budget, &inputs, &outputs, &cost, &r.Report)
	if err != nil {
		return nil, fmt.Errorf("sleep_runs %s: %w", id, err)
	}
	r.EndedAt = ended.String
	r.Phases, r.Budget, r.Inputs, r.Outputs, r.Cost = rawJSON(phases), rawJSON(budget), rawJSON(inputs), rawJSON(outputs), rawJSON(cost)
	return r, nil
}

func rawJSON(s string) json.RawMessage {
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	b, _ := json.Marshal(s)
	return b
}

// ReadFichas reads every page and its items from an open (read-only)
// database, pages by creation time.
func ReadFichas(ctx context.Context, db *sql.DB) ([]Ficha, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, kind, title, status, trust_zone, COALESCE(sleep_run_id, ''), body FROM pages ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	out := []Ficha{}
	for rows.Next() {
		var p Ficha
		if err := rows.Scan(&p.ID, &p.Kind, &p.Title, &p.Status, &p.Zone, &p.SleepRunID, &p.Body); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := db.QueryContext(ctx, `SELECT id, ordinal, status, trust_zone, COALESCE(sleep_run_id, ''), text FROM page_items WHERE page_id = ? ORDER BY ordinal, id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Items = []FichaItem{}
		for items.Next() {
			var it FichaItem
			if err := items.Scan(&it.ID, &it.Ordinal, &it.Status, &it.Zone, &it.SleepRunID, &it.Text); err != nil {
				items.Close()
				return nil, err
			}
			out[i].Items = append(out[i].Items, it)
		}
		if err := items.Close(); err != nil {
			return nil, err
		}
		for j := range out[i].Items {
			it := &out[i].Items[j]
			it.Cites = []string{}
			cs, err := db.QueryContext(ctx, `SELECT source_id FROM provenance WHERE derived_id = ? AND source_type = 'event' ORDER BY source_id`, it.ID)
			if err != nil {
				return nil, err
			}
			for cs.Next() {
				var s string
				if err := cs.Scan(&s); err != nil {
					cs.Close()
					return nil, err
				}
				it.Cites = append(it.Cites, s)
			}
			if err := cs.Close(); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// StudyFichas reads the pages of a closed study database.
func StudyFichas(ctx context.Context, dir string) ([]Ficha, error) {
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return ReadFichas(ctx, db)
}

// ---------------------------------------------------------------------------
// Plain RAG (condition B) on a LoCoMo conversation.

// StudyRAG builds one plain-RAG index for the conversation (the battery's
// B: 1,000-rune chunks per session, passages embedded once, exact cosine,
// the top 50 kept greedily within the budget, the official history layout
// with the session date) and answers each question from it.
func StudyRAG(ctx context.Context, emb *Cached, conv *LocomoConversation, qs []StudyQuestion, budget int, cond string) ([]*StudyAskLine, error) {
	base := conv.AsQuestion("", "", nil)
	var chunks []Chunk
	for i, s := range base.HaystackSessions {
		chunks = append(chunks, ChunkSession(i, s, ChunkChars)...)
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	vecs, err := emb.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	var out []*StudyAskLine
	for _, q := range qs {
		start := time.Now()
		qq := conv.AsQuestion(q.ID, q.Question, q.Evidence.Valid)
		qv, err := emb.EmbedQuery(ctx, []string{q.Question})
		if err != nil {
			return nil, err
		}
		scores := make([]float64, len(vecs))
		order := make([]int, len(vecs))
		for i, v := range vecs {
			order[i], scores[i] = i, dot(qv[0], v)
		}
		sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })
		sel, text := FillBudget(qq, chunks, order[:min(RAGTopK, len(order))], budget)
		l := &StudyAskLine{QuestionID: q.ID, Condition: cond, Context: text, ContextTokens: Tokens(text), Source: SourceLocomo,
			SampleID: conv.SampleID, Index: q.Index, Half: q.Half, Question: q.Question, Gold: q.Gold, Category: q.Category,
			CategoryName: q.CategoryName, SpeakerA: conv.SpeakerA, SpeakerB: conv.SpeakerB, Items: []StudyItem{}}
		l.RetrievalMS = ms(time.Since(start))
		for _, ci := range renderOrder(qq, chunks, sel) {
			c := chunks[ci]
			for _, p := range c.Pieces {
				l.Items = append(l.Items, StudyItem{ID: fmt.Sprintf("chunk%d.%d", c.Session, c.Ordinal), Type: "rag_piece",
					Key: conv.Sessions[c.Session].Turns[p.Turn].DiaID, Tokens: Tokens(p.Text), Truncated: !p.Whole, Text: p.Text})
			}
		}
		l.Evidence.Raw, l.Evidence.Invalid = q.EvidenceRaw, q.Evidence.Invalid
		for _, e := range MatchEvidenceRAG(qq, chunks, sel) {
			l.Evidence.Turns = append(l.Evidence.Turns, StudyEvidenceTurn{Key: conv.Sessions[e.Session].Turns[e.Turn].DiaID,
				Coverage: e.Coverage, Reached: e.Reached, By: e.By})
		}
		if l.Evidence.Turns == nil {
			l.Evidence.Turns = []StudyEvidenceTurn{}
		}
		l.Evidence.summarise()
		out = append(out, l)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Leak check (PROTOCOL.md "Stops"): the sleep's inputs must hold no trace
// of gold answers, judge text or test questions.

// LeakReport is the result of StudyLeakCheck.
type LeakReport struct {
	DB          string   `json:"db"`
	Outcomes    int      `json:"outcomes"`
	BadOutcomes []string `json:"bad_outcomes"` // value not +-1, or a used id that is not a record id
	Retrievals  int      `json:"retrievals"`
	// TestQuestionsInRetrievals: forbidden questions (the test half, when
	// checked before the test run) found as a retrieval's query text.
	TestQuestionsInRetrievals []string `json:"test_questions_in_retrievals"`
	DerivedTexts              int      `json:"derived_texts"` // facts, pages and page items checked
	// GoldHits: a gold answer found in a derived text. Expected when the
	// gold is literal in an origin event (extractive fichas copy turns);
	// Unexplained lists the hits whose origins do not contain it.
	GoldHits    int      `json:"gold_hits"`
	Unexplained []string `json:"unexplained_gold_hits"`
	JudgeWords  []string `json:"judge_words"` // derived texts with CORRECT/WRONG/"Gold answer"
	OK          bool     `json:"ok"`
}

var reRecordID = regexp.MustCompile(`^(evt|fct|pg|pgi)_[0-9A-Za-z]+$`)
var reJudgeWords = regexp.MustCompile(`\b(CORRECT|WRONG)\b|(?i)gold answer|generated answer`)

func normLeak(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// StudyLeakCheck inspects a closed study database. golds are the gold
// answers of every question of the source; forbidden are question texts
// that must not appear as retrieval queries (the test half before its run).
func StudyLeakCheck(ctx context.Context, dir string, golds, forbidden []string) (*LeakReport, error) {
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	r := &LeakReport{DB: dir, BadOutcomes: []string{}, TestQuestionsInRetrievals: []string{}, Unexplained: []string{}, JudgeWords: []string{}}
	rows, err := db.QueryContext(ctx, `SELECT id, value, used_ids FROM outcomes`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, used string
		var v float64
		if err := rows.Scan(&id, &v, &used); err != nil {
			rows.Close()
			return nil, err
		}
		r.Outcomes++
		var ids []string
		bad := v != 1 && v != -1
		if json.Unmarshal([]byte(used), &ids) != nil {
			bad = true
		}
		for _, u := range ids {
			if !reRecordID.MatchString(u) {
				bad = true
			}
		}
		if bad {
			r.BadOutcomes = append(r.BadOutcomes, id)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	forbid := map[string]bool{}
	for _, q := range forbidden {
		forbid[normLeak(q)] = true
	}
	rows, err = db.QueryContext(ctx, `SELECT query_text FROM retrievals`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			rows.Close()
			return nil, err
		}
		r.Retrievals++
		if forbid[normLeak(q)] {
			r.TestQuestionsInRetrievals = append(r.TestQuestionsInRetrievals, q)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	type derived struct{ id, text string }
	var ds []derived
	for _, qy := range []string{
		`SELECT id, title || ' ' || body FROM pages`,
		`SELECT id, text FROM page_items`,
		`SELECT id, predicate || ' ' || value FROM facts`,
	} {
		rows, err := db.QueryContext(ctx, qy)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var d derived
			if err := rows.Scan(&d.id, &d.text); err != nil {
				rows.Close()
				return nil, err
			}
			ds = append(ds, d)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	r.DerivedTexts = len(ds)
	var gs []string
	for _, g := range golds {
		if g = normLeak(g); len([]rune(g)) >= 4 {
			gs = append(gs, g)
		}
	}
	for _, d := range ds {
		t := normLeak(d.text)
		if reJudgeWords.MatchString(d.text) {
			r.JudgeWords = append(r.JudgeWords, d.id)
		}
		for _, g := range gs {
			if !strings.Contains(t, g) {
				continue
			}
			r.GoldHits++
			ok, err := originsContain(ctx, db, d.id, g)
			if err != nil {
				return nil, err
			}
			if !ok {
				r.Unexplained = append(r.Unexplained, d.id+": "+g)
			}
		}
	}
	r.OK = len(r.BadOutcomes) == 0 && len(r.TestQuestionsInRetrievals) == 0 && len(r.Unexplained) == 0 && len(r.JudgeWords) == 0
	return r, nil
}

// originsContain reports whether an origin event of a derived record (its
// own provenance edges, and for a page those of its items) contains g.
func originsContain(ctx context.Context, db *sql.DB, id, g string) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT e.content FROM provenance p JOIN events e ON e.id = p.source_id
		WHERE p.source_type = 'event' AND (p.derived_id = ? OR p.derived_id IN (SELECT id FROM page_items WHERE page_id = ?))`, id, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return false, err
		}
		if strings.Contains(normLeak(c), g) {
			return true, nil
		}
	}
	return false, rows.Err()
}
