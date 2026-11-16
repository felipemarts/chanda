package lme

// The learning study (eval/learn/PROTOCOL.md): the learned-keys admin step
// (decision 0145) and the protocol's leak check. Both work on a study
// directory like the sleep study's steps.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// ---------------------------------------------------------------------------
// Keys (decision 0145): the admin tool learned_keys over MCP.

// ToolLearnedKeys is the admin tool of decision 0145.
const ToolLearnedKeys = "learned_keys"

// KeysActions are the actions of the learned_keys tool.
var KeysActions = []string{"enable", "disable", "reset", "rebuild", "stats"}

// ErrNoKeysTool: the Chanda build has no learned_keys tool yet.
var ErrNoKeysTool = errors.New("this Chanda build has no admin tool " + ToolLearnedKeys +
	" (decision 0145, learned query keys, is not implemented on this branch yet)")

// StudyKeysResult is the saved result of a keys step.
type StudyKeysResult struct {
	DB         string          `json:"db"`
	Action     string          `json:"action"`
	ChandaSet  []string        `json:"chanda_set,omitempty"`
	ConfigHash string          `json:"chanda_config_hash"`
	At         string          `json:"at"`
	WallMS     float64         `json:"wall_ms"`
	Output     json.RawMessage `json:"output"` // the tool's data exactly as returned over MCP
}

// CheckKeysAction refuses an action the tool does not have.
func CheckKeysAction(action string) error {
	for _, a := range KeysActions {
		if a == action {
			return nil
		}
	}
	return fmt.Errorf("keys action %q: want one of %s", action, strings.Join(KeysActions, "|"))
}

// StudyKeys calls learned_keys {action} on the database in dir with the
// admin profile and the step's configuration, flushes (a rebuild enqueues
// key embeddings) and closes the server. It returns ErrNoKeysTool (wrapped)
// when the server does not list the tool. No LLM is configured.
func StudyKeys(ctx context.Context, o *StudyOptions, dir, action string) (*StudyKeysResult, error) {
	if err := CheckKeysAction(action); err != nil {
		return nil, err
	}
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
	ok, err := srv.HasTool(ctx, ToolLearnedKeys)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s: %w", dir, ErrNoKeysTool)
	}
	res := &StudyKeysResult{DB: dir, Action: action, ChandaSet: OverrideStrings(o.Chanda.Set), ConfigHash: h}
	start := time.Now()
	var raw json.RawMessage
	if err := srv.Call(ctx, ToolLearnedKeys, map[string]any{"action": action}, &raw); err != nil {
		return nil, err
	}
	res.WallMS = ms(time.Since(start))
	res.At = start.UTC().Format(time.RFC3339)
	res.Output = raw
	if action == "rebuild" {
		var f struct {
			Complete bool `json:"complete"`
		}
		if err := srv.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
			return nil, err
		}
	}
	m.addStep(StudyStep{Step: "keys", Set: OverrideStrings(o.Chanda.Set), ConfigHash: h, Detail: fmt.Sprintf("%s: %.300s", action, raw)})
	if err := srv.Close(); err != nil {
		return nil, err
	}
	return res, m.Write(dir)
}

// ---------------------------------------------------------------------------
// Leak check of the learning study (eval/learn/PROTOCOL.md "Leak check"),
// run on a closed database before a condition's test half.

// LearnedKeysTable is the name decision 0145's migration is expected to
// use. DetectKeyTable also accepts the other names below, and any table
// with the key columns (text, record_id, retrieval_id).
const LearnedKeysTable = "learned_keys"

var keyTableNames = []string{LearnedKeysTable, "query_keys", "record_keys", "keys"}

// LeakHit is one match of a forbidden string in a database value.
type LeakHit struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Row    string `json:"row"`   // the row's id (or rowid)
	Match  string `json:"match"` // what was found: the test question, the gold answer or the judge word
	// Via: the use-half question, held in the same value, that contains
	// the match (an explained hit).
	Via string `json:"via,omitempty"`
}

