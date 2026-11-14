package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/embed"
	"github.com/felipemarts/chanda-mcp/internal/prov"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// Performance at scale (BASE.md "Performance targets": with 100 thousand
// records and four cores, p95 under 150 ms without the reranker). Numbers
// are recorded in docs/decisions/0100-performance.md.
//
// Skipped with -short. CHANDA_PERF_RECORDS overrides the record count;
// CHANDA_PERF_CPUPROFILE=<file> writes a CPU profile of the read loop.

// perfRNG is a fixed linear congruential generator (no math/rand).
type perfRNG uint64

func (l *perfRNG) next() uint64 {
	*l = *l*6364136223846793005 + 1442695040888963407
	return uint64(*l >> 33)
}

var perfSyll = []string{"ka", "lo", "mi", "ne", "pu", "ra", "si", "to", "ve", "zu", "bri", "cha", "dor", "fen", "gal", "hum"}

// perfWord is vocabulary word i of 4096 (three syllables).
func perfWord(i int) string {
	return perfSyll[i%16] + perfSyll[(i/16)%16] + perfSyll[(i/256)%16]
}

var perfCommon = []string{"the", "deploy", "cluster", "price", "plan", "team", "owner", "release", "customer", "invoice",
	"meeting", "policy", "support", "server", "backup", "report", "budget", "project", "contract", "access"}

// perfText is a sentence of n words: a skewed draw over the vocabulary
// (common words repeat often, rare ones seldom) mixed with plain words.
func perfText(g *perfRNG, n int) string {
	words := make([]string, n)
	for j := range words {
		if g.next()%5 == 0 {
			words[j] = perfCommon[g.next()%uint64(len(perfCommon))]
			continue
		}
		x := int(g.next() % 4096)
		words[j] = perfWord(x * x / 4096)
	}
	return strings.Join(words, " ")
}

type perfCounts struct{ chunks, turns, facts int }

