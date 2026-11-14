package lme

import (
	"math"
	"sort"
	"strings"
)

// Dist summarizes a distribution.
type Dist struct {
	Min    float64 `json:"min"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	Max    float64 `json:"max"`
	Sum    float64 `json:"sum"`
}

// NewDist summarizes xs (nearest-rank percentiles).
func NewDist(xs []float64) Dist {
	if len(xs) == 0 {
		return Dist{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	var sum float64
	for _, x := range s {
		sum += x
	}
	return Dist{Min: s[0], Mean: sum / float64(len(s)), Median: Percentile(s, 50), P95: Percentile(s, 95), Max: s[len(s)-1], Sum: sum}
}

// Percentile is the nearest-rank percentile of sorted xs.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	r := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[max(0, min(len(sorted)-1, r-1))]
}

// UniqueTexts counts distinct texts and their size.
type UniqueTexts struct {
	Count       int     `json:"count"`
	Chars       int64   `json:"chars"`
	EstTokens   int64   `json:"est_tokens"`    // shared estimator, ceil(runes/4)
	E5Tokens    int64   `json:"e5_tokens_cap"` // est tokens, each capped at e5's 512-token window
	E5SimdSecs  float64 `json:"e5_simd_seconds_projected"`
	E5StockSecs float64 `json:"e5_stock_seconds_projected"`
}

type uniq struct {
	seen map[string]bool
	u    UniqueTexts
}

func newUniq() *uniq { return &uniq{seen: map[string]bool{}} }

func (u *uniq) add(text string) {
	h := SHA256(text)
	if u.seen[h] {
		return
	}
	u.seen[h] = true
	u.u.Count++
	u.u.Chars += int64(len([]rune(text)))
	t := int64(Tokens(text))
	u.u.EstTokens += t
	u.u.E5Tokens += min(t, 512)
	u.u.E5SimdSecs += E5Seconds(int(t), true)
	u.u.E5StockSecs += E5Seconds(int(t), false)
}

// E5Seconds projects the e5-small cost of embedding one text of tokens
// estimated tokens, from decision 0036 (darwin/arm64, busy machine, so an
// upper bound): simd 16 ms for ~20 tokens and 63 ms for a ~150-word (~200
// token) paragraph; stock 41 ms and 350 ms. Linear between those points
// (0036: "cost grows linearly with tokens"), extrapolated linearly above
// 200 tokens up to the 512-token window. The 200-token slope: simd
// (63-16)/180 = 0.261 ms/token, stock (350-41)/180 = 1.717 ms/token.
func E5Seconds(tokens int, simd bool) float64 {
	t := float64(min(max(tokens, 1), 512))
	if simd {
		return (16 + (t-20)*0.261) / 1000
	}
	return (41 + (t-20)*1.717) / 1000
}

// DatasetStats is what DATASET.md reports.
type DatasetStats struct {
	Questions           int            `json:"questions"`
	ByType              map[string]int `json:"by_type"`
	AbstentionByType    map[string]int `json:"abstention_by_type"`
	Abstention          int            `json:"abstention"`
	SessionsPerQuestion Dist           `json:"sessions_per_question"`
	TurnsPerQuestion    Dist           `json:"turns_per_question"`
	CharsPerQuestion    Dist           `json:"chars_per_question"`
	EvidencePerQuestion Dist           `json:"evidence_sessions_per_question"`
	TotalChars          int64          `json:"total_chars_all_questions"`
	TotalTurns          int64          `json:"total_turns_all_questions"`
	TotalSessions       int64          `json:"total_sessions_all_questions"`
	UniqueSessionIDs    int            `json:"unique_session_ids"`
	UniqueSessions      UniqueTexts    `json:"unique_session_texts"`
	UniqueTurns         UniqueTexts    `json:"unique_turn_texts"`
	UniqueChunks        UniqueTexts    `json:"unique_rag_chunks"`
	UniqueQuestions     UniqueTexts    `json:"unique_question_texts"`

	// Consistency checks between the README and the data.
	AnswerIDsMatchAnswerPrefix int            `json:"answer_session_ids_equal_ids_containing_answer"`
	AnswerIDsNotInHaystack     int            `json:"answer_session_ids_missing_from_haystack"`
	NoOfficialTarget           []string       `json:"questions_without_official_target"`
	NoOfficialTargetNonAbs     int            `json:"non_abstention_without_official_target"`
	NoOfficialTargetByType     map[string]int `json:"non_abstention_without_official_target_by_type"`
	OfficialTargetsDiffer      int            `json:"official_targets_differ_from_answer_session_ids"`
	HasAnswerOutsideAnswerIDs  int            `json:"has_answer_turns_in_sessions_without_answer_in_id"`
	SessionsAfterQuestionDate  int            `json:"haystack_sessions_dated_after_question"`
	QuestionsWithLaterSessions int            `json:"questions_with_sessions_dated_after_question"`
	EvidenceAfterQuestionDate  int            `json:"evidence_sessions_dated_after_question"`
	MaxDaysAfterQuestion       float64        `json:"max_days_session_after_question"`
	UnsortedHaystacks          int            `json:"haystacks_not_sorted_by_date"`
	AbstentionWithEvidence     int            `json:"abstention_with_answer_session_ids"`
	Roles                      map[string]int `json:"turn_roles"`
}

// ComputeStats streams path and measures it.
func ComputeStats(path string) (*DatasetStats, error) { return ComputeStatsFor(path, nil) }

// ComputeStatsFor measures only the questions in only (all when nil).
func ComputeStatsFor(path string, only map[string]bool) (*DatasetStats, error) {
	st := &DatasetStats{ByType: map[string]int{}, AbstentionByType: map[string]int{}, Roles: map[string]int{}, NoOfficialTargetByType: map[string]int{}}
	var sess, turns, chars, evid []float64
	ids := map[string]bool{}
	us, ut, uc, uq := newUniq(), newUniq(), newUniq(), newUniq()
	err := Select(path, only, func(q *Question) error {
		st.Questions++
		st.ByType[q.QuestionType]++
		if IsAbstention(q.QuestionID) {
			st.Abstention++
			st.AbstentionByType[q.QuestionType]++
			if len(q.AnswerSessionIDs) > 0 {
				st.AbstentionWithEvidence++
			}
		}
		uq.add(q.Question)
		qd, _ := ParseDate(q.QuestionDate)
		nt, nc := 0, 0
		inHay := map[string]bool{}
		var withAnswer []string
		later := false
		ev := set(q.AnswerSessionIDs)
		for i, id := range q.HaystackSessionIDs {
			ids[id] = true
			inHay[id] = true
			if strings.Contains(id, "answer") {
				withAnswer = append(withAnswer, id)
			}
			d, _ := ParseDate(q.HaystackDates[i])
			if d.After(qd) {
				st.SessionsAfterQuestionDate++
				later = true
				if ev[id] {
					st.EvidenceAfterQuestionDate++
				}
				st.MaxDaysAfterQuestion = max(st.MaxDaysAfterQuestion, d.Sub(qd).Hours()/24)
			}
			var b strings.Builder
			for _, t := range q.HaystackSessions[i] {
				st.Roles[t.Role]++
				nt++
				nc += len([]rune(t.Content))
				ut.add(t.Content)
				b.WriteString(TurnLine(t))
				b.WriteByte('\n')
				if t.HasAnswer && !strings.Contains(id, "answer") {
					st.HasAnswerOutsideAnswerIDs++
				}
			}
			us.add(b.String())
			for _, c := range ChunkSession(i, q.HaystackSessions[i], ChunkChars) {
				uc.add(c.Text)
			}
		}
		if later {
			st.QuestionsWithLaterSessions++
		}
		for i := 1; i < len(q.HaystackDates); i++ {
			a, _ := ParseDate(q.HaystackDates[i-1])
			b, _ := ParseDate(q.HaystackDates[i])
			if b.Before(a) {
				st.UnsortedHaystacks++
				break
			}
		}
		for _, id := range q.AnswerSessionIDs {
			if !inHay[id] {
				st.AnswerIDsNotInHaystack++
			}
		}
		if sameSet(withAnswer, q.AnswerSessionIDs) {
			st.AnswerIDsMatchAnswerPrefix++
		}
		off := OfficialTargets(q)
		if len(off) == 0 {
			st.NoOfficialTarget = append(st.NoOfficialTarget, q.QuestionID)
			if !IsAbstention(q.QuestionID) {
				st.NoOfficialTargetNonAbs++
				st.NoOfficialTargetByType[q.QuestionType]++
			}
		}
		if !IsAbstention(q.QuestionID) && len(off) > 0 && !sameSet(off, q.AnswerSessionIDs) {
			st.OfficialTargetsDiffer++
		}
		sess = append(sess, float64(len(q.HaystackSessionIDs)))
		turns = append(turns, float64(nt))
		chars = append(chars, float64(nc))
		evid = append(evid, float64(len(q.AnswerSessionIDs)))
		st.TotalChars += int64(nc)
		st.TotalTurns += int64(nt)
		st.TotalSessions += int64(len(q.HaystackSessionIDs))
		return nil
	})
	if err != nil {
		return nil, err
	}
	st.SessionsPerQuestion, st.TurnsPerQuestion, st.CharsPerQuestion, st.EvidencePerQuestion = NewDist(sess), NewDist(turns), NewDist(chars), NewDist(evid)
	st.UniqueSessionIDs = len(ids)
	st.UniqueSessions, st.UniqueTurns, st.UniqueChunks, st.UniqueQuestions = us.u, ut.u, uc.u, uq.u
	return st, nil
}

func sameSet(a, b []string) bool {
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	n := map[string]bool{}
	for _, x := range b {
		n[x] = true
	}
	if len(m) != len(n) {
		return false
	}
	for x := range m {
		if !n[x] {
			return false
		}
	}
	return true
}
