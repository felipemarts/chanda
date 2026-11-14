package scenarios

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Forget scenarios. Each one writes content carrying distinctive marker
// strings plus a control record that must survive, checks that the markers
// are findable before forget (positive controls for every probe), forgets,
// and then checks absence independently:
//
//   - raw byte scan of chanda.db, -wal and -shm while the server still runs;
//   - raw byte scan again after the server exits;
//   - a copy of the database opened with an independent SQLite reader:
//     every column of every row of every table (FTS shadow tables
//     included), the FTS5 vocabulary, and the embeddings rows of the ids
//     collected before forget;
//   - after a restart: search (with and without history), context, get and
//     timeline through MCP.
//
// The byte scans run before any post-forget query, because a later query
// that names a marker is itself logged (retrievals.query_text) — that is a
// new query, not residue of the forgotten content.

// collected are the ids that held the forgotten content before forget.
type collected struct {
	ids []string
}

func (c *collected) add(items []chanda.Item, marker string) {
	for _, it := range withText(items, marker) {
		if !contains(c.ids, it.ID) {
			c.ids = append(c.ids, it.ID)
		}
	}
	sort.Strings(c.ids)
}

// forgetCase describes what checkForgotten verifies.
type forgetCase struct {
	markers []string // must be gone everywhere
	control string   // marker of a record that must survive
	queries []string // context queries that found the content before forget
}

// prechecks runs while the server is stopped, before forget: the markers
// and the control are in the files, and the collected ids have vectors.
func (s *S) forgetPrechecks(fc forgetCase, c *collected) {
	s.step("stop the server; scan the raw files and a copy of the database before forget (positive controls)")
	s.stop()
	n := s.scanTotal(fc.markers)
	s.check("before forget: markers present in the raw files (scanner positive control)", "> 0 occurrences", fmt.Sprint(n), n > 0)
	db := s.inspect()
	defer db.Close()
	vec := s.countIn(db, `SELECT count(*) FROM embeddings WHERE record_id IN (%s)`, c.ids)
	s.check("before forget: embeddings rows of the records holding the markers (vector positive control)",
		"> 0", fmt.Sprintf("%d rows for %d records", vec, len(c.ids)), vec > 0)
	vocab := s.vocabHits(db, fc.markers)
	s.check("before forget: markers in the FTS5 vocabulary (index positive control)", "> 0 terms", fmt.Sprint(len(vocab)), len(vocab) > 0)
}

// checkForgotten runs after a forget that ran with the server up (live
// scan first) or through the CLI (server down).
func (s *S) checkForgotten(fc forgetCase, c *collected) {
	if s.cs != nil {
		s.step("scan chanda.db, chanda.db-wal and chanda.db-shm as raw bytes while the server still runs")
		hits, absent := s.scan(fc.markers)
		s.checkStr("live raw byte scan (db, wal, shm) for the markers", "none (files absent: none)",
			fmtHits(hits)+" (files absent: "+listOrNone(absent)+")")
		s.stop()
	}
	s.step("after the server exits: scan the raw files again and inspect a copy of the database")
	hits, absent := s.scan(fc.markers)
	s.checkStr("raw byte scan after exit (db, wal, shm) for the markers", "none",
		fmtHits(hits))
	s.checkStr("files present after exit (WAL and SHM are removed on a clean close)", "chanda.db",
		strings.Join(present(s.fileSet(), absent), ", "))
	ctl := s.scanTotal([]string{fc.control})
	s.check("control record still in the raw file", "> 0 occurrences", fmt.Sprint(ctl), ctl > 0)
	db := s.inspect()
	s.checkStr("tables whose rows hold a marker (every column, every table, FTS shadow tables included)", "none",
		listOrNone(s.tablesHolding(db, fc.markers)))
	s.checkStr("markers in the FTS5 vocabulary", "none", listOrNone(s.vocabHits(db, fc.markers)))
	s.checkInt("embeddings rows of the forgotten records", 0,
		s.countIn(db, `SELECT count(*) FROM embeddings WHERE record_id IN (%s)`, c.ids))
	s.checkInt("rows of any table keyed by a forgotten id (events, facts, embeddings, embed_queue, provenance)", 0,
		s.countIn(db, `SELECT (SELECT count(*) FROM events WHERE id IN (%s))`, c.ids)+
			s.countIn(db, `SELECT (SELECT count(*) FROM facts WHERE id IN (%s))`, c.ids)+
			s.countIn(db, `SELECT (SELECT count(*) FROM embed_queue WHERE record_id IN (%s))`, c.ids)+
			s.countIn(db, `SELECT (SELECT count(*) FROM provenance WHERE derived_id IN (%s))`, c.ids)+
			s.countIn(db, `SELECT (SELECT count(*) FROM provenance WHERE source_id IN (%s))`, c.ids))
	db.Close()

	s.step("restart the server; search, context, get and timeline for the markers")
	s.start("read,standard")
	var hitsSearch, hitsHist, hitsCtx, idHits []string
	for _, m := range fc.markers {
		sr := s.search(m, map[string]any{"limit": 100})
		sh := s.search(m, map[string]any{"limit": 100, "include_history": true})
		if len(withText(sr.Items, m)) > 0 {
			hitsSearch = append(hitsSearch, m)
		}
		if len(withText(sh.Items, m)) > 0 {
			hitsHist = append(hitsHist, m)
		}
		for _, it := range append(sr.Items, sh.Items...) {
			if contains(c.ids, it.ID) {
				idHits = append(idHits, "query "+m)
				break
			}
		}
	}
	for _, q := range append(append([]string{}, fc.queries...), fc.markers...) {
		cx := s.context(q, nil)
		for _, m := range fc.markers {
			if strings.Contains(strings.ToLower(cx.Text), m) {
				hitsCtx = append(hitsCtx, fmt.Sprintf("%q->%s", q, m))
			}
		}
	}
	s.checkStr("search (default) items holding a marker", "none", listOrNone(hitsSearch))
	s.checkStr("search (include_history) items holding a marker", "none", listOrNone(hitsHist))
	s.checkStr("search items with a forgotten id", "none", listOrNone(idHits))
	s.checkStr("context blocks holding a marker (pre-forget queries and the markers themselves)", "none", listOrNone(hitsCtx))
	notFound := 0
	for _, id := range c.ids {
		env, _ := s.call(chanda.ToolGet, map[string]any{"id": id})
		if !env.OK && env.Error.Code == chanda.CodeNotFound {
			notFound++
		}
	}
	s.check("get on each forgotten id", fmt.Sprintf("not_found for all %d", len(c.ids)), fmt.Sprintf("not_found for %d", notFound), notFound == len(c.ids))
	sr := s.search(fc.control, nil)
	s.checkBool("control record still found by search", true, len(withText(sr.Items, fc.control)) > 0)
}

