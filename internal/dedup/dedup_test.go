package dedup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
)

func TestNearDuplicateGuard(t *testing.T) {
	const thr = 0.95
	cases := []struct {
		name    string
		a, b    string
		cosine  float64
		aliases []string
		want    bool
		reason  string
	}{
		{"different numbers are an update, not a copy", "The price is 10", "the price is 12", 0.99, nil, false, ReasonNumbersDiffer},
		{"negation in Portuguese is a contradiction", "O preço é 10", "O preço não é 10", 0.99, nil, false, ReasonNegationDiffers},
		{"negation in English", "Alice is the owner", "Alice isn't the owner", 0.99, nil, false, ReasonNegationDiffers},
		{"nunca vs jamais: marker kinds differ", "Ele nunca aprova", "Ele jamais aprova", 0.99, nil, false, ReasonNegationDiffers},
		{"same content, different case and spacing", "The price is 10", "the  price is 10", 0.99, nil, true, ReasonNearDuplicate},
		{"numbers compared after normalization", "Custa R$ 1.000,50 por mês", "custa R$ 1000.50 por mês", 0.97, nil, true, ReasonNearDuplicate},
		{"dates compared after normalization", "Starts on 25/12/2026", "starts on 2026-12-25", 0.97, nil, true, ReasonNearDuplicate},
		{"different dates", "Starts on 2026-12-25", "Starts on 2026-12-26", 0.99, nil, false, ReasonDatesDiffer},
		{"ambiguous date digits still compared as numbers", "Due 01/03/2026", "Due 03/01/2026", 0.99, nil, false, ReasonNumbersDiffer},
		{"different entities", "Then Alice approved it", "Then Bob approved it", 0.99, nil, false, ReasonEntitiesDiffer},
		{"sentence-initial capital is not an entity", "Prices rose", "prices rose", 0.99, nil, true, ReasonNearDuplicate},
		{"known alias at sentence start is an entity", "Alice approved", "Bob approved", 0.99, []string{"Alice", "Bob"}, false, ReasonEntitiesDiffer},
		{"acronyms are entities", "ACME raised prices", "IBM raised prices", 0.99, nil, false, ReasonEntitiesDiffer},
		{"possessive does not change the entity", "Then Alice's team shipped", "then Alice team shipped", 0.99, nil, true, ReasonNearDuplicate},
		{"at the threshold is not above it", "The price is 10", "The price is 10", 0.95, nil, false, ReasonBelowThreshold},
		{"below the threshold", "a", "a", 0.5, nil, false, ReasonBelowThreshold},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := IsNearDuplicate(c.a, c.b, c.cosine, thr, c.aliases...)
			if got != c.want || reason != c.reason {
				t.Fatalf("got %v %s want %v %s\nsig a=%+v\nsig b=%+v", got, reason, c.want, c.reason,
					Signature(c.a, c.aliases...), Signature(c.b, c.aliases...))
			}
		})
	}
}

// Decision 0138: the lexical path collapses a newer text that repeats every
// word of the older one and adds at most MaxAddedWords, above a cosine floor
// lower than the plain threshold; the signature still decides.
func TestLexicalPath(t *testing.T) {
	const thr = 0.98
	lex := Lexical{MaxAddedWords: 3, MinCosine: 0.9}
	base := "A reunião de planejamento acontece toda segunda-feira na sala 4."
	cases := []struct {
		name         string
		newer, older string
		cosine       float64
		lex          Lexical
		want         bool
		reason       string
	}{
		{"a reminder word appended", base + " Lembrete.", base, 0.93, lex, true, ReasonNearDuplicate},
		{"case, spacing and punctuation only", "a  reunião de planejamento acontece toda segunda feira na sala 4", base, 0.95, lex, true, ReasonNearDuplicate},
		{"three words appended", base + " Até mais, obrigado.", base, 0.93, lex, true, ReasonNearDuplicate},
		{"four words appended", base + " Até mais, muito obrigado.", base, 0.93, lex, false, ReasonBelowThreshold},
		{"one word substituted", strings.Replace(base, "segunda", "terça", 1), base, 0.97, lex, false, ReasonBelowThreshold},
		{"the newer text drops a word of the older", strings.Replace(base, " de planejamento", "", 1), base, 0.95, lex, false, ReasonBelowThreshold},
		{"at the floor is not above it", base + " Lembrete.", base, 0.9, lex, false, ReasonBelowThreshold},
		{"the plain threshold still applies alone", strings.Replace(base, "segunda", "terça", 1), base, 0.99, lex, true, ReasonNearDuplicate},
		{"off: plain rule only", base + " Lembrete.", base, 0.93, Lexical{}, false, ReasonBelowThreshold},
		{"signature still decides: a number appended", base + " Sala 5.", base, 0.95, lex, false, ReasonNumbersDiffer},
		{"signature still decides: a negation appended", base + " Não.", base, 0.95, lex, false, ReasonNegationDiffers},
		{"signature still decides: an entity appended", base + " Com a Ana.", base, 0.95, lex, false, ReasonEntitiesDiffer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := IsNearDuplicateOf(c.newer, c.older, c.cosine, thr, c.lex)
			if got != c.want || reason != c.reason {
				t.Fatalf("got %v %s want %v %s", got, reason, c.want, c.reason)
			}
		})
	}
	if n, ok := Added("a b a", "A, b; a c!"); !ok || n != 1 {
		t.Fatalf("Added = %d %v, want 1 true", n, ok)
	}
	if _, ok := Added("a a b", "a b c"); ok {
		t.Fatal("words are counted with repetition")
	}
}

