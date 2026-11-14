package lme

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const tiny = "testdata/tiny.json"

func loadTiny(t *testing.T) []*Question {
	t.Helper()
	var qs []*Question
	if err := Each(tiny, func(q *Question) error { qs = append(qs, q); return nil }); err != nil {
		t.Fatal(err)
	}
	return qs
}

func TestLoaderTinyFixture(t *testing.T) {
	qs := loadTiny(t)
	if len(qs) != 3 {
		t.Fatalf("%d questions", len(qs))
	}
	q := qs[0]
	if q.QuestionID != "q1" || q.QuestionType != "single-session-user" || len(q.HaystackSessions) != 3 {
		t.Fatalf("q1 = %+v", q)
	}
	if !q.HaystackSessions[1][0].HasAnswer || q.HaystackSessions[1][1].HasAnswer {
		t.Fatal("has_answer not decoded")
	}
	if got := qs[1].AnswerText(); got != "2" {
		t.Fatalf("numeric answer = %q", got)
	}
	if got := q.AnswerText(); got != "Business Administration" {
		t.Fatalf("answer = %q", got)
	}
	if IsAbstention("q1") || !IsAbstention("q3_abs") || IsAbstention("q_abs_x") {
		t.Fatal("IsAbstention")
	}
}

func TestLoaderRejectsMismatchedHaystack(t *testing.T) {
	bad := `[{"question_id":"x","question_type":"t","question":"q?","question_date":"2023/05/30 (Tue) 23:40",
	  "answer":"a","answer_session_ids":[],"haystack_dates":["2023/05/20 (Sat) 02:21"],
	  "haystack_session_ids":["s1","s2"],"haystack_sessions":[[],[]]}]`
	err := EachReader(strings.NewReader(bad), func(*Question) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "2 session ids, 1 dates") {
		t.Fatalf("err = %v", err)
	}
	if err := EachReader(strings.NewReader(`{"a":1}`), func(*Question) error { return nil }); err == nil {
		t.Fatal("object accepted")
	}
}

func TestParseDate(t *testing.T) {
	got, err := RFC3339("2023/05/20 (Sat) 02:21")
	if err != nil || got != "2023-05-20T02:21:00Z" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ParseDate("2023-05-20"); err == nil {
		t.Fatal("wrong layout accepted")
	}
}

func TestOfficialTargets(t *testing.T) {
	qs := loadTiny(t)
	if got := OfficialTargets(qs[0]); !reflect.DeepEqual(got, []string{"answer_a1"}) {
		t.Fatalf("q1 targets %v", got)
	}
	// q2's evidence is an assistant turn: the official code renames the
	// session "noans" and the question has no target.
	if got := OfficialTargets(qs[1]); len(got) != 0 {
		t.Fatalf("q2 targets %v", got)
	}
}

func TestChronologicalOrder(t *testing.T) {
	q := loadTiny(t)[0]
	if got := ChronologicalOrder(q); !reflect.DeepEqual(got, []int{0, 2, 1}) {
		t.Fatalf("order %v", got)
	}
}