// forgetCall runs forget over MCP (admin profile) and records its report.
func (s *S) forgetCall(scope map[string]any) chanda.ForgetOutput {
	var dry chanda.ForgetOutput
	s.ok(chanda.ToolForget, map[string]any{"scope": scope, "dry_run": true}, &dry)
	var out chanda.ForgetOutput
	env, data := s.call(chanda.ToolForget, map[string]any{"scope": scope})
	if env.OK {
		if err := json.Unmarshal(data, &out); err != nil {
			s.fail(err)
		}
	}
	s.check("forget call succeeds", "ok", okOrCode(env), env.OK)
	s.checkBool("dry run planned the same root events as the real run", true, dry.RootEvents == out.RootEvents && dry.RootEvents > 0)
	s.checkBool("forget's own verification report (Chanda self-check, not relied on)", true, out.Verification.Passed)
	return out
}

func okOrCode(env chanda.Envelope) string {
	if env.OK {
		return "ok"
	}
	return string(env.Error.Code) + ": " + env.Error.Message
}

// ---------------------------------------------------------------------------

func forgetDocument(s *S) {
	fc := forgetCase{
		markers: []string{"vexalornqat", "zintelmarq"},
		control: "controlpalisandrex",
		queries: []string{"Qual o código vexalornqat do projeto Orvalho?", "Quem é o fornecedor do Projeto Orvalho?"},
	}
	s.step("start chanda serve (profiles read, standard, admin)")
	s.start("read,standard,admin")
	s.step("write docs/orvalho.md (header facts with marker zintelmarq, a paragraph with marker vexalornqat) and docs/manual.md (control), ingest the folder over MCP, flush")
	s.writeDoc("orvalho.md", "# Projeto Orvalho\n\nFornecedor: Zintelmarq Ltda\nOrcamento: 870 mil reais\n\n"+
		"O codigo interno vexalornqat libera o faturamento do projeto Orvalho.\n")
	s.writeDoc("manual.md", "# Manual da sede\n\nO armario controlpalisandrex fica no segundo andar.\n")
	var ing chanda.IngestOutput
	s.ok(chanda.ToolIngest, map[string]any{"path": s.docs}, &ing)
	s.flush()
	uri := ""
	for _, d := range ing.Documents {
		if strings.HasSuffix(d.URI, "/orvalho.md") {
			uri = d.URI
		}
	}
	if uri == "" {
		s.failf("orvalho.md not ingested: %+v", ing)
	}
	s.step("before forget: search and context find both markers; collect the ids of the records holding them")
	var c collected
	for _, m := range fc.markers {
		sr := s.search(m, map[string]any{"include_history": true})
		c.add(sr.Items, m)
	}
	tl, _ := s.timeline("Projeto Orvalho", "", "")
	for _, e := range tl.Entries {
		if !contains(c.ids, e.FactID) {
			c.ids = append(c.ids, e.FactID)
		}
	}
	sort.Strings(c.ids)
	found := 0
	for _, q := range fc.queries {
		if cx := s.context(q, nil); strings.Contains(strings.ToLower(cx.Text), "zintelmarq") || strings.Contains(strings.ToLower(cx.Text), "vexalornqat") {
			found++
		}
	}
	s.checkInt("before forget: context queries that return a marker", len(fc.queries), found)
	s.check("before forget: records holding the markers (events and facts)", ">= 3 (2 chunks, >= 1 fact)", fmt.Sprint(len(c.ids)), len(c.ids) >= 3)
	s.forgetPrechecks(fc, &c)
	s.start("read,standard,admin")
	s.step("forget scope document (the document uri returned by ingest) over MCP, dry run first")
	out := s.forgetCall(map[string]any{"kind": "document", "value": uri})
	s.check("forget removed the document's events and facts", "events >= 2, facts >= 2", fmt.Sprintf("events %d, facts %d", out.Removed.Events, out.Removed.Facts),
		out.Removed.Events >= 2 && out.Removed.Facts >= 2)
	s.checkForgotten(fc, &c)
	tl2, code := s.timeline("Projeto Orvalho", "", "")
	s.check("timeline of the document's subject", "not_found or no entries", fmt.Sprintf("%s, %d entries", code, len(tl2.Entries)),
		code == "not_found" || len(tl2.Entries) == 0)
	s.stop()
}

