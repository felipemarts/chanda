package lme

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// The sleep study (eval/sleep/PROTOCOL.md, eval/sleep/README.md): steps on a
// study directory whose Chanda database persists between them. Each step
// starts its own "lme chanda serve" on the directory, does its calls and
// closes the server, so the SQLite file is quiescent between steps and can
// be copied.
//
// Gold answers stay in the harness: they are read from the dataset into the
// ask JSONL for the judge and never sent to Chanda. Only the outcome sign
// and the used ids reach the database (StudyOutcome).

// StudyMetaFile is the harness's own file in a study directory.
const StudyMetaFile = "study.json"

// Study sources.
const (
	SourceLocomo      = "locomo"
	SourceLongMemEval = "longmemeval"
)

// StudyTurn maps one dataset turn to the event log_session returned.
type StudyTurn struct {
	Key       string `json:"key"`     // LoCoMo dia_id, or LongMemEval "s<session>t<turn>"
	Session   int    `json:"session"` // LoCoMo: session index (0-based); LongMemEval: haystack index
	Turn      int    `json:"turn"`    // index in the session
	SessionID string `json:"session_id"`
	EventID   string `json:"event_id"` // "" when the turn was rejected
	Action    string `json:"action,omitempty"`
}

// StudyStep is one step applied to a study directory.
type StudyStep struct {
	Step       string   `json:"step"`
	At         string   `json:"at"`
	Set        []string `json:"set,omitempty"`
	ConfigHash string   `json:"config_hash,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}

// StudyMeta is study.json: where the database came from and the turn map.
// It holds no gold answer and no question text.
type StudyMeta struct {
	Source        string         `json:"source"`
	Dataset       string         `json:"dataset"`
	DatasetSHA256 string         `json:"dataset_sha256"`
	SampleID      string         `json:"sample_id,omitempty"`
	QuestionID    string         `json:"question_id,omitempty"`
	SpeakerA      string         `json:"speaker_a,omitempty"`
	SpeakerB      string         `json:"speaker_b,omitempty"`
	Embedder      string         `json:"embedder"`
	EmbedModel    string         `json:"embed_model"`
	EmbedDim      int            `json:"embed_dim"`
	Sessions      int            `json:"sessions"`
	Turns         []StudyTurn    `json:"turns"`
	Ingest        map[string]any `json:"ingest,omitempty"`
	CopiedFrom    string         `json:"copied_from,omitempty"`
	Steps         []StudyStep    `json:"steps"`
}

// ReadStudyMeta reads dir/study.json.
func ReadStudyMeta(dir string) (*StudyMeta, error) {
	b, err := os.ReadFile(filepath.Join(dir, StudyMetaFile))
	if err != nil {
		return nil, fmt.Errorf("%s is not a study directory (run lme study ingest): %w", dir, err)
	}
	m := &StudyMeta{}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", dir, StudyMetaFile, err)
	}
	return m, nil
}

// Write writes dir/study.json atomically.
func (m *StudyMeta) Write(dir string) error {
	b, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, StudyMetaFile+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, StudyMetaFile))
}

func (m *StudyMeta) addStep(s StudyStep) {
	s.At = time.Now().UTC().Format(time.RFC3339)
	m.Steps = append(m.Steps, s)
}

// EventKeys maps event ids to their turn keys (first turn for a duplicate).
func (m *StudyMeta) EventKeys() map[string]string {
	out := map[string]string{}
	for _, t := range m.Turns {
		if t.EventID != "" {
			if _, ok := out[t.EventID]; !ok {
				out[t.EventID] = t.Key
			}
		}
	}
	return out
}

// KeyEvents maps turn keys to event ids.
func (m *StudyMeta) KeyEvents() map[string]string {
	out := map[string]string{}
	for _, t := range m.Turns {
		out[t.Key] = t.EventID
	}
	return out
}

// LMETurnKey names a LongMemEval haystack turn.
func LMETurnKey(session, turn int) string { return fmt.Sprintf("s%dt%d", session, turn) }

// StudyOptions configures the study steps.
type StudyOptions struct {
	Chanda ChandaOptions // Exe/Args/Embed/Budget/Set, and the LLM endpoint for sleep
	Budget int           // context budget (tokens)
	Log    func(format string, a ...any)
}

func (o *StudyOptions) logf(format string, a ...any) {
	if o.Log != nil {
		o.Log(format, a...)
	}
}

// StudyConfig is the chanda.yaml a step runs with, and its hash.
func (o *StudyOptions) StudyConfig(dir string, m *StudyMeta, admin bool) (ChandaOptions, string, error) {
	co := o.Chanda
	co.Admin = admin
	co.AppendLog = true
	if co.Budget == 0 {
		co.Budget = o.Budget
	}
	text, err := co.Config(dir, m.EmbedModel, m.EmbedDim)
	if err != nil {
		return co, "", err
	}
	h, err := CheckChandaConfig(text)
	if err != nil {
		return co, "", fmt.Errorf("chanda configuration (overrides %v): %w", OverrideStrings(co.Set), err)
	}
	return co, h, nil
}

func (o *StudyOptions) start(ctx context.Context, dir string, m *StudyMeta, admin bool) (*ChandaServer, string, error) {
	co, h, err := o.StudyConfig(dir, m, admin)
	if err != nil {
		return nil, "", err
	}
	srv, err := StartChanda(ctx, co, dir, m.EmbedModel, m.EmbedDim)
	return srv, h, err
}

func dbExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "chanda.db"))
	return err == nil
}

// ---------------------------------------------------------------------------
// Ingest.

// StudyIngestLocomo logs every session of conv into a new database in dir
// (PROTOCOL.md "Ingestion"): one log_session per session, id
// <sample_id>-s<n>, turns in order, speaker_a as user and speaker_b as
// assistant, content LocomoContent, ts the session date_time. m carries
// the dataset and embedder fields; the turn map is filled in.
func StudyIngestLocomo(ctx context.Context, o *StudyOptions, dir string, conv *LocomoConversation, m *StudyMeta) error {
	if dbExists(dir) {
		return fmt.Errorf("%s already holds a database", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m.Source, m.SampleID, m.SpeakerA, m.SpeakerB, m.Sessions = SourceLocomo, conv.SampleID, conv.SpeakerA, conv.SpeakerB, len(conv.Sessions)
	srv, h, err := o.start(ctx, dir, m, false)
	if err != nil {
		return err
	}
	defer srv.Close()
	start := time.Now()
	counts := map[string]int{}
	for i, s := range conv.Sessions {
		ts := s.Date.Format(time.RFC3339)
		turns := make([]chanda.Turn, len(s.Turns))
		for j, t := range s.Turns {
			turns[j] = chanda.Turn{Role: conv.Role(t), Content: LocomoContent(t), TS: ts}
		}
		var out chanda.LogSessionOutput
		if err := srv.Call(ctx, chanda.ToolLogSession, chanda.LogSessionInput{SessionID: conv.SessionID(i), Turns: turns}, &out); err != nil {
			return fmt.Errorf("session %d: %w", s.N, err)
		}
		if len(out.Turns) != len(turns) {
			return fmt.Errorf("session %d: %d turn results for %d turns", s.N, len(out.Turns), len(turns))
		}
		for _, tr := range out.Turns {
			if tr.Ordinal < 0 || tr.Ordinal >= len(s.Turns) {
				return fmt.Errorf("session %d: ordinal %d out of range", s.N, tr.Ordinal)
			}
			counts[tr.Action]++
			m.Turns = append(m.Turns, StudyTurn{Key: s.Turns[tr.Ordinal].DiaID, Session: i, Turn: tr.Ordinal,
				SessionID: conv.SessionID(i), EventID: tr.ID, Action: tr.Action})
		}
	}
	var f chanda.FlushOutput
	if err := srv.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
		return err
	}
	if !f.Complete {
		return fmt.Errorf("flush incomplete: %+v", f)
	}
	m.Ingest = map[string]any{"turns": len(m.Turns), "actions": counts, "seconds": time.Since(start).Seconds(),
		"ignored_date_keys": conv.IgnoredDateKeys}
	m.addStep(StudyStep{Step: "ingest", Set: OverrideStrings(o.Chanda.Set), ConfigHash: h,
		Detail: fmt.Sprintf("%s: %d sessions, %d turns", conv.SampleID, len(conv.Sessions), len(m.Turns))})
	if err := srv.Close(); err != nil {
		return err
	}
	return m.Write(dir)
}

// StudyIngestLME logs one LongMemEval question's haystack exactly as
// condition D does (ChandaServer.IngestQuestion).
func StudyIngestLME(ctx context.Context, o *StudyOptions, dir string, q *Question, m *StudyMeta) error {
	if dbExists(dir) {
		return fmt.Errorf("%s already holds a database", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m.Source, m.QuestionID, m.Sessions = SourceLongMemEval, q.QuestionID, len(q.HaystackSessions)
	srv, h, err := o.start(ctx, dir, m, false)
	if err != nil {
		return err
	}
	defer srv.Close()
	in, err := srv.IngestQuestion(ctx, q)
	if err != nil {
		return err
	}
	keys := make([]TurnKey, 0, len(in.TurnEvent))
	for k := range in.TurnEvent {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].Session != keys[b].Session {
			return keys[a].Session < keys[b].Session
		}
		return keys[a].Turn < keys[b].Turn
	})
	for _, k := range keys {
		m.Turns = append(m.Turns, StudyTurn{Key: LMETurnKey(k.Session, k.Turn), Session: k.Session, Turn: k.Turn,
			SessionID: ChandaSessionID(k.Session), EventID: in.TurnEvent[k]})
	}
	m.Ingest = map[string]any{"turns": in.Turns, "created": in.Created, "duplicates": in.Duplicates, "near_duplicates": in.NearDups,
		"rejected": in.Rejected, "reject_reasons": in.RejectReasons, "seconds": in.Elapsed.Seconds()}
	m.addStep(StudyStep{Step: "ingest", Set: OverrideStrings(o.Chanda.Set), ConfigHash: h,
		Detail: fmt.Sprintf("%s: %d sessions, %d turns", q.QuestionID, len(q.HaystackSessions), in.Turns)})
	if err := srv.Close(); err != nil {
		return err
	}
	return m.Write(dir)
}

// ---------------------------------------------------------------------------
// Questions.

// StudyQuestion is one question asked in the study.
type StudyQuestion struct {
	ID           string
	Index        int // LoCoMo QA index
	Half         string
	Question     string
	Gold         string // harness only
	Category     int
	CategoryName string
	QuestionType string
	QuestionDate string
	Evidence     EvidenceParse
	EvidenceRaw  []string
	lme          *Question
}

// LocomoSplit is eval/sleep/split-locomo.json.
type LocomoSplit struct {
	Dataset       string `json:"dataset"`
	Revision      string `json:"revision"`
	SHA256        string `json:"sha256"`
	Seed          int    `json:"seed"`
	Conversations []struct {
		SampleID  string `json:"sample_id"`
		Questions []struct {
			Index    int    `json:"index"`
			Category int    `json:"category"`
			Half     string `json:"half"`
		} `json:"questions"`
	} `json:"conversations"`
}

// ReadLocomoSplit reads the split file.
func ReadLocomoSplit(path string) (*LocomoSplit, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &LocomoSplit{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Halves of the split.
const (
	HalfUse  = "use"
	HalfTest = "test"
	HalfAll  = "all"
)

// LocomoStudyQuestions returns the split's questions of conv in split
// order (the file's order), keeping those of half (use, test or all). The
// split's category must match the dataset's.
func LocomoStudyQuestions(conv *LocomoConversation, split *LocomoSplit, half string) ([]StudyQuestion, error) {
	if half != HalfUse && half != HalfTest && half != HalfAll {
		return nil, fmt.Errorf("half %q: want use, test or all", half)
	}
	for _, c := range split.Conversations {
		if c.SampleID != conv.SampleID {
			continue
		}
		var out []StudyQuestion
		for _, sq := range c.Questions {
			if sq.Index < 0 || sq.Index >= len(conv.QA) {
				return nil, fmt.Errorf("split: %s index %d out of range", conv.SampleID, sq.Index)
			}
			qa := conv.QA[sq.Index]
			if qa.Category != sq.Category {
				return nil, fmt.Errorf("split: %s index %d category %d, dataset says %d", conv.SampleID, sq.Index, sq.Category, qa.Category)
			}
			if half != HalfAll && sq.Half != half {
				continue
			}
			out = append(out, StudyQuestion{
				ID: LocomoQuestionID(conv.SampleID, sq.Index), Index: sq.Index, Half: sq.Half, Question: qa.Question,
				Gold: qa.AnswerText(), Category: qa.Category, CategoryName: LocomoCategoryNames[qa.Category],
				Evidence: ParseEvidence(qa.Evidence, conv.HasDia), EvidenceRaw: qa.Evidence,
			})
		}
		return out, nil
	}
	return nil, fmt.Errorf("split has no conversation %s", conv.SampleID)
}

// LMEStudyQuestion is the bench question of a LongMemEval study directory.
func LMEStudyQuestion(q *Question) StudyQuestion {
	return StudyQuestion{ID: q.QuestionID, Half: HalfAll, Question: q.Question, Gold: q.AnswerText(),
		QuestionType: q.QuestionType, QuestionDate: q.QuestionDate, lme: q}
}

// ---------------------------------------------------------------------------
// Ask.

// StudyItem is one delivered item (Chanda) or turn piece (plain RAG).
type StudyItem struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Key            string   `json:"key,omitempty"` // turn key of an event item (LoCoMo dia_id), or of a RAG piece
	Tokens         int      `json:"tokens"`        // ceil(runes/4) of the item as rendered
	Truncated      bool     `json:"truncated,omitempty"`
	OriginEventIDs []string `json:"origin_event_ids,omitempty"` // facts and pages
	Text           string   `json:"text"`                       // as delivered
	// KeyRank is features.key_rank of the item in the retrieval log: the
	// best rank of a learned query key that points to it (decision 0145);
	// 0 when no key reached it or keys are off.
	KeyRank int `json:"key_rank,omitempty"`
}

// StudyEvidenceTurn is one evidence turn of a question and how it fared.
type StudyEvidenceTurn struct {
	Key      string  `json:"key"`
	EventID  string  `json:"event_id,omitempty"`
	Coverage float64 `json:"coverage"`
	Reached  bool    `json:"reached"`
	By       string  `json:"by,omitempty"`
	// Cited: a delivered fact or page item's text names EventID (the
	// secondary count of PROTOCOL.md "Measures"; ficha lines carry ids).
	Cited bool `json:"cited,omitempty"`
}

// StudyEvidence summarises a question's evidence in the block.
type StudyEvidence struct {
	Raw     []string            `json:"raw,omitempty"`
	Invalid []string            `json:"invalid,omitempty"`
	Turns   []StudyEvidenceTurn `json:"turns"`
	Scored  bool                `json:"scored"` // at least one valid evidence turn
	Reached int                 `json:"reached"`
	Full    bool                `json:"full"`
	// ReachedOrCited / FullOrCited add the turns only a citing item credits.
	ReachedOrCited int  `json:"reached_or_cited"`
	FullOrCited    bool `json:"full_or_cited"`
}

func (e *StudyEvidence) summarise() {
	e.Scored = len(e.Turns) > 0
	e.Reached, e.ReachedOrCited = 0, 0
	for _, t := range e.Turns {
		if t.Reached {
			e.Reached++
		}
		if t.Reached || t.Cited {
			e.ReachedOrCited++
		}
	}
	e.Full = e.Scored && e.Reached == len(e.Turns)
	e.FullOrCited = e.Scored && e.ReachedOrCited == len(e.Turns)
}

// StudyAskLine is one line of an ask (or rag) JSONL. Its first keys are
// those answer.py reads (question_id, condition, context, context_tokens).
type StudyAskLine struct {
	QuestionID       string        `json:"question_id"`
	Condition        string        `json:"condition"`
	Context          string        `json:"context"`
	ContextTokens    int           `json:"context_tokens"`
	Source           string        `json:"source"`
	SampleID         string        `json:"sample_id,omitempty"`
	Index            int           `json:"index"`
	Half             string        `json:"half,omitempty"`
	Question         string        `json:"question"`
	Gold             string        `json:"gold"` // harness only: the judge's input, never sent to Chanda
	Category         int           `json:"category,omitempty"`
	CategoryName     string        `json:"category_name,omitempty"`
	QuestionType     string        `json:"question_type,omitempty"`
	QuestionDate     string        `json:"question_date,omitempty"`
	SpeakerA         string        `json:"speaker_a,omitempty"`
	SpeakerB         string        `json:"speaker_b,omitempty"`
	RetrievalID      string        `json:"retrieval_id,omitempty"`
	ChandaTokensUsed int           `json:"chanda_tokens_used,omitempty"`
	Reason           string        `json:"reason,omitempty"`
	Items            []StudyItem   `json:"items"`
	Evidence         StudyEvidence `json:"evidence"`
	// Sleep items: facts and pages, rendered tokens over block tokens.
	SleepItems  int     `json:"sleep_items"`
	SleepTokens int     `json:"sleep_tokens"`
	SleepShare  float64 `json:"sleep_share"`
	RetrievalMS float64 `json:"retrieval_ms"`
	// KeyCandidates: logged candidates with features.key_rank > 0 (decision
	// 0145), delivered or not; 0 when keys are off.
	KeyCandidates int `json:"key_candidates,omitempty"`
	// DropReasons counts the logged candidates by their drop reason;
	// DroppedSuperseded lists those dropped as "superseded" (decision 0148:
	// the old side of an active supersession), in log order.
	DropReasons       map[string]int `json:"drop_reasons,omitempty"`
	DroppedSuperseded []string       `json:"dropped_superseded,omitempty"`
	ChandaSet         []string       `json:"chanda_set,omitempty"`
	ConfigHash        string         `json:"chanda_config_hash,omitempty"`
	DB                string         `json:"db,omitempty"`
}

// evTurn is an evidence turn to look for: its key, text and event id.
type evTurn struct{ Key, Content, EventID string }

// matchStudyEvidence applies the selection study's rule (evidence.go) to
// the raw excerpts of a block: coverage first in the items carrying the
// turn's event id, failing that in the text of all raw excerpts. Unlike the
// battery's whole-block fallback, fact and page items are left out of that
// fallback: an extractive ficha copies turn text literally, and the primary
// measure is about the turn itself. A fact or page whose text names the
// event id marks the turn Cited (secondary count).
func matchStudyEvidence(turns []evTurn, items []chanda.Item) []StudyEvidenceTurn {
	var raw strings.Builder
	for _, it := range items {
		if it.Type == chanda.RecEvent {
			raw.WriteString(squash(it.Text))
			raw.WriteString("\x00")
		}
	}
	whole := raw.String()
	out := make([]StudyEvidenceTurn, 0, len(turns))
	for _, t := range turns {
		e := StudyEvidenceTurn{Key: t.Key, EventID: t.EventID}
		tr := []rune(squash(t.Content))
		best := 0.0
		if t.EventID != "" {
			for _, it := range items {
				if it.ID == t.EventID {
					best = max(best, coverageSquashed(tr, squash(it.Text)))
				}
			}
		}
		if Reaches(best) {
			e.Reached, e.By = true, MatchByID
		} else if c := coverageSquashed(tr, whole); c > best {
			best = c
			if Reaches(c) {
				e.Reached, e.By = true, MatchByText
			}
		}
		e.Coverage = round3(best)
		if t.EventID != "" {
			for _, it := range items {
				if (it.Type == chanda.RecFact || it.Type == chanda.RecPage || it.Type == chanda.RecPageItem) && strings.Contains(it.Text, t.EventID) {
					e.Cited = true
				}
			}
		}
		out = append(out, e)
	}
	return out
}

func isSleepType(t chanda.RecordType) bool {
	return t == chanda.RecFact || t == chanda.RecPage || t == chanda.RecPageItem
}

// StudyAsk runs context (budget o.Budget) for each question on the
// database in dir and returns one line per question, in order. Every call
// is logged by Chanda as a retrieval.
func StudyAsk(ctx context.Context, o *StudyOptions, dir, cond string, qs []StudyQuestion) ([]*StudyAskLine, error) {
	m, err := ReadStudyMeta(dir)
	if err != nil {
		return nil, err
	}
	srv, h, err := o.start(ctx, dir, m, false)
	if err != nil {
		return nil, err
	}
	defer srv.Close()
	keyOf := m.EventKeys()
	evOf := m.KeyEvents()
	var lines []*StudyAskLine
	for i, q := range qs {
		l := &StudyAskLine{QuestionID: q.ID, Condition: cond, Source: m.Source, SampleID: m.SampleID, Index: q.Index, Half: q.Half,
			Question: q.Question, Gold: q.Gold, Category: q.Category, CategoryName: q.CategoryName, QuestionType: q.QuestionType,
			QuestionDate: q.QuestionDate, SpeakerA: m.SpeakerA, SpeakerB: m.SpeakerB, ChandaSet: OverrideStrings(o.Chanda.Set),
			ConfigHash: h, DB: dir}
		in := chanda.ContextInput{Query: q.Question, BudgetTokens: o.Budget}
		if q.lme != nil {
			if in.AsOf, err = AsOfDay(q.QuestionDate); err != nil {
				return nil, err
			}
		}
		start := time.Now()
		var out chanda.ContextOutput
		if err := srv.Call(ctx, chanda.ToolContext, in, &out); err != nil {
			return nil, fmt.Errorf("%s: %w", q.ID, err)
		}
		l.RetrievalMS = ms(time.Since(start))
		l.RetrievalID, l.Context, l.ChandaTokensUsed, l.Reason = out.RetrievalID, out.Text, out.TokensUsed, out.Reason
		l.ContextTokens = Tokens(out.Text)
		seg, trunc := BlockSegments(out.Text, out.Items)
		l.Items = []StudyItem{}
		for _, it := range out.Items {
			si := StudyItem{ID: it.ID, Type: string(it.Type), Tokens: Tokens(seg[it.ID]), Truncated: trunc[it.ID], Text: it.Text}
			if it.Type == chanda.RecEvent {
				si.Key = keyOf[it.ID]
			} else {
				si.OriginEventIDs = it.Origin.EventIDs
			}
			if isSleepType(it.Type) {
				l.SleepItems++
				l.SleepTokens += si.Tokens
			}
			l.Items = append(l.Items, si)
		}
		if l.ContextTokens > 0 {
			l.SleepShare = float64(l.SleepTokens) / float64(l.ContextTokens)
		}
		var turns []evTurn
		if q.lme != nil {
			for _, e := range EvidenceTurns(q.lme) {
				k := LMETurnKey(e.Session, e.Turn)
				turns = append(turns, evTurn{Key: k, Content: q.lme.HaystackSessions[e.Session][e.Turn].Content, EventID: evOf[k]})
			}
		} else {
			l.Evidence.Raw, l.Evidence.Invalid = q.EvidenceRaw, q.Evidence.Invalid
			for _, d := range q.Evidence.Valid {
				turns = append(turns, evTurn{Key: d, EventID: evOf[d]})
			}
			if len(turns) > 0 {
				if err := fillLocomoContent(m, turns); err != nil {
					return nil, err
				}
			}
		}
		l.Evidence.Turns = matchStudyEvidence(turns, out.Items)
		l.Evidence.summarise()
		lines = append(lines, l)
		o.logf("[%d/%d] %s %s tokens=%d items=%d sleep=%d evidence %d/%d", i+1, len(qs), cond, q.ID, l.ContextTokens,
			len(l.Items), l.SleepItems, l.Evidence.Reached, len(l.Evidence.Turns))
	}
	m.addStep(StudyStep{Step: "ask", Set: OverrideStrings(o.Chanda.Set), ConfigHash: h, Detail: fmt.Sprintf("%s: %d questions", cond, len(qs))})
	if err := srv.Close(); err != nil {
		return nil, err
	}
	if err := annotateRetrievalLog(ctx, dir, lines); err != nil {
		return nil, err
	}
	return lines, m.Write(dir)
}

// DropSuperseded is the drop reason of a superseded record (decision 0148).
const DropSuperseded = "superseded"

// annotateRetrievalLog reads each line's retrieval log back from the closed
// database, copies features.key_rank (decision 0145) onto the delivered
// items, and counts the drop reasons (DropSuperseded ids listed). Without
// keys every rank is 0; with nothing dropped the lines are unchanged.
func annotateRetrievalLog(ctx context.Context, dir string, lines []*StudyAskLine) error {
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	for _, l := range lines {
		if l.RetrievalID == "" {
			continue
		}
		var raw string
		if err := db.QueryRowContext(ctx, `SELECT candidates FROM retrievals WHERE id = ?`, l.RetrievalID).Scan(&raw); err != nil {
			return fmt.Errorf("%s: retrieval %s: %w", l.QuestionID, l.RetrievalID, err)
		}
		var cands []struct {
			ID       string             `json:"id"`
			Features map[string]float64 `json:"features"`
			Dropped  string             `json:"dropped"`
		}
		if err := json.Unmarshal([]byte(raw), &cands); err != nil {
			return fmt.Errorf("%s: retrieval %s candidates: %w", l.QuestionID, l.RetrievalID, err)
		}
		rank := map[string]int{}
		for _, c := range cands {
			if k := int(c.Features["key_rank"]); k > 0 {
				l.KeyCandidates++
				rank[c.ID] = k
			}
			if c.Dropped != "" {
				if l.DropReasons == nil {
					l.DropReasons = map[string]int{}
				}
				l.DropReasons[c.Dropped]++
				if c.Dropped == DropSuperseded {
					l.DroppedSuperseded = append(l.DroppedSuperseded, c.ID)
				}
			}
		}
		for i := range l.Items {
			l.Items[i].KeyRank = rank[l.Items[i].ID]
		}
	}
	return nil
}

// locomoCache holds the loaded LoCoMo file for fillLocomoContent.
var locomoCache = map[string]*LocomoConversation{}

// LoadStudyLocomo returns the conversation of a LoCoMo study directory,
// checking the file's SHA-256.
func LoadStudyLocomo(m *StudyMeta) (*LocomoConversation, error) {
	if c, ok := locomoCache[m.Dataset+"\x00"+m.SampleID]; ok {
		return c, nil
	}
	sum, _, err := FileSHA256(m.Dataset)
	if err != nil {
		return nil, err
	}
	if sum != m.DatasetSHA256 {
		return nil, fmt.Errorf("%s: sha256 %s, the study was ingested from %s", m.Dataset, sum, m.DatasetSHA256)
	}
	convs, err := LoadLocomo(m.Dataset)
	if err != nil {
		return nil, err
	}
	c, err := FindLocomo(convs, m.SampleID)
	if err != nil {
		return nil, err
	}
	locomoCache[m.Dataset+"\x00"+m.SampleID] = c
	return c, nil
}

func fillLocomoContent(m *StudyMeta, turns []evTurn) error {
	c, err := LoadStudyLocomo(m)
	if err != nil {
		return err
	}
	for i := range turns {
		t, _, ok := c.Turn(turns[i].Key)
		if !ok {
			return fmt.Errorf("evidence %s: no such turn", turns[i].Key)
		}
		turns[i].Content = LocomoContent(t)
	}
	return nil
}

// FileSHA256 is the hex SHA-256 and size of a file.
func FileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), n, nil
}

// ---------------------------------------------------------------------------
// JSONL.

// WriteJSONL writes one JSON value per line.
func WriteJSONL[T any](path string, rows []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			f.Close()
			return err
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadJSONL reads one JSON value per non-empty line.
func ReadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	var out []T
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, v)
	}
	return out, sc.Err()
}

// ---------------------------------------------------------------------------
// Outcome.

// StudyAnswer is the part of an answers JSONL line the outcome step reads:
// the answer text and the judge's verdict. Verdict may be a boolean or a
// string (yes/no, CORRECT/WRONG).
type StudyAnswer struct {
	QuestionID string          `json:"question_id"`
	Answer     string          `json:"answer"`
	Verdict    json.RawMessage `json:"verdict"`
}

// Correct decodes the verdict.
func (a StudyAnswer) Correct() (bool, error) {
	var b bool
	if json.Unmarshal(a.Verdict, &b) == nil {
		return b, nil
	}
	var s string
	if json.Unmarshal(a.Verdict, &s) == nil {
		switch strings.ToUpper(strings.TrimSpace(s)) {
		case "YES", "CORRECT", "TRUE":
			return true, nil
		case "NO", "WRONG", "FALSE":
			return false, nil
		}
	}
	return false, fmt.Errorf("%s: verdict %s is neither a boolean nor yes/no", a.QuestionID, a.Verdict)
}

// StudyOutcomeRecord is one recorded outcome.
type StudyOutcomeRecord struct {
	QuestionID  string   `json:"question_id"`
	RetrievalID string   `json:"retrieval_id"`
	Value       float64  `json:"value"`
	UsedIDs     []string `json:"used_ids"`
	OutcomeID   string   `json:"outcome_id"`
	Credited    int      `json:"credited"`
}

// openRO opens a closed Chanda database read-only.
func openRO(dbPath string) (*sql.DB, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// StudyOutcome records one outcome per ask line on the database in dir:
// value +1 when the judge said correct, -1 otherwise; used_ids by UsedIDs
// over the delivered items. It refuses lines whose retrieval is not in
// this database, retrievals that already have an outcome, and lines with
// no answer (unless allowMissing, which skips them).
func StudyOutcome(ctx context.Context, o *StudyOptions, dir string, lines []*StudyAskLine, answers map[string]StudyAnswer, allowMissing bool) ([]StudyOutcomeRecord, error) {
	m, err := ReadStudyMeta(dir)
	if err != nil {
		return nil, err
	}
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		return nil, err
	}
	// A run stopped mid-way (rate limit) resumes: a retrieval that already
	// has an outcome is skipped when the recorded value is the one computed now.
	resume := true
	existing := map[string]*StudyOutcomeRecord{}
	for _, l := range lines {
		if l.RetrievalID == "" {
			db.Close()
			return nil, fmt.Errorf("%s: no retrieval_id (a plain-RAG line?)", l.QuestionID)
		}
		var nr, no int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retrievals WHERE id = ?`, l.RetrievalID).Scan(&nr); err != nil {
			db.Close()
			return nil, err
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outcomes WHERE retrieval_id = ?`, l.RetrievalID).Scan(&no); err != nil {
			db.Close()
			return nil, err
		}
		if nr != 1 {
			db.Close()
			return nil, fmt.Errorf("%s: retrieval %s is not in %s", l.QuestionID, l.RetrievalID, dir)
		}
		if no != 0 {
			if !resume {
				db.Close()
				return nil, fmt.Errorf("%s: retrieval %s already has an outcome", l.QuestionID, l.RetrievalID)
			}
			var v float64
			var used, id string
			if err := db.QueryRowContext(ctx, `SELECT value, used_ids, id FROM outcomes WHERE retrieval_id = ?`, l.RetrievalID).Scan(&v, &used, &id); err != nil {
				db.Close()
				return nil, err
			}
			existing[l.RetrievalID] = &StudyOutcomeRecord{QuestionID: l.QuestionID, RetrievalID: l.RetrievalID, Value: v, OutcomeID: id}
			_ = json.Unmarshal([]byte(used), &existing[l.RetrievalID].UsedIDs)
		}
	}
	db.Close()
	type job struct {
		l    *StudyAskLine
		val  float64
		used []string
	}
	var jobs []job
	for _, l := range lines {
		a, ok := answers[l.QuestionID]
		if !ok {
			if allowMissing {
				o.logf("outcome: %s has no answer, skipped", l.QuestionID)
				continue
			}
			return nil, fmt.Errorf("%s: no answer in the answers file", l.QuestionID)
		}
		c, err := a.Correct()
		if err != nil {
			return nil, err
		}
		items := make([]UsedItem, 0, len(l.Items))
		for _, it := range l.Items {
			items = append(items, UsedItem{ID: it.ID, Text: it.Text})
		}
		v := -1.0
		if c {
			v = 1
		}
		jobs = append(jobs, job{l, v, UsedIDs(a.Answer, items)})
	}
	srv, h, err := o.start(ctx, dir, m, false)
	if err != nil {
		return nil, err
	}
	defer srv.Close()
	var recs []StudyOutcomeRecord
	for _, j := range jobs {
		if e := existing[j.l.RetrievalID]; e != nil {
			if e.Value != j.val {
				return recs, fmt.Errorf("%s: retrieval %s already has outcome %v, computed %v", j.l.QuestionID, j.l.RetrievalID, e.Value, j.val)
			}
			recs = append(recs, *e)
			continue
		}
		var out chanda.OutcomeOutput
		for attempt := 0; ; attempt++ {
			err := srv.Call(ctx, chanda.ToolOutcome, chanda.OutcomeInput{RetrievalID: j.l.RetrievalID, Value: j.val, UsedIDs: j.used}, &out)
			if err == nil {
				break
			}
			// ranking.outcome_rate_per_minute (60): wait for the window to pass.
			if attempt < 3 && strings.Contains(err.Error(), "rate_limited") {
				o.logf("outcome: %s rate limited; waiting 61 s", j.l.QuestionID)
				select {
				case <-time.After(61 * time.Second):
				case <-ctx.Done():
					return recs, ctx.Err()
				}
				continue
			}
			return recs, fmt.Errorf("%s: %w", j.l.QuestionID, err)
		}
		recs = append(recs, StudyOutcomeRecord{QuestionID: j.l.QuestionID, RetrievalID: j.l.RetrievalID, Value: j.val, UsedIDs: j.used,
			OutcomeID: out.OutcomeID, Credited: out.Credited})
	}
	pos := 0
	for _, r := range recs {
		if r.Value > 0 {
			pos++
		}
	}
	m.addStep(StudyStep{Step: "outcome", Set: OverrideStrings(o.Chanda.Set), ConfigHash: h, Detail: fmt.Sprintf("%d outcomes, %d positive", len(recs), pos)})
	if err := srv.Close(); err != nil {
		return recs, err
	}
	return recs, m.Write(dir)
}

// ---------------------------------------------------------------------------
// Copy.

// StudyCopy copies a closed study directory (database, its WAL side files
// when present, study.json) to a new directory. Logs, snapshots and the
// chanda.yaml of the source are not copied.
func StudyCopy(from, to string) error {
	m, err := ReadStudyMeta(from)
	if err != nil {
		return err
	}
	if !dbExists(from) {
		return fmt.Errorf("%s has no chanda.db", from)
	}
	if ents, err := os.ReadDir(to); err == nil && len(ents) > 0 {
		return fmt.Errorf("%s exists and is not empty", to)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	for _, n := range []string{"chanda.db", "chanda.db-wal", "chanda.db-shm"} {
		src := filepath.Join(from, n)
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := copyFile(src, filepath.Join(to, n)); err != nil {
			return err
		}
	}
	abs, _ := filepath.Abs(from)
	m.CopiedFrom = abs
	m.addStep(StudyStep{Step: "copy", Detail: "from " + abs})
	return m.Write(to)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