// LearnLeakReport is the result of StudyLeakCheckLearn.
type LearnLeakReport struct {
	Protocol string `json:"protocol"` // "learn"
	DB       string `json:"db"`
	KeyTable string `json:"key_table"` // "" when the database has none
	KeyNote  string `json:"key_note,omitempty"`
	Keys     int    `json:"keys"`
	// Tables and columns whose text values were scanned, and those skipped
	// (FTS virtual tables and their shadow tables; event turns are skipped
	// row by row: kind = 'turn').
	Scanned        []string  `json:"scanned"`
	Skipped        []string  `json:"skipped"`
	ValuesScanned  int       `json:"values_scanned"`
	Retrievals     int       `json:"retrievals"`
	Outcomes       int       `json:"outcomes"`
	UseQuestions   int       `json:"use_questions"`
	TestQuestions  int       `json:"test_questions"`
	GoldsChecked   int       `json:"golds_checked"` // gold answers of 4 characters or more (normalised)
	GoldsTooShort  int       `json:"golds_too_short"`
	TestQuestionIn []LeakHit `json:"test_question_in_db"` // stop
	// TestQuestionInUse: a test question found only inside a use-half
	// question the value holds (one question's text contains the other's).
	// Listed, not a stop.
	TestQuestionInUse []LeakHit `json:"test_question_inside_use_question"`
	KeysNotUse        []string  `json:"keys_not_use_question"`       // stop: key id: text
	RetrievalsNotUse  []string  `json:"retrievals_not_use_question"` // stop: retrieval id: query
	BadOutcomes       []string  `json:"bad_outcomes"`                // stop: outcome id: reason
	JudgeOutput       []LeakHit `json:"judge_output"`                // stop
	GoldHits          []LeakHit `json:"gold_hits"`                   // stop: in keys, retrievals or outcomes
	GoldInUseQuestion []LeakHit `json:"gold_in_use_question"`        // listed: inside a use-half question's text
	Failures          []string  `json:"failures"`
	OK                bool      `json:"ok"`
}

// LearnLeakInput: the split's questions of the database's conversation.
type LearnLeakInput struct {
	Use, Test []StudyQuestion
	KeyTable  string // "" detects it
}

// normWords lowercases s and keeps its runs of letters and digits, one
// space apart, between two spaces (so that a Contains is a match on whole
// words). JSON escapes, punctuation and spacing do not matter.
func normWords(s string) string {
	var b strings.Builder
	b.WriteByte(' ')
	in := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			in = true
		} else if in {
			b.WriteByte(' ')
			in = false
		}
	}
	if in {
		b.WriteByte(' ')
	}
	return b.String()
}

// textsOf returns the strings to check in one stored value: the value
// itself, or for a JSON object or array the string leaves (numbers, field
// names and ids carry no text).
func textsOf(v string) []string {
	t := strings.TrimSpace(v)
	if t == "" || (t[0] != '{' && t[0] != '[') || !json.Valid([]byte(t)) {
		return []string{v}
	}
	var x any
	if json.Unmarshal([]byte(t), &x) != nil {
		return []string{v}
	}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch y := x.(type) {
		case string:
			out = append(out, y)
		case []any:
			for _, e := range y {
				walk(e)
			}
		case map[string]any:
			keys := make([]string, 0, len(y))
			for k := range y {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(y[k])
			}
		}
	}
	walk(x)
	return out
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

type tableInfo struct {
	name    string
	virtual bool
	cols    []string
	hasID   bool
}

func listTables(ctx context.Context, db *sql.DB) ([]tableInfo, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var ts []tableInfo
	for rows.Next() {
		var n, s string
		if err := rows.Scan(&n, &s); err != nil {
			rows.Close()
			return nil, err
		}
		ts = append(ts, tableInfo{name: n, virtual: strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "CREATE VIRTUAL TABLE")})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range ts {
		if ts[i].virtual {
			continue
		}
		cr, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, ts[i].name)
		if err != nil {
			return nil, err
		}
		for cr.Next() {
			var c string
			if err := cr.Scan(&c); err != nil {
				cr.Close()
				return nil, err
			}
			ts[i].cols = append(ts[i].cols, c)
			if c == "id" {
				ts[i].hasID = true
			}
		}
		if err := cr.Close(); err != nil {
			return nil, err
		}
	}
	return ts, nil
}