func forgetEntity(s *S) {
	fc := forgetCase{
		markers: []string{"belvasque", "tamborvex"},
		control: "controlgranitovo",
		queries: []string{"Qual o endereço de Quorrin Belvasque?", "email de Quorrin Belvasque"},
	}
	s.step("start chanda serve (profiles read, standard, admin)")
	s.start("read,standard,admin")
	s.step("assert_fact about entity \"Quorrin Belvasque\": email, phone, address (marker tamborvex); a control fact about another entity; flush")
	var c collected
	for _, f := range [][2]string{{"email", "quorrin.belvasque@exemplo.com"}, {"phone", "+55 11 4002-8922"}, {"address", "Rua Tamborvex 77, Curitiba"}} {
		r := s.assertFact("Quorrin Belvasque", f[0], f[1], "2026-01-10", nil)
		c.ids = append(c.ids, r.ID)
	}
	s.assertFact("Lidia Marquez", "address", "Rua controlgranitovo 12", "2026-01-10", nil)
	s.flush()
	s.step("before forget: collect the ids of every record holding a marker (facts and their assertion events)")
	for _, m := range fc.markers {
		sr := s.search(m, map[string]any{"include_history": true, "limit": 100})
		c.add(sr.Items, m)
	}
	for _, id := range append([]string{}, c.ids...) {
		var g chanda.GetOutput
		s.ok(chanda.ToolGet, map[string]any{"id": id}, &g)
		for _, e := range g.Provenance {
			if e.SourceType == chanda.RecEvent && !contains(c.ids, e.SourceID) {
				c.ids = append(c.ids, e.SourceID)
			}
		}
	}
	sort.Strings(c.ids)
	tl, _ := s.timeline("Quorrin Belvasque", "", "")
	act := 0
	for _, e := range tl.Entries {
		if e.Status == "active" {
			act++
		}
	}
	s.checkInt("before forget: active facts of the entity", 3, act)
	found := 0
	for _, q := range fc.queries {
		if cx := s.context(q, nil); strings.Contains(strings.ToLower(cx.Text), "tamborvex") || strings.Contains(strings.ToLower(cx.Text), "quorrin.belvasque@") {
			found++
		}
	}
	s.checkInt("before forget: context queries that return the entity's facts", len(fc.queries), found)
	s.forgetPrechecks(fc, &c)
	s.start("read,standard,admin")
	s.step("forget scope entity \"Quorrin Belvasque\" over MCP, dry run first")
	out := s.forgetCall(map[string]any{"kind": "entity", "value": "Quorrin Belvasque"})
	s.check("forget removed the entity's facts", "facts = 3", fmt.Sprintf("facts %d, events %d", out.Removed.Facts, out.Removed.Events), out.Removed.Facts == 3)
	s.checkForgotten(fc, &c)
	tl2, code := s.timeline("Quorrin Belvasque", "", "")
	s.check("timeline of the entity", "not_found or no entries", fmt.Sprintf("%s, %d entries", code, len(tl2.Entries)),
		code == "not_found" || len(tl2.Entries) == 0)
	s.stop()
}