// perfSeed bulk-inserts records straight into the tables (the write path at
// this volume would take minutes): 60% document chunks in documents of 120
// chunks, 30% session turns, 10% facts over 2 000 entities with a
// derived_from edge to a chunk, and one hash embedding per record.
func perfSeed(t *testing.T, a *App, total int) perfCounts {
	t.Helper()
	ctx := context.Background()
	n := perfCounts{chunks: total * 6 / 10, turns: total * 3 / 10}
	n.facts = total - n.chunks - n.turns
	ts := core.FormatTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := perfRNG(7)
	model, dim := a.emb.ModelID(), a.emb.Dim()
	type vec struct{ rt, id, text string }
	var pending []vec
	flush := func(tx *sql.Tx, st *sql.Stmt) error {
		if len(pending) == 0 {
			return nil
		}
		texts := make([]string, len(pending))
		for i, p := range pending {
			texts[i] = p.text
		}
		vs, err := a.emb.Embed(ctx, texts)
		if err != nil {
			return err
		}
		for i, p := range pending {
			if _, err := st.Exec("ns", p.rt, p.id, model, dim, embed.Encode(vs[i])); err != nil {
				return err
			}
		}
		pending = pending[:0]
		return nil
	}
	err := a.e.Store.Tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES('src_doc','ns','document','kb','internal','` + ts + `')`,
			`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES('src_ses','ns','session','sessions','internal','` + ts + `')`,
			`INSERT INTO sources(id,namespace,kind,uri,trust_zone,created_at) VALUES('src_web','ns','web','https://example.com','external','` + ts + `')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		insDoc, _ := tx.Prepare(`INSERT INTO documents(id,namespace,source_id,uri,current_version,content_hash,updated_at) VALUES(?,?,?,?,1,?,?)`)
		insEv, _ := tx.Prepare(`INSERT INTO events(id,namespace,source_id,kind,content,content_hash,trust_zone,actor,recorded_at,doc_id,doc_version,ordinal,heading_path,session_id,flags)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		insDC, _ := tx.Prepare(`INSERT INTO document_chunks(namespace,doc_id,version,ordinal,event_id) VALUES(?,?,1,?,?)`)
		insEnt, _ := tx.Prepare(`INSERT INTO entities(id,namespace,canonical_name,type,created_at) VALUES(?,?,?,'',?)`)
		insAl, _ := tx.Prepare(`INSERT INTO entity_aliases(namespace,alias_norm,entity_id) VALUES(?,?,?)`)
		insFact, _ := tx.Prepare(`INSERT INTO facts(id,namespace,subject,predicate,value,value_norm,valid_from,recorded_at,status,confidence,trust_zone,is_single,text)
			VALUES(?,?,?,?,?,?,?,?,'active',0.5,'internal',?,?)`)
		insProv, _ := tx.Prepare(`INSERT INTO provenance(namespace,derived_type,derived_id,source_type,source_id,relation,created_at) VALUES('ns','fact',?,'event',?,'derived_from',?)`)
		insEmb, _ := tx.Prepare(`INSERT INTO embeddings(namespace,record_type,record_id,model_id,dim,vector) VALUES(?,?,?,?,?,?)`)
		for _, st := range []*sql.Stmt{insDoc, insEv, insDC, insEnt, insAl, insFact, insProv, insEmb} {
			if st == nil {
				return fmt.Errorf("prepare failed")
			}
			defer st.Close()
		}
		embedLater := func(rt, id, text string) error {
			pending = append(pending, vec{rt, id, text})
			if len(pending) >= 512 {
				return flush(tx, insEmb)
			}
			return nil
		}
		docs := (n.chunks + 119) / 120
		for d := 0; d < docs; d++ {
			did := fmt.Sprintf("doc_%05d", d)
			if _, err := insDoc.Exec(did, "ns", "src_doc", fmt.Sprintf("kb/doc-%05d.md", d), "h"+did, ts); err != nil {
				return err
			}
		}
		for i := 0; i < n.chunks; i++ {
			id, d := fmt.Sprintf("evt_c%06d", i), i/120
			did := fmt.Sprintf("doc_%05d", d)
			text := perfText(&g, 40+int(g.next()%60))
			heading := "Doc " + perfWord(d%4096) + " > Section " + perfWord((d*7+i%120/10)%4096)
			if _, err := insEv.Exec(id, "ns", "src_doc", "chunk", text, "h"+id, "internal", "bench", ts, did, 1, i%120, heading, nil, ""); err != nil {
				return err
			}
			if _, err := insDC.Exec("ns", did, i%120, id); err != nil {
				return err
			}
			if err := embedLater("event", id, text); err != nil {
				return err
			}
		}
		for i := 0; i < n.turns; i++ {
			id := fmt.Sprintf("evt_t%06d", i)
			src, zone, kind, flags := "src_ses", "internal", "turn", ""
			if i%10 == 0 { // some external tool output, a few instruction-like
				src, zone, kind = "src_web", "external", "web"
				if i%50 == 0 {
					flags = "instruction_like"
				}
			}
			text := perfText(&g, 15+int(g.next()%40))
			if _, err := insEv.Exec(id, "ns", src, kind, text, "h"+id, zone, "bench", ts, nil, nil, i%40, nil, fmt.Sprintf("s%05d", i/40), flags); err != nil {
				return err
			}
			if err := embedLater("event", id, text); err != nil {
				return err
			}
		}
		const ents = 2000
		for e := 0; e < ents; e++ {
			id, name := fmt.Sprintf("ent_%05d", e), "Entity "+perfWord((e*37)%4096)
			if _, err := insEnt.Exec(id, "ns", name, ts); err != nil {
				return err
			}
			if _, err := insAl.Exec("ns", fmt.Sprintf("entity %s %d", perfWord((e*37)%4096), e), id); err != nil {
				return err
			}
		}
		singles := []string{"owner", "status", "manager", "location"}
		for i := 0; i < n.facts; i++ {
			id, e := fmt.Sprintf("fct_%06d", i), i%ents
			pred, single := fmt.Sprintf("attr_%s", perfWord(int(g.next()%64))), 0
			if k := i / ents; k < len(singles) { // one active value per (entity, single predicate)
				pred, single = singles[k], 1
			}
			value := perfWord(int(g.next() % 4096))
			text := "Entity " + perfWord((e*37)%4096) + " " + pred + ": " + value
			if _, err := insFact.Exec(id, "ns", fmt.Sprintf("ent_%05d", e), pred, value, value, ts, ts, single, text); err != nil {
				return err
			}
			if _, err := insProv.Exec(id, fmt.Sprintf("evt_c%06d", (i*13)%n.chunks), ts); err != nil {
				return err
			}
			if err := embedLater("fact", id, text); err != nil {
				return err
			}
		}
		return flush(tx, insEmb)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// perfQueries are the measured queries: vocabulary words of every
// frequency, plain words, a fact-shaped question and a miss.
func perfQueries() []string {
	return []string{
		perfWord(5) + " " + perfWord(300) + " " + perfWord(1200),
		"how do we configure the " + perfWord(40) + " " + perfWord(900) + " release",
		perfWord(2000) + " " + perfWord(3000) + " " + perfWord(77) + " " + perfWord(1),
		"who is the owner of entity " + perfWord(37),
		"deploy cluster backup",
		"customer invoice policy for " + perfWord(150),
		perfWord(4000) + " " + perfWord(3500),
		"something entirely absent from the corpus",
		"what is the status of entity " + perfWord(74*37%4096) + " project",
		perfWord(10) + " " + perfWord(11) + " " + perfWord(12) + " " + perfWord(13) + " " + perfWord(14) + " meeting",
	}
}

type perfStats struct{ p50, p95, max time.Duration }

func percentiles(ds []time.Duration) perfStats {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return perfStats{s[len(s)/2], s[(len(s)*95+99)/100-1], s[len(s)-1]}
}

func (p perfStats) String() string {
	r := func(d time.Duration) string { return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000) }
	return "p50=" + r(p.p50) + " p95=" + r(p.p95) + " max=" + r(p.max)
}

func TestPerformanceAt100k(t *testing.T) {
	if testing.Short() || os.Getenv("CHANDA_PERF") == "" {
		t.Skip("performance measurement (100k records); run with CHANDA_PERF=1 (make perf)")
	}
	total := 100_000
	if s := os.Getenv("CHANDA_PERF_RECORDS"); s != "" {
		fmt.Sscan(s, &total)
	}
	prev := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(prev)

	a, _ := newApp(t, func(c *config.Config) {
		c.Sleep.Scheduler = false
		if s := os.Getenv("CHANDA_PERF_CACHE_MB"); s != "" {
			fmt.Sscan(s, &c.Store.CacheMB)
		}
		if s := os.Getenv("CHANDA_PERF_MMAP_MB"); s != "" {
			fmt.Sscan(s, &c.Store.MmapMB)
		}
	})
	start := time.Now()
	n := perfSeed(t, a, total)
	t.Logf("seeded %d records (%d chunks, %d turns, %d facts) + %d vectors in %v", total, n.chunks, n.turns, n.facts, total,
		time.Since(start).Round(time.Millisecond))
	if _, err := a.e.Store.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(a.e.Cfg.DBPath); err == nil {
		t.Logf("database file: %d MB (store.cache_mb=%d, store.mmap_mb=%d)", fi.Size()>>20, a.e.Cfg.Store.CacheMB, a.e.Cfg.Store.MmapMB)
	}

	ctx := context.Background()
	qs := perfQueries()
	// Warm-up: loads the vector index and SQLite's page cache.
	for _, q := range qs {
		must(a.Search(agent, chanda.SearchInput{Query: q}))
		must(a.Context(agent, chanda.ContextInput{Query: q}))
	}
	if path := os.Getenv("CHANDA_PERF_CPUPROFILE"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			t.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); f.Close() }()
	}
	const rounds = 20
	var search, context []time.Duration
	hits := 0
	for i := 0; i < rounds*len(qs); i++ {
		q := qs[i%len(qs)]
		t0 := time.Now()
		s := must(a.Search(agent, chanda.SearchInput{Query: q}))
		search = append(search, time.Since(t0))
		t0 = time.Now()
		c := must(a.Context(agent, chanda.ContextInput{Query: q}))
		context = append(context, time.Since(t0))
		if i < len(qs) && len(s.Items) > 0 && c.Text != "" {
			hits++
		}
	}
	for j, q := range qs {
		var per []time.Duration
		for i := j; i < len(search); i += len(qs) {
			per = append(per, search[i], context[i])
		}
		t.Logf("  query %d %q: %s", j, q, percentiles(per))
	}
	ps, pc := percentiles(search), percentiles(context)
	t.Logf("search  (vectors, no reranker, GOMAXPROCS=4, %d queries): %s", len(search), ps)
	t.Logf("context (vectors, no reranker, GOMAXPROCS=4, %d queries): %s", len(context), pc)
	if hits < len(qs)/2 {
		t.Errorf("only %d of %d queries found something: the fixture does not exercise the read path", hits, len(qs))
	}

	// Write latency: remember of new session text (normalize, admit,
	// exact-duplicate check, FTS, extractors, near-duplicate guard, audit).
	g := perfRNG(99)
	var writes []time.Duration
	for i := 0; i < 200; i++ {
		text := perfText(&g, 20+int(g.next()%40))
		t0 := time.Now()
		must(a.Remember(agent, chanda.RememberInput{Content: text, SessionID: "perf"}))
		writes = append(writes, time.Since(t0))
	}
	t.Logf("remember (%d writes): %s", len(writes), percentiles(writes))

	var checks []time.Duration
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		rep, err := prov.Check(ctx, a.e.Store.DB(), "ns")
		checks = append(checks, time.Since(t0))
		if err != nil || !rep.OK {
			t.Fatalf("prov.Check: %v %+v", err, rep.Violations[:min(3, len(rep.Violations))])
		}
	}
	t.Logf("prov.Check (namespace, %d records): %s", total, percentiles(checks))
	t0 := time.Now()
	v := must(a.Verify(cli, chanda.VerifyInput{}))
	t.Logf("verify (audit chain of %d entries + prov.Check): %v", v.Audit.Entries, time.Since(t0).Round(time.Millisecond))

	if os.Getenv("CHANDA_PERF_STRICT") != "" && (ps.p95 > 150*time.Millisecond || pc.p95 > 150*time.Millisecond) {
		t.Errorf("p95 above the 150 ms target: search %v, context %v", ps.p95, pc.p95)
	}
}