// shadowOf reports the virtual table a table is a shadow of (FTS5 keeps
// <name>_data, _idx, _content, _docsize, _config), or "".
func shadowOf(name string, virtuals []string) string {
	for _, v := range virtuals {
		if strings.HasPrefix(name, v+"_") {
			return v
		}
	}
	return ""
}

// DetectKeyTable finds the learned-keys table: one of the expected names,
// else a table with text, record_id and retrieval_id columns. "" if none.
func DetectKeyTable(ctx context.Context, db *sql.DB) (string, error) {
	ts, err := listTables(ctx, db)
	if err != nil {
		return "", err
	}
	byName := map[string]tableInfo{}
	var virtuals []string
	for _, t := range ts {
		byName[t.name] = t
		if t.virtual {
			virtuals = append(virtuals, t.name)
		}
	}
	for _, n := range keyTableNames {
		if t, ok := byName[n]; ok && !t.virtual {
			return n, nil
		}
	}
	for _, t := range ts {
		if t.virtual || shadowOf(t.name, virtuals) != "" {
			continue
		}
		has := map[string]bool{}
		for _, c := range t.cols {
			has[c] = true
		}
		if has["text"] && has["record_id"] && has["retrieval_id"] {
			return t.name, nil
		}
	}
	return "", nil
}