// forgetEntityMentions: BASE.md forget scope "entity" and decision 0050
// ("Entity scope resolves to the events that mention the entity, plus the
// origin events of facts whose subject is the entity"): a free-text
// statement naming the entity is content about it.
func forgetEntityMentions(s *S) {
	fc := forgetCase{
		markers: []string{"drovanquiz"},
		control: "controlbasaltrino",
		queries: []string{"Qual o apelido de Ysolde Karvenni?"},
	}
	s.step("start chanda serve (profiles read, standard, admin)")
	s.start("read,standard,admin")
	s.step("assert_fact job_title about entity \"Ysolde Karvenni\"; remember a free-text statement naming her (marker drovanquiz); a control statement; flush")
	var c collected
	r := s.assertFact("Ysolde Karvenni", "job_title", "Arquiteta de dados", "2026-02-01", nil)
	c.ids = append(c.ids, r.ID)
	m := s.remember("Ysolde Karvenni prefere reunioes as tercas; o apelido dela no time e drovanquiz.", nil)
	c.ids = append(c.ids, m.ID)
	s.remember("A sala controlbasaltrino tem projetor novo.", nil)
	s.flush()
	sort.Strings(c.ids)
	sr := s.search("Ysolde Karvenni drovanquiz", nil)
	s.checkBool("before forget: search finds the statement naming the entity", true, len(withText(sr.Items, "drovanquiz")) > 0)
	s.forgetPrechecks(fc, &c)
	s.start("read,standard,admin")
	s.step("forget scope entity \"Ysolde Karvenni\" over MCP, dry run first")
	s.forgetCall(map[string]any{"kind": "entity", "value": "Ysolde Karvenni"})
	s.checkForgotten(fc, &c)
	s.stop()
}

// forgetSearchCLI: a person forgets everything matching a keyword with the
// CLI while the server is down.
func forgetSearchCLI(s *S) {
	fc := forgetCase{
		markers: []string{"kravellinth"},
		control: "controlquartzino",
		queries: []string{"Qual o status do projeto kravellinth?"},
	}
	s.step("start chanda serve (profiles read, standard)")
	s.start("read,standard")
	s.step("remember, log_session (user and assistant turns) and assert_fact, all naming marker kravellinth; a control statement; flush")
	var c collected
	s.remember("O projeto kravellinth foi pausado em marco por falta de verba.", nil)
	var ls chanda.LogSessionOutput
	s.ok(chanda.ToolLogSession, map[string]any{"session_id": "sess-k1", "turns": []map[string]any{
		{"role": "user", "content": "Qual o status do kravellinth?"},
		{"role": "assistant", "content": "O kravellinth continua pausado ate a revisao do orcamento."},
	}}, &ls)
	s.assertFact("Projeto Kravellinth", "status", "pausado", "2026-03-01", nil)
	s.remember("A sala controlquartzino tem projetor.", nil)
	s.flush()
	sr := s.search("kravellinth", map[string]any{"include_history": true, "limit": 100})
	c.add(sr.Items, "kravellinth")
	s.check("before forget: records holding the marker", ">= 4 (statement, 2 turns, fact)", fmt.Sprint(len(c.ids)), len(c.ids) >= 4)
	s.forgetPrechecks(fc, &c)
	s.step("chanda forget --kind search --query kravellinth (CLI, human actor) with the server down, dry run first")
	envDry, dataDry := s.cli("forget", "--kind", "search", "--query", "kravellinth", "--dry-run")
	env, data := s.cli("forget", "--kind", "search", "--query", "kravellinth")
	var dry, out chanda.ForgetOutput
	_ = json.Unmarshal(dataDry, &dry)
	_ = json.Unmarshal(data, &out)
	s.check("CLI forget succeeds", "ok", okOrCode(env), env.OK && envDry.OK)
	s.checkBool("forget's own verification report (Chanda self-check, not relied on)", true, out.Verification.Passed)
	s.check("forget removed the matching events and the derived fact", "events >= 4, facts >= 1",
		fmt.Sprintf("events %d, facts %d", out.Removed.Events, out.Removed.Facts), out.Removed.Events >= 4 && out.Removed.Facts >= 1)
	s.step("start the server again (no query yet)")
	s.start("read,standard")
	s.checkForgotten(fc, &c)
	tl, code := s.timeline("Projeto Kravellinth", "", "")
	s.check("timeline of the fact's subject", "not_found or no entries", fmt.Sprintf("%s, %d entries", code, len(tl.Entries)),
		code == "not_found" || len(tl.Entries) == 0)
	s.stop()
}

// present lists the base names of files not in absent.
func present(files, absent []string) []string {
	var out []string
	for _, f := range files {
		if b := filepath.Base(f); !contains(absent, b) {
			out = append(out, b)
		}
	}
	return out
}
