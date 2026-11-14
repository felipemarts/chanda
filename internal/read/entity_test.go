package read

import (
	"encoding/json"
	"testing"
)

// BASE.md "Signals" (entity match): every logged candidate carries
// features.entity_match, 1 when the query names an entity the record is
// about: the subject of a fact, the subject of a fact an event states, or an
// entity the record's text names with the alias the query used.
func TestRetrievalLogsEntityMatch(t *testing.T) {
	f := newFx(t)
	f.basic("ns")
	f.exec(`INSERT INTO entities(id,namespace,canonical_name,created_at) VALUES ('ent_ana','ns','Ana Ribeiro',?), ('ent_old','ns','Ana R.',?)`, t0, t0)
	f.exec(`UPDATE entities SET merged_into = 'ent_ana' WHERE id = 'ent_old'`)
	f.exec(`INSERT INTO entity_aliases(namespace,alias_norm,entity_id) VALUES ('ns','ana ribeiro','ent_ana'), ('ns','ana r','ent_old')`)
	f.event(ev{ID: "evt_card", NS: "ns", Source: "src_doc_ns", Content: "Cargo: Analista Financeira. Ramal: 2345."})
	f.event(ev{ID: "evt_named", NS: "ns", Source: "src_doc_ns", Content: "Ana Ribeiro cuida da conciliação bancária."})
	f.event(ev{ID: "evt_other", NS: "ns", Source: "src_doc_ns", Content: "O ramal da recepção é 2000, cargo de recepcionista."})
	f.fact(fc{ID: "fct_ana", NS: "ns", Subject: "ent_old", Predicate: "cargo", Value: "Analista Financeira", Text: "Ana Ribeiro cargo: Analista Financeira"})
	f.fact(fc{ID: "fct_other", NS: "ns", Subject: "ent_x", Predicate: "cargo", Value: "Recepcionista", Text: "Recepção cargo: Recepcionista"})
	f.prov("ns", "fact", "fct_ana", "event", "evt_card", "derived_from")
	r := New(f.e, Deps{})
	res, err := r.Search(f.ctx, SearchInput{Namespace: "ns", Query: "Qual o cargo e o ramal de Ana Ribeiro?"})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := f.e.Store.DB().QueryRow(`SELECT candidates FROM retrievals WHERE id = ?`, res.RetrievalID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var cs []LoggedCandidate
	if err := json.Unmarshal([]byte(raw), &cs); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"evt_card": 1, "evt_named": 1, "fct_ana": 1, "evt_other": 0, "fct_other": 0}
	seen := 0
	for _, c := range cs {
		w, ok := want[c.ID]
		if !ok {
			continue
		}
		seen++
		if got, has := c.Features["entity_match"]; !has || got != w {
			t.Errorf("%s: entity_match = %v (present %v), want %v", c.ID, got, has, w)
		}
	}
	if seen != len(want) {
		t.Fatalf("logged %d of %d candidates: %s", seen, len(want), raw)
	}
}