// StudyLeakCheckLearn runs the learning protocol's leak check on the
// closed database in dir. It opens the file read-only. A run stops (OK
// false) when:
//   - a test question's text is in any text value outside the ingested
//     turns (every table and column enumerated from sqlite_master; FTS
//     tables and their shadows are copies of other tables and are skipped),
//     unless the value holds it only inside a use-half question;
//   - a key's text or a retrieval's query is not a use-half question;
//   - an outcome's value is not +-1, its retrieval is missing, or a used id
//     is not among the retrieval's delivered ids;
//   - judge output (CORRECT/WRONG, "gold answer", "generated answer") is in
//     any text value outside the ingested turns;
//   - a gold answer is in the keys, retrievals or outcomes, unless it
//     occurs inside a use-half question the value holds (listed).
func StudyLeakCheckLearn(ctx context.Context, dir string, in LearnLeakInput) (*LearnLeakReport, error) {
	db, err := openRO(filepath.Join(dir, "chanda.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	r, err := LeakCheckLearnDB(ctx, db, in)
	if r != nil {
		r.DB = dir
	}
	return r, err
}

// LeakCheckLearnDB is StudyLeakCheckLearn on an open database.
func LeakCheckLearnDB(ctx context.Context, db *sql.DB, in LearnLeakInput) (*LearnLeakReport, error) {
	r := &LearnLeakReport{Protocol: "learn", Scanned: []string{}, Skipped: []string{}, TestQuestionIn: []LeakHit{},
		TestQuestionInUse: []LeakHit{}, KeysNotUse: []string{}, RetrievalsNotUse: []string{}, BadOutcomes: []string{},
		JudgeOutput: []LeakHit{}, GoldHits: []LeakHit{}, GoldInUseQuestion: []LeakHit{}, Failures: []string{},
		UseQuestions: len(in.Use), TestQuestions: len(in.Test)}

	// Normalised question texts.
	useSet := map[string]string{} // norm -> text
	var useNorm []string
	for _, q := range in.Use {
		n := normWords(q.Question)
		if _, ok := useSet[n]; !ok {
			useSet[n] = q.Question
			useNorm = append(useNorm, n)
		}
	}
	sort.Strings(useNorm)
	type needle struct {
		text, norm string
		inUse      []string // normalised use questions that contain it
	}
	mk := func(text string) needle {
		nd := needle{text: text, norm: normWords(text)}
		for _, u := range useNorm {
			if strings.Contains(u, nd.norm) {
				nd.inUse = append(nd.inUse, u)
			}
		}
		return nd
	}
	var tests []needle
	seenT := map[string]bool{}
	for _, q := range in.Test {
		n := normWords(q.Question)
		if strings.TrimSpace(n) == "" || seenT[n] {
			continue
		}
		seenT[n] = true
		tests = append(tests, mk(q.Question))
	}
	var golds []needle
	seenG := map[string]bool{}
	for _, q := range append(append([]StudyQuestion{}, in.Use...), in.Test...) {
		n := normWords(q.Gold)
		if seenG[n] {
			continue
		}
		seenG[n] = true
		if len([]rune(strings.TrimSpace(n))) < 4 {
			r.GoldsTooShort++
			continue
		}
		golds = append(golds, mk(q.Gold))
	}
	r.GoldsChecked = len(golds)
	// match reports whether nd is in the normalised value nv, and the use
	// question that explains it ("" when none does).
	match := func(nd needle, nv string) (bool, string) {
		if !strings.Contains(nv, nd.norm) {
			return false, ""
		}
		for _, u := range nd.inUse {
			if strings.Contains(nv, u) {
				return true, useSet[u]
			}
		}
		return true, ""
	}

	// Key table.
	kt := in.KeyTable
	var err error
	if kt == "" {
		if kt, err = DetectKeyTable(ctx, db); err != nil {
			return nil, err
		}
	}
	r.KeyTable = kt
	if kt == "" {
		r.KeyNote = "no key table"
	}

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
	if in.KeyTable != "" && !found[in.KeyTable] {
		return nil, fmt.Errorf("key table %q is not in the database", in.KeyTable)
	}
	for _, need := range []string{"retrievals", "outcomes", "events"} {
		if !found[need] {
			return nil, fmt.Errorf("no %s table: not a Chanda database", need)
		}
	}
	goldTables := map[string]bool{"retrievals": true, "outcomes": true}
	if kt != "" {
		goldTables[kt] = true
	}

	// Generic scan of every text value.
	for _, t := range ts {
		if t.virtual {
			r.Skipped = append(r.Skipped, t.name+" (virtual)")
			continue
		}
		if v := shadowOf(t.name, virtuals); v != "" {
			r.Skipped = append(r.Skipped, t.name+" (shadow of "+v+")")
			continue
		}
		if len(t.cols) == 0 {
			continue
		}
		idExpr := "rowid"
		if t.hasID {
			idExpr = "id"
		}
		cols := make([]string, len(t.cols))
		for i, c := range t.cols {
			cols[i] = quoteIdent(c)
		}
		q := "SELECT CAST(" + idExpr + " AS TEXT), " + strings.Join(cols, ", ") + " FROM " + quoteIdent(t.name)
		if t.name == "events" {
			q += " WHERE kind IS NOT 'turn'"
			r.Skipped = append(r.Skipped, "events rows of kind turn (the ingested turns)")
		}
		rows, err := db.QueryContext(ctx, q)
		if err != nil && idExpr == "rowid" { // WITHOUT ROWID table
			q = strings.Replace(q, "CAST(rowid AS TEXT)", "''", 1)
			rows, err = db.QueryContext(ctx, q)
		}
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", t.name, err)
		}
		r.Scanned = append(r.Scanned, t.name+"("+strings.Join(t.cols, ",")+")")
		vals := make([]any, len(t.cols)+1)
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			id, _ := vals[0].(string)
			for ci, c := range t.cols {
				s, ok := vals[ci+1].(string)
				if !ok || s == "" {
					continue
				}
				r.ValuesScanned++
				for _, x := range textsOf(s) {
					if reJudgeWords.MatchString(x) {
						r.JudgeOutput = append(r.JudgeOutput, LeakHit{Table: t.name, Column: c, Row: id, Match: reJudgeWords.FindString(x)})
					}
					nv := normWords(x)
					for _, nd := range tests {
						if ok, via := match(nd, nv); ok {
							h := LeakHit{Table: t.name, Column: c, Row: id, Match: nd.text, Via: via}
							if via == "" {
								r.TestQuestionIn = append(r.TestQuestionIn, h)
							} else {
								r.TestQuestionInUse = append(r.TestQuestionInUse, h)
							}
						}
					}
					if goldTables[t.name] {
						for _, nd := range golds {
							if ok, via := match(nd, nv); ok {
								h := LeakHit{Table: t.name, Column: c, Row: id, Match: nd.text, Via: via}
								if via == "" {
									r.GoldHits = append(r.GoldHits, h)
								} else {
									r.GoldInUseQuestion = append(r.GoldInUseQuestion, h)
								}
							}
						}
					}
				}
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	// Retrieval queries and key texts must be use-half questions.
	rows, err := db.QueryContext(ctx, `SELECT id, query_text, delivered FROM retrievals ORDER BY id`)
	if err != nil {
		return nil, err
	}
	delivered := map[string]map[string]bool{}
	for rows.Next() {
		var id, qt, dl string
		if err := rows.Scan(&id, &qt, &dl); err != nil {
			rows.Close()
			return nil, err
		}
		r.Retrievals++
		if _, ok := useSet[normWords(qt)]; !ok {
			r.RetrievalsNotUse = append(r.RetrievalsNotUse, id+": "+qt)
		}
		var ids []string
		set := map[string]bool{}
		if json.Unmarshal([]byte(dl), &ids) == nil {
			for _, x := range ids {
				set[x] = true
			}
		}
		delivered[id] = set
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if kt != "" {
		idc := "rowid"
		for _, t := range ts {
			if t.name == kt && t.hasID {
				idc = "id"
			}
		}
		rows, err := db.QueryContext(ctx, "SELECT CAST("+idc+" AS TEXT), text FROM "+quoteIdent(kt)+" ORDER BY 1")
		if err != nil {
			return nil, fmt.Errorf("key table %s: %w", kt, err)
		}
		for rows.Next() {
			var id string
			var text sql.NullString
			if err := rows.Scan(&id, &text); err != nil {
				rows.Close()
				return nil, err
			}
			r.Keys++
			if _, ok := useSet[normWords(text.String)]; !ok {
				r.KeysNotUse = append(r.KeysNotUse, id+": "+text.String)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	// Outcomes: +-1, retrieval present, used ids among the delivered ids.
	rows, err = db.QueryContext(ctx, `SELECT id, retrieval_id, value, used_ids FROM outcomes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, rid, used string
		var v float64
		if err := rows.Scan(&id, &rid, &v, &used); err != nil {
			rows.Close()
			return nil, err
		}
		r.Outcomes++
		var why []string
		if v != 1 && v != -1 {
			why = append(why, fmt.Sprintf("value %v", v))
		}
		dl, ok := delivered[rid]
		if !ok {
			why = append(why, "retrieval "+rid+" missing")
		}
		var ids []string
		if err := json.Unmarshal([]byte(used), &ids); err != nil {
			why = append(why, "used_ids not a JSON array of ids")
		}
		for _, u := range ids {
			if ok && !dl[u] {
				why = append(why, "used id "+u+" not delivered")
			}
		}
		if len(why) > 0 {
			r.BadOutcomes = append(r.BadOutcomes, id+": "+strings.Join(why, "; "))
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, f := range []struct {
		n    int
		what string
	}{
		{len(r.TestQuestionIn), "test question text in the database"},
		{len(r.KeysNotUse), "key text that is not a use-half question"},
		{len(r.RetrievalsNotUse), "retrieval query that is not a use-half question"},
		{len(r.BadOutcomes), "outcome not +-1 or with used ids outside the delivered ids"},
		{len(r.JudgeOutput), "judge output in the database"},
		{len(r.GoldHits), "gold answer in keys, retrievals or outcomes, not inside a use-half question"},
	} {
		if f.n > 0 {
			r.Failures = append(r.Failures, fmt.Sprintf("%d %s", f.n, f.what))
		}
	}
	r.OK = len(r.Failures) == 0
	return r, nil
}
