package lme

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LoCoMo (snap-research/locomo, data/locomo10.json; eval/locomo/SOURCE.md)
// for the sleep transfer test (eval/sleep/PROTOCOL.md). Only the turns, the
// speaker names and the session dates are read; observation,
// session_summary and event_summary are never decoded.

// LocomoSHA256 is the pinned locomo10.json (SOURCE.md).
const LocomoSHA256 = "79fa87e90f04081343b8c8debecb80a9a6842b76a7aa537dc9fdf651ea698ff4"

// LocomoCategoryNames maps the QA category integers (SOURCE.md mapping).
var LocomoCategoryNames = map[int]string{1: "multi-hop", 2: "temporal", 3: "open-domain", 4: "single-hop", 5: "adversarial"}

// LocomoTurn is one dialogue turn.
type LocomoTurn struct {
	Speaker     string `json:"speaker"`
	DiaID       string `json:"dia_id"`
	Text        string `json:"text"`
	BlipCaption string `json:"blip_caption,omitempty"`
}

// LocomoSession is session_<N> with its session_<N>_date_time.
type LocomoSession struct {
	N        int          // 1-based session number
	DateTime string       // as in the file, e.g. "1:56 pm on 8 May, 2023"
	Date     time.Time    // DateTime parsed (ParseLocomoDate)
	Turns    []LocomoTurn // in order
}

// LocomoQA is one QA item. Answer is a string or a number; category 5
// items usually have none.
type LocomoQA struct {
	Question string          `json:"question"`
	Answer   json.RawMessage `json:"answer"`
	Evidence []string        `json:"evidence"`
	Category int             `json:"category"`
}

// AnswerText is the gold answer as text (numbers as given, "" when absent).
func (q *LocomoQA) AnswerText() string {
	if len(q.Answer) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(q.Answer, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(q.Answer))
}

// LocomoConversation is one sample.
type LocomoConversation struct {
	SampleID string
	SpeakerA string
	SpeakerB string
	Sessions []LocomoSession
	QA       []LocomoQA
	// IgnoredDateKeys lists session_<N>_date_time keys with no session_<N>
	// (conv-26 has session_20 to session_35).
	IgnoredDateKeys []string
	// dia: dia_id -> (session index in Sessions, turn index)
	dia map[string][2]int
}

var (
	reSessionKey  = regexp.MustCompile(`^session_(\d+)$`)
	reSessionDate = regexp.MustCompile(`^session_(\d+)_date_time$`)
)

