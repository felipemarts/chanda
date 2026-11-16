package lme

// The conflict-resolution study (eval/conflicts/PROTOCOL.md, decision
// 0148): the conflicts admin step, the protocol's leak check, and the
// Portuguese suite's two-session ingestion. All work on a study directory
// like the sleep study's steps.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// ---------------------------------------------------------------------------
// The conflicts admin tool (decision 0148) over MCP.

// ToolConflicts is the admin tool of decision 0148.
const ToolConflicts = "conflicts"

// ConflictsActions are the actions the study step calls (the tool also has
// revert, which the study never uses).
var ConflictsActions = []string{"scan", "stats", "list", "reset"}

// ErrNoConflictsTool: the Chanda build has no conflicts tool yet.
var ErrNoConflictsTool = errors.New("this Chanda build has no admin tool " + ToolConflicts +
	" (decision 0148, conflict resolution, is not implemented on this branch yet)")

// ConflictsArgs are the scan arguments: write the queue, return the
// candidate and refusal lists, and the similarity floor (0: the
// configuration's sleep.conflicts.min_cosine).
type ConflictsArgs struct {
	Write     bool
	Details   bool
	MinCosine float64
}

// CheckConflictsAction refuses an action the step does not call, and scan
// flags on another action.
func CheckConflictsAction(action string, a ConflictsArgs) error {
	ok := false
	for _, x := range ConflictsActions {
		ok = ok || x == action
	}
	if !ok {
		return fmt.Errorf("conflicts action %q: want one of %s", action, strings.Join(ConflictsActions, "|"))
	}
	if action != "scan" && (a.Write || a.Details || a.MinCosine != 0) {
		return fmt.Errorf("conflicts %s: -write, -details and -min-cosine are scan flags", action)
	}
	if a.MinCosine < 0 || a.MinCosine > 1 {
		return fmt.Errorf("-min-cosine %v: want a cosine in (0, 1]", a.MinCosine)
	}
	return nil
}

// toolArgs is the tool input: action, and for scan write and details
// (always) and min_cosine (when given).
func (a ConflictsArgs) toolArgs(action string) map[string]any {
	in := map[string]any{"action": action}
	if action == "scan" {
		in["write"], in["details"] = a.Write, a.Details
		if a.MinCosine > 0 {
			in["min_cosine"] = a.MinCosine
		}
	}
	return in
}

// StudyAdminResult is the saved result of an admin-tool step (keys,
// conflicts).
type StudyAdminResult struct {
	DB         string          `json:"db"`
	Tool       string          `json:"tool"`
	Action     string          `json:"action"`
	Args       map[string]any  `json:"args,omitempty"`
	ChandaSet  []string        `json:"chanda_set,omitempty"`
	ConfigHash string          `json:"chanda_config_hash"`
	At         string          `json:"at"`
	WallMS     float64         `json:"wall_ms"`
	Output     json.RawMessage `json:"output"` // the tool's data exactly as returned over MCP
}

// studyAdmin calls tool with args on the database in dir with the admin
// profile and the step's configuration and no LLM, optionally flushes, and
// closes the server. missing is returned (wrapped) when the server does not
// list the tool.
func studyAdmin(ctx context.Context, o *StudyOptions, dir, step, tool string, args map[string]any, missing error, flush bool) (*StudyAdminResult, error) {
	m, err := ReadStudyMeta(dir)
	if err != nil {
		return nil, err
	}
	so := *o
	so.Chanda.LLMURL = ""
	srv, h, err := so.start(ctx, dir, m, true)
	if err != nil {
		return nil, err
	}
	defer srv.Close()
	ok, err := srv.HasTool(ctx, tool)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s: %w", dir, missing)
	}
	action, _ := args["action"].(string)
	res := &StudyAdminResult{DB: dir, Tool: tool, Action: action, Args: args, ChandaSet: OverrideStrings(o.Chanda.Set), ConfigHash: h}
	start := time.Now()
	var raw json.RawMessage
	if err := srv.Call(ctx, tool, args, &raw); err != nil {
		return nil, err
	}
	res.WallMS = ms(time.Since(start))
	res.At = start.UTC().Format(time.RFC3339)
	res.Output = raw
	if flush {
		var f chanda.FlushOutput
		if err := srv.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
			return nil, err
		}
	}
	b, _ := json.Marshal(args)
	m.addStep(StudyStep{Step: step, Set: OverrideStrings(o.Chanda.Set), ConfigHash: h, Detail: fmt.Sprintf("%s: %.300s", b, raw)})
	if err := srv.Close(); err != nil {
		return nil, err
	}
	return res, m.Write(dir)
}