func TestChunkSession(t *testing.T) {
	long := strings.Repeat("This sentence is about forty characters. ", 60)
	turns := []Turn{{Role: "user", Content: "short question"}, {Role: "assistant", Content: long}, {Role: "user", Content: "thanks"}}
	cs := ChunkSession(7, turns, 300)
	var all strings.Builder
	for i, c := range cs {
		if c.Session != 7 || c.Ordinal != i {
			t.Fatalf("chunk %d = %+v", i, c)
		}
		if n := utf8.RuneCountInString(c.Text); n > 300 {
			t.Fatalf("chunk %d has %d runes", i, n)
		}
		all.WriteString(c.Text)
	}
	// No text is lost (whitespace aside).
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	want := squash("user: short question" + "assistant: " + long + "user: thanks")
	if squash(all.String()) != want {
		t.Fatal("chunks do not cover the session text")
	}
	// The long turn's first piece does not fit next to the question: the
	// question is a chunk of its own, the turn starts the next one.
	if cs[0].Text != "user: short question" || !strings.HasPrefix(cs[1].Text, "assistant: This sentence") {
		t.Fatalf("chunks %q / %q", cs[0].Text, cs[1].Text)
	}
	if got := ChunkSession(0, turns[:1], 300); len(got) != 1 || got[0].Text != "user: short question" {
		t.Fatalf("%v", got)
	}
	short := ChunkSession(0, []Turn{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"}}, 300)
	if len(short) != 1 || short[0].Text != "user: a\nassistant: b" {
		t.Fatalf("%v", short)
	}
}

func TestQueryTermsMatchChanda(t *testing.T) {
	got := QueryTerms("What degree did I graduate with? Ação, ação e 2 cafés", MaxQueryTerms)
	want := []string{"degree", "graduate", "acao", "2", "cafes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terms %v", got)
	}
	if MatchExpr(got) != `"degree" OR "graduate" OR "acao" OR "2" OR "cafes"` {
		t.Fatal(MatchExpr(got))
	}
}

func TestTokens(t *testing.T) {
	for s, n := range map[string]int{"": 0, "a": 1, "abcd": 1, "abcde": 2, "ação": 1} {
		if Tokens(s) != n {
			t.Fatalf("Tokens(%q) = %d", s, Tokens(s))
		}
	}
}

func TestStatsTiny(t *testing.T) {
	st, err := ComputeStats(tiny)
	if err != nil {
		t.Fatal(err)
	}
	if st.Questions != 3 || st.Abstention != 1 || st.UniqueSessionIDs != 5 || st.TotalSessions != 7 {
		t.Fatalf("%+v", st)
	}
	// sharegpt_x1 appears in all three haystacks with the same content.
	if st.UniqueSessions.Count != 5 || st.UniqueTurns.Count != 10 {
		t.Fatalf("unique sessions %d turns %d", st.UniqueSessions.Count, st.UniqueTurns.Count)
	}
	if st.UnsortedHaystacks != 1 || st.NoOfficialTargetNonAbs != 1 || st.AnswerIDsMatchAnswerPrefix != 3 {
		t.Fatalf("%+v", st)
	}
}

func TestBM25Tiny(t *testing.T) {
	for _, unit := range []string{UnitSession, UnitTurn} {
		r, err := RunBM25(t.Context(), loadTiny(t)[0], unit)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Ranked) == 0 || r.Ranked[0] != 1 {
			t.Fatalf("%s: ranked %v, want the degree session first", unit, r.Ranked)
		}
	}
}

func TestRAGTinyFitsBudget(t *testing.T) {
	q := loadTiny(t)[0]
	emb, err := OpenEmbedder(t.Context(), EmbedOptions{Kind: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []int{2000, 60} {
		r, err := RunRAG(t.Context(), q, emb, budget)
		if err != nil {
			t.Fatal(err)
		}
		if r.Tokens > budget || Tokens(r.Context) != r.Tokens {
			t.Fatalf("budget %d: %d tokens", budget, r.Tokens)
		}
		if r.Ranked[0] != 1 {
			t.Fatalf("ranked %v", r.Ranked)
		}
		if budget == 2000 {
			// Everything fits: three sessions, rendered in date order.
			if len(r.InBudget) != 3 {
				t.Fatalf("in budget %v", r.InBudget)
			}
			i0 := strings.Index(r.Context, "2023/05/20")
			i1 := strings.Index(r.Context, "2023/05/21")
			i2 := strings.Index(r.Context, "2023/05/22")
			if !(i0 < i1 && i1 < i2) || !strings.Contains(r.Context, "### Session 1:\nSession Date: 2023/05/20 (Sat) 02:21\nSession Content:\nuser: Can you") {
				t.Fatalf("context:\n%s", r.Context)
			}
			if strings.Contains(r.Context, "answer_") {
				t.Fatal("dataset session id leaked into the context")
			}
		}
	}
}

func ExampleTokens() {
	fmt.Println(Tokens("ceil(runes / 4)"))
	// Output: 4
}

func TestMain(m *testing.M) {
	// The integration test re-runs this test binary as the chanda server.
	if os.Getenv("LME_TEST_CHANDA") == "1" {
		os.Exit(runChandaForTest())
	}
	os.Exit(m.Run())
}