func TestSignature(t *testing.T) {
	s := Signature("Em 01/02/2026 a ACME e o João não pagaram 1.500 a Maria, nem 20.", "joao")
	want := Sig{
		Numbers:   []string{"01", "02", "2026", "1500", "20"}, // 01/02/2026 is ambiguous: kept as numbers
		Entities:  []string{"acme", "joao", "maria"},
		Negations: []string{"nao", "nem"},
	}
	if fmt.Sprintf("%v", s) != fmt.Sprintf("%v", want) {
		t.Fatalf("got  %+v\nwant %+v", s, want)
	}
}

func seed(t *testing.T, e *env.Env) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES('src_1','ns','user_statement','u','user','t')`,
		`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at) VALUES('evt_1','ns','src_1','statement','a','h1','user','a','t')`,
		`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at) VALUES('evt_2','ns','src_1','statement','b','h2','user','a','t')`,
	} {
		if _, err := e.Store.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkNearDuplicate_EdgeNotDeletion(t *testing.T) {
	e, _ := env.NewTest(t)
	ctx := context.Background()
	seed(t, e)
	g := New(e)
	mark := func(ns string, nt core.RecordType, nid string, et core.RecordType, eid string) (int64, error) {
		var id int64
		err := e.Store.Tx(ctx, func(tx *sql.Tx) (err error) {
			id, err = g.Mark(ctx, tx, ns, nt, nid, et, eid)
			return err
		})
		return id, err
	}
	id, err := mark("ns", core.RecEvent, "evt_2", core.RecEvent, "evt_1")
	if err != nil || id == 0 {
		t.Fatalf("mark: %d %v", id, err)
	}
	var n int
	e.Store.DB().QueryRow(`SELECT count(*) FROM provenance WHERE derived_id='evt_2' AND source_id='evt_1' AND relation='duplicate_of'`).Scan(&n)
	if n != 1 {
		t.Fatalf("duplicate_of edge missing")
	}
	e.Store.DB().QueryRow(`SELECT count(*) FROM events`).Scan(&n)
	if n != 2 {
		t.Fatalf("a near-duplicate must not be deleted")
	}
	if id, err := mark("ns", core.RecEvent, "evt_2", core.RecEvent, "evt_1"); err != nil || id != 0 {
		t.Fatalf("second mark must be a no-op: %d %v", id, err)
	}
	cases := []struct {
		name string
		ns   string
		nid  string
		eid  string
		nt   core.RecordType
		code core.Code
	}{
		{"self", "ns", "evt_1", "evt_1", core.RecEvent, core.CodeInvalidArgument},
		{"reverse of an existing edge", "ns", "evt_1", "evt_2", core.RecEvent, core.CodeConflict},
		{"unknown record", "ns", "evt_9", "evt_1", core.RecEvent, core.CodeNotFound},
		{"other namespace", "other", "evt_2", "evt_1", core.RecEvent, core.CodeNotFound},
		{"unsupported type", "ns", "ent_1", "evt_1", core.RecEntity, core.CodeInvalidArgument},
	}
	for _, c := range cases {
		if _, err := mark(c.ns, c.nt, c.nid, core.RecEvent, c.eid); core.CodeOf(err) != c.code {
			t.Errorf("%s: %v want %s", c.name, err, c.code)
		}
	}
	var acts int
	e.Store.DB().QueryRow(`SELECT count(*) FROM audit_log WHERE action=?`, audit.ActNearDup).Scan(&acts)
	if acts != 1 {
		t.Fatalf("near_duplicate audits=%d", acts)
	}
	if r, err := audit.Verify(ctx, e.Store.DB()); err != nil || !r.OK {
		t.Fatalf("audit: %+v %v", r, err)
	}
}