// StudyConflicts calls conflicts {action, ...} on the database in dir. It
// returns ErrNoConflictsTool (wrapped) when the server does not list the
// tool.
func StudyConflicts(ctx context.Context, o *StudyOptions, dir, action string, a ConflictsArgs) (*StudyAdminResult, error) {
	if err := CheckConflictsAction(action, a); err != nil {
		return nil, err
	}
	return studyAdmin(ctx, o, dir, "conflicts", ToolConflicts, a.toolArgs(action), ErrNoConflictsTool, false)
}

// ---------------------------------------------------------------------------
// Leak check of the conflict study (eval/conflicts/PROTOCOL.md, Procedure
// step 3), run on a closed database before its question is asked.

// ConflictLeakReport is the result of StudyLeakCheckConflicts.
type ConflictLeakReport struct {
	Protocol      string   `json:"protocol"` // "conflicts"
	DB            string   `json:"db"`
	Scanned       []string `json:"scanned"`
	Skipped       []string `json:"skipped"`
	ValuesScanned int      `json:"values_scanned"`
	Retrievals    int      `json:"retrievals"` // stop when > 0: the database has not been asked yet
	Questions     int      `json:"questions"`
	GoldsChecked  int      `json:"golds_checked"` // gold answers of 4 characters or more (normalised)
	GoldsTooShort int      `json:"golds_too_short"`
	// QuestionHits: the question's text in a value outside the ingested
	// turns (stop). GoldHits: the gold answer, as whole words (stop).
	QuestionHits []LeakHit `json:"question_hits"`
	GoldHits     []LeakHit `json:"gold_hits"`
	// GoldInTurns: golds that also occur in an ingested turn. Informative
	// only: a gold hit is a stop either way.
	GoldInTurns []string  `json:"gold_in_turns"`
	JudgeOutput []LeakHit `json:"judge_output"` // stop
	Failures    []string  `json:"failures"`
	OK          bool      `json:"ok"`
}

// StudyLeakCheckConflicts runs the conflict protocol's leak check on the
// closed database in dir (opened read-only). qs are the directory's
// questions (one for a LongMemEval directory). A run stops (OK false) when:
//   - a question's text is in any text value outside the ingested turns
//     (every table and column from sqlite_master, as the learning study's
//     check: the conflict queue, supersessions, sleep_runs report and
//     outputs, audit details, retrievals, ...; FTS tables and their shadows
//     are skipped);
//   - a gold answer of 4 characters or more is there, as whole words;
//   - judge output (CORRECT/WRONG, "gold answer", "generated answer") is
//     there;
//   - the database holds any retrieval (it has not been asked yet).
func StudyLeakCheckConflicts(ctx context.Context, dir string, qs []StudyQuestion) (*ConflictLeakReport, error) {
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	r := &ConflictLeakReport{Protocol: "conflicts", DB: dir, Scanned: []string{}, Skipped: []string{}, QuestionHits: []LeakHit{},
		GoldHits: []LeakHit{}, GoldInTurns: []string{}, JudgeOutput: []LeakHit{}, Failures: []string{}, Questions: len(qs)}
	type needle struct{ text, norm string }
	var questions, golds []needle
	seen := map[string]bool{}
	for _, q := range qs {
		if n := normWords(q.Question); strings.TrimSpace(n) != "" && !seen["q"+n] {
			seen["q"+n] = true
			questions = append(questions, needle{q.Question, n})
		}
		n := normWords(q.Gold)
		if seen["g"+n] {
			continue
		}
		seen["g"+n] = true
		if len([]rune(strings.TrimSpace(n))) < 4 {
			r.GoldsTooShort++
			continue
		}
		golds = append(golds, needle{q.Gold, n})
	}
	r.GoldsChecked = len(golds)

	ts, err := listTables(ctx, db)
	if err != nil {
		return nil, err
	}
	var virtuals []string
	found := map[string]bool{}
	for _, t := range ts {
		found[t.name] = true
		if t.virtual {
			virtuals = append(virtuals, t.name)
		}
	}
	for _, need := range []string{"retrievals", "events"} {
		if !found[need] {
			return nil, fmt.Errorf("no %s table: not a Chanda database", need)
		}
	}
	if err := scanTextValues(ctx, db, ts, virtuals, &r.Scanned, &r.Skipped, func(table, col, id string, leaves []string) {
		r.ValuesScanned++
		for _, x := range leaves {
			if reJudgeWords.MatchString(x) {
				r.JudgeOutput = append(r.JudgeOutput, LeakHit{Table: table, Column: col, Row: id, Match: reJudgeWords.FindString(x)})
			}
			nv := normWords(x)
			for _, nd := range questions {
				if strings.Contains(nv, nd.norm) {
					r.QuestionHits = append(r.QuestionHits, LeakHit{Table: table, Column: col, Row: id, Match: nd.text})
				}
			}
			for _, nd := range golds {
				if strings.Contains(nv, nd.norm) {
					r.GoldHits = append(r.GoldHits, LeakHit{Table: table, Column: col, Row: id, Match: nd.text})
				}
			}
		}
	}); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retrievals`).Scan(&r.Retrievals); err != nil {
		return nil, err
	}
	if len(r.GoldHits) > 0 {
		inTurns := map[string]bool{}
		rows, err := db.QueryContext(ctx, `SELECT content FROM events WHERE kind = 'turn'`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return nil, err
			}
			nc := normWords(c)
			for _, nd := range golds {
				if strings.Contains(nc, nd.norm) {
					inTurns[nd.text] = true
				}
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		for g := range inTurns {
			r.GoldInTurns = append(r.GoldInTurns, g)
		}
		sort.Strings(r.GoldInTurns)
	}
	for _, f := range []struct {
		n    int
		what string
	}{
		{len(r.QuestionHits), "question text outside the ingested turns"},
		{len(r.GoldHits), "gold answer outside the ingested turns"},
		{len(r.JudgeOutput), "judge output in the database"},
		{r.Retrievals, "retrieval in a database that has not been asked yet"},
	} {
		if f.n > 0 {
			r.Failures = append(r.Failures, fmt.Sprintf("%d %s", f.n, f.what))
		}
	}
	r.OK = len(r.Failures) == 0
	return r, nil
}

// ---------------------------------------------------------------------------
// The Portuguese suite (eval/conflicts/suite/pairs.json): one database per
// scripted pair, the older session then the newer.

// SourceSuite is the study source of a suite pair directory.
const SourceSuite = "conflicts-suite"

// SuiteSession is one scripted session.
type SuiteSession struct {
	Date  string `json:"date"` // RFC 3339
	Turns []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"turns"`
}

