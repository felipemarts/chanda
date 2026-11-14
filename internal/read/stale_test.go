package read

import (
	"slices"
	"testing"
	"time"
)

// Decision 0120 (BASE.md "Temporal correctness", context never delivers a
// superseded value): a raw excerpt that states a value of a single-valued
// fact chain which does not hold at as_of is dropped in context mode, even
// when the excerpt is not the fact's origin.
func TestContextDropsExcerptsStatingStaleValues(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.source("ns", "src_sess", "session", "sess-1", "internal")
	const jan, jul, oct = "2026-01-05T00:00:00.000000000Z", "2026-07-01T00:00:00.000000000Z", "2026-10-01T00:00:00.000000000Z"
	single := func(id, subj, pred, value, status, from, to string) {
		var vt any
		if to != "" {
			vt = to
		}
		f.exec(`INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,valid_to,recorded_at,status,confidence,trust_zone,is_single,text)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "ns", subj, pred, value, value, from, vt, from, status, 0.5, "internal", 1, subj+" "+pred+": "+value)
	}
	entity := func(id, name, alias string) {
		f.exec(`INSERT INTO entities(id,namespace,canonical_name,created_at) VALUES (?,?,?,?)`, id, "ns", name, t0)
		f.exec(`INSERT INTO entity_aliases(namespace,alias_norm,entity_id) VALUES (?,?,?)`, "ns", alias, id)
	}
	entity("ent_pro", "Plano Profissional", "plano profissional")
	entity("ent_basic", "Plano Básico", "plano basico")
	entity("ent_clean", "Fornecimento de limpeza", "fornecimento de limpeza")
	// Plano Profissional: 120 (Jan-Jul), 133.2 (Jul-Oct), 157.1 (Oct-).
	single("fct_p1", "ent_pro", "preco", "r$ 120", "superseded", jan, jul)
	single("fct_p2", "ent_pro", "preco", "r$ 133.2", "superseded", jul, oct)
	single("fct_p3", "ent_pro", "preco", "r$ 157.1", "active", oct, "")
	// Plano Básico costs 120 today: its own chain (59.9 before).
	single("fct_b1", "ent_basic", "preco", "r$ 59.9", "superseded", jan, jul)
	single("fct_b2", "ent_basic", "preco", "r$ 120", "active", jul, "")
	// A supplier asserted in a session, replaced later.
	single("fct_c1", "ent_clean", "fornecedor", "café serra azul", "superseded", jan, jul)
	single("fct_c2", "ent_clean", "fornecedor", "papelaria central", "active", jul, "")
	f.event(ev{ID: "evt_assert", NS: "ns", Source: "src_sess", Kind: "fact_assertion", SessionID: "sess-1", Ordinal: 9,
		Content: "Fornecimento de limpeza fornecedor: Café Serra Azul", RecordedAt: jan})
	f.prov("ns", "fact", "fct_c1", "event", "evt_assert", "derived_from")

	evs := []ev{
		{ID: "evt_jul", Content: "A partir de julho, o Plano Profissional passa a sair por R$ 133,20 por usuário.", RecordedAt: jul},
		{ID: "evt_oct", Content: "A partir de outubro, o Plano Profissional passa a sair por R$ 157,10 por usuário.", RecordedAt: oct},
		{ID: "evt_old_turn", Kind: "turn", Source: "src_sess", SessionID: "sess-1", Ordinal: 1, RecordedAt: jan,
			Content: "Ficou decidido que a limpeza passa a ser feita pela Café Serra Azul."},
		{ID: "evt_basic", Content: "O Plano Básico custa R$ 120,00 por usuário, preço promocional.", RecordedAt: jul},
		{ID: "evt_label", Content: "Qual o preço do plano mais vendido? O preço é de R$ 133,20 por usuário.", RecordedAt: jul},
		{ID: "evt_prefix", Content: "O Plano Profissional tem desconto para R$ 1.200,00 anuais.", RecordedAt: jan},
	}
	for _, e := range evs {
		e.NS = "ns"
		if e.Source == "" {
			e.Source = "src_doc_ns"
		}
		f.event(e)
	}
	r := New(f.e, Deps{})
	cases := []struct {
		name, query string
		asOf        time.Time
		want, not   []string
	}{
		{"names the subject, superseded value", "Plano Profissional preço usuário", time.Time{},
			[]string{"evt_oct", "evt_prefix"}, []string{"evt_jul"}},
		{"value current for another subject stays", "Plano Básico preço usuário", time.Time{},
			[]string{"evt_basic"}, nil},
		{"attribute label with a stale value", "preço plano mais vendido usuário", time.Time{},
			nil, []string{"evt_label"}},
		{"turn of the session that asserted the old value", "limpeza Café Serra Azul", time.Time{},
			nil, []string{"evt_old_turn"}},
		{"explicit as_of keeps the value that held then", "Plano Profissional preço usuário", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
			[]string{"evt_jul"}, []string{"evt_oct"}},
		{"explicit as_of inside the session value", "limpeza Café Serra Azul", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			[]string{"evt_old_turn"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: tc.query, AsOf: tc.asOf, BudgetTokens: 1500})
			if err != nil {
				t.Fatal(err)
			}
			got := ids(res.Items)
			for _, id := range tc.want {
				if !slices.Contains(got, id) {
					t.Errorf("%s missing from context: %v", id, got)
				}
			}
			for _, id := range tc.not {
				if slices.Contains(got, id) {
					t.Errorf("%s delivered in context: %v", id, got)
				}
			}
			s, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: tc.query, AsOf: tc.asOf})
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range tc.not {
				if !slices.Contains(ids(s.Items), id) {
					t.Errorf("search keeps every live event, %s missing: %v", id, ids(s.Items))
				}
			}
		})
	}
}

// Decision 0120: with an explicit as_of, an excerpt that stated a fact whose
// validity does not contain as_of (it only began later) is not delivered.
func TestContextAsOfDropsExcerptsOfLaterFacts(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	const jan, jun = "2026-01-05T00:00:00.000000000Z", "2026-06-01T00:00:00.000000000Z"
	f.event(ev{ID: "evt_v1", NS: "ns", Source: "src_doc_ns", Content: "Zeca Oliveira ramal: 3530", RecordedAt: jan})
	f.event(ev{ID: "evt_v2", NS: "ns", Source: "src_doc_ns", Content: "Zeca Oliveira ramal: 5014", RecordedAt: jun})
	f.fact(fc{ID: "fct_v1", NS: "ns", Predicate: "ramal", Value: "3530", Text: "Zeca Oliveira ramal: 3530", Status: "superseded",
		ValidFrom: jan, ValidTo: jun, RecordedAt: jan})
	f.fact(fc{ID: "fct_v2", NS: "ns", Predicate: "ramal", Value: "5014", Text: "Zeca Oliveira ramal: 5014", ValidFrom: jun, RecordedAt: jun})
	f.exec(`UPDATE facts SET superseded_by = 'fct_v2' WHERE id = 'fct_v1'`)
	f.prov("ns", "fact", "fct_v1", "event", "evt_v1", "derived_from")
	f.prov("ns", "fact", "fct_v2", "event", "evt_v2", "derived_from")
	r := New(f.e, Deps{})
	for _, tc := range []struct {
		asOf      time.Time
		want, not string
	}{
		{time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC), "evt_v1", "evt_v2"},
		{time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "evt_v2", "evt_v1"},
		{time.Time{}, "evt_v2", "evt_v1"},
	} {
		res, err := r.Context(f.ctx, ContextInput{Namespace: "ns", Query: "Zeca Oliveira ramal", AsOf: tc.asOf, BudgetTokens: 1500})
		if err != nil {
			t.Fatal(err)
		}
		got := ids(res.Items)
		if !slices.Contains(got, tc.want) || slices.Contains(got, tc.not) {
			t.Errorf("as_of %v: got %v, want %s and not %s", tc.asOf, got, tc.want, tc.not)
		}
	}
}

func TestContainsWord(t *testing.T) {
	for _, tc := range []struct {
		s, v string
		want bool
	}{
		{"custa r$ 120 por mês", "r$ 120", true},
		{"custa r$ 1200 por mês", "r$ 120", false},
		{"custa r$ 120.5", "r$ 120", false},
		{"r$ 120.", "r$ 120", true},
		{"cc-4632", "cc-4632", true},
		{"xcc-4632", "cc-4632", false},
		{"no máximo 4 horas úteis.", "4 horas úteis", true},
	} {
		if got := containsWord(tc.s, tc.v); got != tc.want {
			t.Errorf("containsWord(%q, %q) = %v", tc.s, tc.v, got)
		}
	}
}
