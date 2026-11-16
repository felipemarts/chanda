package facts

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/env"
)

func serviceWith(t *testing.T, exts ...config.ExtractorConfig) (*Service, *env.Env) {
	t.Helper()
	e, _ := env.NewTest(t)
	e.Cfg.Write.Extractors = exts
	return New(e), e
}

func cands(cs []Candidate) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Subject+"|"+c.Predicate+"|"+c.Value)
	}
	return fmt.Sprint(out)
}

func TestDeterministicExtractors(t *testing.T) {
	cases := []struct {
		name string
		ext  config.ExtractorConfig
		ev   Event
		want string
	}{
		{
			name: "header: Key: value lines at the top, subject from title key",
			ext:  config.ExtractorConfig{Kind: KindHeader},
			ev:   Event{Content: "Title: Pricing Policy\nOwner: Maria Souza\nStatus: approved\n\nOwner: ignored in body"},
			want: "[Pricing Policy|owner|Maria Souza Pricing Policy|status|approved]",
		},
		{
			name: "header: after a heading, subject from the heading",
			ext:  config.ExtractorConfig{Kind: KindHeader},
			ev:   Event{Content: "# Alice\n\nJob Title: Engineer\nE-mail: alice@example.com\nFree text: not header\nthen prose"},
			want: "[Alice|job_title|Engineer Alice|e_mail|alice@example.com Alice|free_text|not header]",
		},
		{
			name: "header: front-matter, subject from configuration",
			ext:  config.ExtractorConfig{Kind: KindHeader, Subject: "Handbook"},
			ev:   Event{Content: "---\nowner: \"RH\"\nupdated: 2026-03-01\n  nested: skip\n---\nBody: not header"},
			want: "[Handbook|owner|RH Handbook|updated|2026-03-01]",
		},
		{
			name: "header: no subject anywhere yields nothing",
			ext:  config.ExtractorConfig{Kind: KindHeader},
			ev:   Event{Content: "Owner: Maria"},
			want: "[]",
		},
		{
			name: "header: no heading or title key, subject from the document file name",
			ext:  config.ExtractorConfig{Kind: KindHeader},
			ev:   Event{Content: "Owner: Maria\nStatus: draft", DocURI: "kb/projects/project-atlas.md"},
			want: "[project-atlas|owner|Maria project-atlas|status|draft]",
		},
		{
			name: "header: the file name only when nothing better names the subject",
			ext:  config.ExtractorConfig{Kind: KindHeader},
			ev:   Event{Content: "Title: Atlas\nOwner: Maria", DocURI: "kb/notes.md"},
			want: "[Atlas|owner|Maria]",
		},
		{
			name: "header: file name without extension, or the whole name when it has none",
			ext:  config.ExtractorConfig{Kind: KindHeader},
			ev:   Event{Content: "Owner: Maria", DocURI: "README"},
			want: "[README|owner|Maria]",
		},
		{
			name: "kv_table: no heading, subject from the document file name",
			ext:  config.ExtractorConfig{Kind: KindKVTable},
			ev:   Event{Content: "| key | value |\n|---|---|\n| owner | Maria |", DocURI: "docs/Plano Pro.txt"},
			want: "[Plano Pro|owner|Maria]",
		},
		{
			name: "header: subject from heading path, predicate filter",
			ext:  config.ExtractorConfig{Kind: KindHeader, Predicate: "Owner"},
			ev:   Event{Content: "Owner: Maria\nStatus: draft", HeadingPath: "Policies > Travel"},
			want: "[Travel|owner|Maria]",
		},
		{
			name: "header: prose with a colon is not a header",
			ext:  config.ExtractorConfig{Kind: KindHeader, Subject: "X"},
			ev:   Event{Content: "We decided the following on Monday, after a long discussion: ship it."},
			want: "[]",
		},
		{
			name: "kv_table: subject from the nearest heading, header row skipped",
			ext:  config.ExtractorConfig{Kind: KindKVTable},
			ev: Event{Content: "# Team\n## Bob\n\n| Campo | Valor |\n|---|:---:|\n| Cargo | Gerente |\n| Endereço | Rua A, 10 |\n\n" +
				"## Carol\n| k | v |\n| - | - |\n| Cargo | Analista |\n| bad | row | extra |"},
			want: "[Bob|cargo|Gerente Bob|endereco|Rua A, 10 Carol|cargo|Analista]",
		},
		{
			name: "kv_table: three columns are not a key-value table",
			ext:  config.ExtractorConfig{Kind: KindKVTable, Subject: "S"},
			ev:   Event{Content: "| a | b | c |\n|---|---|---|\n| 1 | 2 | 3 |"},
			want: "[]",
		},
		{
			name: "kv_table: subject from configuration",
			ext:  config.ExtractorConfig{Kind: KindKVTable, Subject: "Acme"},
			ev:   Event{Content: "| key | value |\n|-----|-------|\n| owner | Maria |\n| owner | Maria |"},
			want: "[Acme|owner|Maria]",
		},
		{
			name: "regex: named groups subject, predicate, value",
			ext:  config.ExtractorConfig{Kind: KindRegex, Pattern: `(?m)^(?P<subject>\w+)'s (?P<predicate>[\w ]+?) is (?P<value>[^.\n]+)\.?$`},
			ev:   Event{Content: "Alice's job title is Engineer.\nBob's phone is 555-1234"},
			want: "[Alice|job_title|Engineer Bob|phone|555-1234]",
		},
		{
			name: "regex: fixed predicate and subject",
			ext:  config.ExtractorConfig{Kind: KindRegex, Pattern: `preço atual: (?P<value>R\$ ?[\d.,]+)`, Predicate: "current_price", Subject: "Plano Pro"},
			ev:   Event{Content: "O preço atual: R$ 1.000,50 por mês"},
			want: "[Plano Pro|current_price|R$ 1.000,50]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := serviceWith(t, c.ext)
			if s.extErr != nil {
				t.Fatal(s.extErr)
			}
			if got := cands(s.Extract(c.ev)); got != c.want {
				t.Fatalf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestExtractorConfigValidation(t *testing.T) {
	bad := []config.ExtractorConfig{
		{Kind: "llm"},
		{Kind: KindRegex, Pattern: `(`},
		{Kind: KindRegex, Pattern: `(?P<subject>\w+) (?P<predicate>\w+)`},
		{Kind: KindRegex, Pattern: `(?P<value>\w+)`, Predicate: "p"},
		{Kind: KindRegex, Pattern: `(?P<subject>\w+) (?P<value>\w+)`},
	}
	for _, b := range bad {
		if err := ValidateExtractors([]config.ExtractorConfig{b}); core.CodeOf(err) != core.CodeInvalidArgument {
			t.Errorf("%+v: err=%v", b, err)
		}
		s, _ := serviceWith(t, b)
		if _, err := s.AfterEvent(t.Context(), nil, Event{}); err == nil {
			t.Errorf("%+v: AfterEvent must report the bad configuration", b)
		}
	}
}

func TestAfterEvent_AssertsCandidatesWithTheEventAsOrigin(t *testing.T) {
	f := newFixture(t)
	f.e.Cfg.Write.Extractors = []config.ExtractorConfig{{Name: "meta", Kind: KindHeader}, {Kind: KindKVTable}}
	f.s = New(f.e)
	f.exec(`INSERT INTO documents(id,namespace,source_id,uri,current_version,doc_date,content_hash,updated_at)
		VALUES('doc_1',?,'src_internal','file:a',1,'2026-02-01','h','t')`, testNS)
	content := "# Acme\nOwner: Maria\nStatus: active\n\n| k | v |\n|---|---|\n| Address | Rua B |"
	f.exec(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,doc_id,doc_version,ordinal)
		VALUES('evt_c1',?,'src_internal','chunk',?,'h','internal','t','2026-03-01T00:00:00.000000000Z','doc_1',1,0)`, testNS, content)
	ev := Event{ID: "evt_c1", Namespace: testNS, Content: content, TrustZone: core.ZoneInternal, DocDate: "2026-02-01"}
	var res []AssertResult
	f.tx(func(tx *sql.Tx) (err error) {
		res, err = f.s.AfterEvent(f.ctx, tx, ev)
		return err
	})
	if len(res) != 3 {
		t.Fatalf("results=%+v", res)
	}
	for _, r := range res {
		fc := f.fact(r.FactID)
		if r.Action != ActionInserted || fc.TrustZone != core.ZoneInternal || fc.ValidFrom != day("2026-02-01") {
			t.Errorf("fact %+v (%s)", fc, r.Action)
		}
		if n := f.count(`SELECT count(*) FROM provenance WHERE derived_id=? AND source_id='evt_c1' AND relation='derived_from'`, r.FactID); n != 1 {
			t.Errorf("missing derived_from edge for %s", r.FactID)
		}
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE actor='extractor:meta'`); n == 0 {
		t.Errorf("extractor actor not recorded")
	}
	// Running the hook again on the same event duplicates nothing.
	f.tx(func(tx *sql.Tx) (err error) {
		res, err = f.s.AfterEvent(f.ctx, tx, ev)
		return err
	})
	for _, r := range res {
		if r.Action != ActionConfirmed || r.Reason != ReasonAlreadyRecorded {
			t.Errorf("re-run: %+v", r)
		}
	}
	if n := f.count(`SELECT count(*) FROM facts`); n != 3 {
		t.Fatalf("facts=%d after re-run", n)
	}
	f.checkInvariants()
}