// LoadLocomo reads every conversation of locomo10.json, in file order.
func LoadLocomo(path string) ([]*LocomoConversation, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		SampleID     string                     `json:"sample_id"`
		Conversation map[string]json.RawMessage `json:"conversation"`
		QA           []LocomoQA                 `json:"qa"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := make([]*LocomoConversation, 0, len(raw))
	for _, r := range raw {
		c, err := parseLocomoConversation(r.SampleID, r.Conversation)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.SampleID, err)
		}
		c.QA = r.QA
		out = append(out, c)
	}
	return out, nil
}

// FindLocomo returns the conversation with sample_id id.
func FindLocomo(convs []*LocomoConversation, id string) (*LocomoConversation, error) {
	for _, c := range convs {
		if c.SampleID == id {
			return c, nil
		}
	}
	return nil, fmt.Errorf("locomo: no conversation %q", id)
}

func parseLocomoConversation(id string, m map[string]json.RawMessage) (*LocomoConversation, error) {
	c := &LocomoConversation{SampleID: id, dia: map[string][2]int{}}
	if err := json.Unmarshal(m["speaker_a"], &c.SpeakerA); err != nil {
		return nil, fmt.Errorf("speaker_a: %w", err)
	}
	if err := json.Unmarshal(m["speaker_b"], &c.SpeakerB); err != nil {
		return nil, fmt.Errorf("speaker_b: %w", err)
	}
	if c.SpeakerA == "" || c.SpeakerB == "" || c.SpeakerA == c.SpeakerB {
		return nil, fmt.Errorf("speakers %q and %q", c.SpeakerA, c.SpeakerB)
	}
	sessions := map[int][]LocomoTurn{}
	dates := map[int]string{}
	for k, v := range m {
		if mm := reSessionKey.FindStringSubmatch(k); mm != nil {
			n, _ := strconv.Atoi(mm[1])
			var turns []LocomoTurn
			if err := json.Unmarshal(v, &turns); err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			sessions[n] = turns
		} else if mm := reSessionDate.FindStringSubmatch(k); mm != nil {
			n, _ := strconv.Atoi(mm[1])
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			dates[n] = s
		}
	}
	var ns []int
	for n := range sessions {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	for i, n := range ns {
		if n != i+1 {
			return nil, fmt.Errorf("sessions are not contiguous from 1: %v", ns)
		}
		ds, ok := dates[n]
		if !ok {
			return nil, fmt.Errorf("session_%d has no session_%d_date_time", n, n)
		}
		t, err := ParseLocomoDate(ds)
		if err != nil {
			return nil, fmt.Errorf("session_%d: %w", n, err)
		}
		s := LocomoSession{N: n, DateTime: ds, Date: t, Turns: sessions[n]}
		for j, tr := range s.Turns {
			if tr.Speaker != c.SpeakerA && tr.Speaker != c.SpeakerB {
				return nil, fmt.Errorf("%s: speaker %q is neither %q nor %q", tr.DiaID, tr.Speaker, c.SpeakerA, c.SpeakerB)
			}
			if _, dup := c.dia[tr.DiaID]; dup || tr.DiaID == "" {
				return nil, fmt.Errorf("dia_id %q empty or repeated", tr.DiaID)
			}
			c.dia[tr.DiaID] = [2]int{i, j}
		}
		c.Sessions = append(c.Sessions, s)
	}
	for n := range dates {
		if _, ok := sessions[n]; !ok {
			c.IgnoredDateKeys = append(c.IgnoredDateKeys, fmt.Sprintf("session_%d_date_time", n))
		}
	}
	sort.Strings(c.IgnoredDateKeys)
	return c, nil
}

// reLocomoDate is the date_time form of every session in locomo10.json:
// "1:56 pm on 8 May, 2023", "10:37 am on 27 June, 2023".
var reLocomoDate = regexp.MustCompile(`(?i)^\s*(\d{1,2}):(\d{2})\s*([ap])\.?m\.?\s+on\s+(\d{1,2})\s+([a-z]+)\.?,?\s+(\d{4})\s*$`)

var monthNames = map[string]time.Month{}

func init() {
	for m := time.January; m <= time.December; m++ {
		full := strings.ToLower(m.String())
		monthNames[full] = m
		monthNames[full[:3]] = m
	}
	monthNames["sept"] = time.September
}

// ParseLocomoDate parses a session date_time. The dataset has no time zone:
// the wall-clock time is kept as is and labelled UTC (the protocol's "local
// time, stored as UTC"; the battery reads LongMemEval dates the same way),
// so the date Chanda shows is the date of the dataset, whatever the zone of
// the machine.
func ParseLocomoDate(s string) (time.Time, error) {
	m := reLocomoDate.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, fmt.Errorf("locomo date %q: want \"H:MM am|pm on D Month, YYYY\"", s)
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[4])
	y, _ := strconv.Atoi(m[6])
	mon, ok := monthNames[strings.ToLower(m[5])]
	if !ok || h < 1 || h > 12 || mi > 59 {
		return time.Time{}, fmt.Errorf("locomo date %q: bad month, hour or minute", s)
	}
	if h == 12 {
		h = 0
	}
	if strings.EqualFold(m[3], "p") {
		h += 12
	}
	t := time.Date(y, mon, d, h, mi, 0, 0, time.UTC)
	if t.Day() != d {
		return time.Time{}, fmt.Errorf("locomo date %q: day out of range", s)
	}
	return t, nil
}

// LocomoContent is the event content of a turn (PROTOCOL.md "Ingestion"):
// "<Speaker>: <text>", plus " [shares a photo: <blip_caption>]" when the
// turn has a caption.
func LocomoContent(t LocomoTurn) string {
	s := t.Speaker + ": " + t.Text
	if t.BlipCaption != "" {
		s += " [shares a photo: " + t.BlipCaption + "]"
	}
	return s
}

// Role is the Chanda role of a turn: speaker_a is the user, speaker_b the
// assistant.
func (c *LocomoConversation) Role(t LocomoTurn) string {
	if t.Speaker == c.SpeakerA {
		return "user"
	}
	return "assistant"
}

// SessionID is the Chanda session id of session index i: <sample_id>-s<n>.
func (c *LocomoConversation) SessionID(i int) string {
	return fmt.Sprintf("%s-s%d", c.SampleID, c.Sessions[i].N)
}

// Turn returns the turn of a dia_id and its (session index, turn index).
func (c *LocomoConversation) Turn(dia string) (LocomoTurn, [2]int, bool) {
	k, ok := c.dia[dia]
	if !ok {
		return LocomoTurn{}, k, false
	}
	return c.Sessions[k[0]].Turns[k[1]], k, true
}

// HasDia reports whether dia is a turn of c.
func (c *LocomoConversation) HasDia(dia string) bool { _, ok := c.dia[dia]; return ok }

// reEvidenceID is one evidence id after splitting: "D3:12", also the
// malformed "D:11:26" and the zero-padded "D30:05".
var reEvidenceID = regexp.MustCompile(`^D:?(\d+):0*(\d+)$`)

// EvidenceParse is how one question's evidence list resolved.
type EvidenceParse struct {
	Valid   []string `json:"valid"`             // dia_ids of existing turns, in order, distinct
	Invalid []string `json:"invalid,omitempty"` // pieces that are not ids, or ids of no turn
	Fixed   []string `json:"fixed,omitempty"`   // raw entries that needed splitting or normalising
}

// ParseEvidence resolves a QA evidence list (PROTOCOL.md "Measures"; the
// malformed and dangling entries of SOURCE.md). An entry may hold several
// ids separated by ";", "," or spaces; each piece is normalised (D:11:26 ->
// D11:26, D30:05 -> D30:5) and kept when the turn exists. A piece that is
// not an id ("D") or names no turn (D10:19 in conv-42) is skipped.
func ParseEvidence(raw []string, exists func(string) bool) EvidenceParse {
	var p EvidenceParse
	seen := map[string]bool{}
	for _, e := range raw {
		pieces := strings.FieldsFunc(e, func(r rune) bool { return r == ';' || r == ',' || r == ' ' || r == '\t' || r == '\n' })
		changed := len(pieces) != 1
		for _, pc := range pieces {
			m := reEvidenceID.FindStringSubmatch(pc)
			if m == nil {
				p.Invalid = append(p.Invalid, pc)
				changed = true
				continue
			}
			s, _ := strconv.Atoi(m[1])
			t, _ := strconv.Atoi(m[2])
			id := fmt.Sprintf("D%d:%d", s, t)
			if id != pc {
				changed = true
			}
			if !exists(id) {
				p.Invalid = append(p.Invalid, pc)
				continue
			}
			if !seen[id] {
				seen[id] = true
				p.Valid = append(p.Valid, id)
			}
		}
		if len(pieces) == 0 {
			p.Invalid = append(p.Invalid, e)
			changed = true
		}
		if changed {
			p.Fixed = append(p.Fixed, e)
		}
	}
	return p
}

// LocomoQuestionID names QA item index of a conversation.
func LocomoQuestionID(sampleID string, index int) string {
	return fmt.Sprintf("%s-q%d", sampleID, index)
}

// AsQuestion renders the conversation as a LongMemEval-shaped haystack for
// the plain-RAG baseline (chunk.go, baselines.go): one haystack session per
// LoCoMo session, turns with the Chanda role and content, the session's
// date_time verbatim as its date. has_answer marks the turns of evidence
// (valid dia_ids). The renderer orders sessions by date and falls back to
// the haystack index when a date does not parse as a LongMemEval date,
// which is the LoCoMo order (sessions are chronological in the file).
func (c *LocomoConversation) AsQuestion(id, question string, evidence []string) *Question {
	q := &Question{QuestionID: id, QuestionType: "locomo", Question: question}
	ev := map[string]bool{}
	for _, d := range evidence {
		ev[d] = true
	}
	for i, s := range c.Sessions {
		q.HaystackSessionIDs = append(q.HaystackSessionIDs, c.SessionID(i))
		q.HaystackDates = append(q.HaystackDates, s.DateTime)
		turns := make([]Turn, len(s.Turns))
		for j, t := range s.Turns {
			turns[j] = Turn{Role: c.Role(t), Content: LocomoContent(t), HasAnswer: ev[t.DiaID]}
		}
		q.HaystackSessions = append(q.HaystackSessions, turns)
	}
	return q
}