// SuitePair is one pair of pairs.json.
type SuitePair struct {
	ID       string       `json:"id"`
	Kind     string       `json:"kind"` // update or control
	Category string       `json:"category"`
	Older    SuiteSession `json:"older"`
	Newer    SuiteSession `json:"newer"`
	Expected string       `json:"expected"` // harness only, never sent to Chanda
}

// LoadSuitePair reads pairs.json and returns the pair with that id and the
// file's SHA-256.
func LoadSuitePair(path, id string) (*SuitePair, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var f struct {
		Pairs []SuitePair `json:"pairs"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(b))
	for i := range f.Pairs {
		if f.Pairs[i].ID == id {
			return &f.Pairs[i], sum, nil
		}
	}
	return nil, "", fmt.Errorf("%s: no pair %s", path, id)
}

// SuiteSessionID is the opaque session id of a pair's session (0 older, 1
// newer): it says nothing about the order or the expected verdict.
func SuiteSessionID(pair string, i int) string { return fmt.Sprintf("%s-s%d", pair, i+1) }

// StudyIngestPair logs the pair's older session, then its newer one, into
// a new database in dir (turn ts = the session date), and flushes. Turn
// keys are s<0|1>t<turn>, as for LongMemEval.
func StudyIngestPair(ctx context.Context, o *StudyOptions, dir string, p *SuitePair, m *StudyMeta) error {
	if dbExists(dir) {
		return fmt.Errorf("%s already holds a database", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m.Source, m.QuestionID, m.Sessions = SourceSuite, p.ID, 2
	srv, h, err := o.start(ctx, dir, m, false)
	if err != nil {
		return err
	}
	defer srv.Close()
	counts := map[string]int{}
	for i, s := range []SuiteSession{p.Older, p.Newer} {
		turns := make([]chanda.Turn, len(s.Turns))
		for j, t := range s.Turns {
			turns[j] = chanda.Turn{Role: t.Role, Content: t.Content, TS: s.Date}
		}
		var out chanda.LogSessionOutput
		if err := srv.Call(ctx, chanda.ToolLogSession, chanda.LogSessionInput{SessionID: SuiteSessionID(p.ID, i), Turns: turns}, &out); err != nil {
			return fmt.Errorf("%s session %d: %w", p.ID, i, err)
		}
		for _, tr := range out.Turns {
			counts[tr.Action]++
			m.Turns = append(m.Turns, StudyTurn{Key: LMETurnKey(i, tr.Ordinal), Session: i, Turn: tr.Ordinal,
				SessionID: SuiteSessionID(p.ID, i), EventID: tr.ID, Action: tr.Action})
		}
	}
	var f chanda.FlushOutput
	if err := srv.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
		return err
	}
	if !f.Complete {
		return fmt.Errorf("flush incomplete: %+v", f)
	}
	m.Ingest = map[string]any{"turns": len(m.Turns), "actions": counts}
	m.addStep(StudyStep{Step: "ingest", Set: OverrideStrings(o.Chanda.Set), ConfigHash: h, Detail: fmt.Sprintf("suite pair %s: 2 sessions", p.ID)})
	if err := srv.Close(); err != nil {
		return err
	}
	return m.Write(dir)
}
