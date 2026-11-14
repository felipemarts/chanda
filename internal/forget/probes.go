package forget

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/env"
)

// Probe names.
const (
	ProbeFTS        = "fts"        // distinctive terms no longer find removed records
	ProbeVector     = "vector"     // saved vectors no longer find removed records
	ProbeBytes      = "bytes"      // removed byte strings absent from the database and WAL files
	ProbeReferences = "references" // no table row references a removed id
	ProbeSnapshots  = "snapshots"  // snapshot files that could hold the content are gone
)

// Probe is the outcome of one verification probe. Failures name ids, tables
// and files, never content.
type Probe struct {
	Name     string   `json:"name"`
	Pass     bool     `json:"pass"`
	Checked  int      `json:"checked"`
	Failures []string `json:"failures,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// Verification is the report forget answers with (BASE.md "Forgetting" step 7).
type Verification struct {
	OK     bool    `json:"ok"`
	Probes []Probe `json:"probes"`
}

// ftsTables are the full-text indexes, maintained by triggers.
var ftsTables = []string{"events_fts", "facts_fts", "pages_fts"}

// scrubFile purges deleted documents from the FTS segments, folds and
// truncates the WAL, and rebuilds the file so no free page keeps old bytes
// (secure_delete is on from store.Open).
func scrubFile(ctx context.Context, db *sql.DB) error {
	for _, t := range ftsTables {
		if _, err := db.ExecContext(ctx, `INSERT INTO `+t+`(`+t+`) VALUES('optimize')`); err != nil {
			return err
		}
	}
	if err := checkpoint(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	return checkpoint(ctx, db)
}

func checkpoint(ctx context.Context, db *sql.DB) error {
	var busy, logN, done int
	if err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logN, &done); err != nil {
		return err
	}
	if busy != 0 {
		return fmt.Errorf("wal checkpoint busy")
	}
	return nil
}

// verify runs every probe after deletion.
func (p *plan) verify(ctx context.Context, e *env.Env, ns, dbPath string) (*Verification, error) {
	db := e.Store.DB()
	v := &Verification{OK: true}
	for _, run := range []func() (Probe, error){
		func() (Probe, error) { return p.probeFTS(ctx, db, ns) },
		func() (Probe, error) { return p.probeVector(ctx, db, e.Cfg.Forget.ProbeVectorK) },
		func() (Probe, error) { return p.probeBytes(ctx, db, dbPath) },
		func() (Probe, error) { return p.probeReferences(ctx, db) },
		func() (Probe, error) { return p.probeSnapshots(), nil },
	} {
		pr, err := run()
		if err != nil {
			return nil, err
		}
		pr.Pass = len(pr.Failures) == 0
		v.OK = v.OK && pr.Pass
		v.Probes = append(v.Probes, pr)
	}
	return v, nil
}

// probeFTS searches each removed event's rarest terms in every FTS index:
// no hit may be a removed record. It also runs the FTS5 integrity check.
func (p *plan) probeFTS(ctx context.Context, db *sql.DB, ns string) (Probe, error) {
	pr := Probe{Name: ProbeFTS}
	noTerms := 0
	for _, ev := range p.removed.Events {
		if len(p.terms[ev]) == 0 {
			noTerms++
		}
		for _, term := range p.terms[ev] {
			for _, t := range ftsTables {
				hits, err := ids(ctx, db, `SELECT record_id FROM `+t+` WHERE `+t+` MATCH ? AND namespace = ? ORDER BY record_id`, ftsPhrase(term), ns)
				if err != nil {
					return pr, err
				}
				pr.Checked++
				for _, h := range hits {
					if p.gone[h] {
						pr.Failures = append(pr.Failures, fmt.Sprintf("%s: a term of %s still finds %s", t, ev, h))
					}
				}
			}
		}
	}
	for _, t := range ftsTables {
		if _, err := db.ExecContext(ctx, `INSERT INTO `+t+`(`+t+`) VALUES('integrity-check')`); err != nil {
			pr.Failures = append(pr.Failures, t+": integrity-check failed")
		}
	}
	if noTerms > 0 {
		pr.Note = fmt.Sprintf("%d removed events had no indexed term", noTerms)
	}
	return pr, nil
}

// probeVector runs an exact nearest-neighbour scan with every vector saved
// before deletion: no neighbour may be a removed record or a rebuilt page.
func (p *plan) probeVector(ctx context.Context, db *sql.DB, k int) (Probe, error) {
	pr := Probe{Name: ProbeVector}
	banned := set(p.rebuild)
	for id := range p.gone {
		banned[id] = true
	}
	type row struct {
		id  string
		vec []float32
	}
	cache := map[string][]row{}
	for _, sv := range p.vectors {
		key := sv.ns + "\x00" + sv.model
		rowsFor, ok := cache[key]
		if !ok {
			rs, err := db.QueryContext(ctx, `SELECT record_id, vector FROM embeddings WHERE namespace = ? AND model_id = ? ORDER BY record_type, record_id`, sv.ns, sv.model)
			if err != nil {
				return pr, err
			}
			for rs.Next() {
				var r row
				var b []byte
				if err := rs.Scan(&r.id, &b); err != nil {
					rs.Close()
					return pr, err
				}
				r.vec = decodeVector(b)
				rowsFor = append(rowsFor, r)
			}
			if err := rs.Close(); err != nil {
				return pr, err
			}
			cache[key] = rowsFor
		}
		type hit struct {
			id    string
			score float64
		}
		hits := make([]hit, 0, len(rowsFor))
		for _, r := range rowsFor {
			if len(r.vec) != len(sv.vec) {
				continue
			}
			var dot float64
			for i := range r.vec {
				dot += float64(r.vec[i]) * float64(sv.vec[i])
			}
			hits = append(hits, hit{r.id, dot})
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			return hits[i].id < hits[j].id
		})
		if len(hits) > k {
			hits = hits[:k]
		}
		pr.Checked++
		for _, h := range hits {
			if banned[h.id] {
				pr.Failures = append(pr.Failures, fmt.Sprintf("vector of %s still finds %s", sv.owner, h.id))
			}
		}
	}
	return pr, nil
}

// survivorText lists the columns where forgotten bytes may legitimately
// remain: the same text held by a record that was not forgotten. A chunk's
// heading_path is text of that chunk: an entity scope removes the facts
// about a heading's subject and their chunks, not every paragraph under the
// heading, and those paragraphs keep the heading (decision 0110).
var survivorText = []string{
	`SELECT 1 FROM events WHERE instr(content, ?1) > 0 OR instr(heading_path, ?1) > 0`,
	`SELECT 1 FROM facts WHERE instr(text, ?1) > 0 OR instr(value, ?1) > 0`,
	`SELECT 1 FROM pages WHERE instr(title, ?1) > 0 OR instr(body, ?1) > 0`,
	`SELECT 1 FROM page_items WHERE instr(text, ?1) > 0`,
	`SELECT 1 FROM entities WHERE instr(canonical_name, ?1) > 0`,
	`SELECT 1 FROM entity_aliases WHERE instr(alias_norm, ?1) > 0`,
	`SELECT 1 FROM sources WHERE instr(uri, ?1) > 0`,
	`SELECT 1 FROM documents WHERE instr(uri, ?1) > 0`,
}

// probeBytes scans the database file and its WAL for byte strings of every
// removed text. Strings that a surviving record still holds are skipped.
func (p *plan) probeBytes(ctx context.Context, db *sql.DB, dbPath string) (Probe, error) {
	pr := Probe{Name: ProbeBytes}
	if dbPath == "" || dbPath == ":memory:" {
		pr.Note = "in-memory database: no file to scan"
		return pr, nil
	}
	var scan []needle
	skipped := 0
	for _, n := range p.needles {
		var found int
		q := `SELECT EXISTS(` + strings.Join(survivorText, ` UNION ALL `) + `)`
		if err := db.QueryRowContext(ctx, q, n.b).Scan(&found); err != nil {
			return pr, err
		}
		if found == 1 {
			skipped++
			continue
		}
		scan = append(scan, n)
	}
	pr.Checked = len(scan)
	for _, f := range []string{dbPath, dbPath + "-wal"} {
		hits, err := scanFile(f, scan)
		if err != nil {
			return pr, err
		}
		for i, hit := range hits {
			if hit {
				pr.Failures = append(pr.Failures, fmt.Sprintf("%s: byte string %d of %s found", filepath.Base(f), i, scan[i].owner))
			}
		}
	}
	if skipped > 0 {
		pr.Note = fmt.Sprintf("%d byte strings skipped: also held by surviving records", skipped)
	}
	return pr, nil
}

// scanFile reports, per needle, whether its bytes occur in the file. It
// streams the file with an overlap so a needle spanning two reads is found.
// A missing file contains nothing.
func scanFile(path string, ns []needle) ([]bool, error) {
	found := make([]bool, len(ns))
	if len(ns) == 0 {
		return found, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return found, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	maxLen := 0
	for _, n := range ns {
		if len(n.b) > maxLen {
			maxLen = len(n.b)
		}
	}
	buf := make([]byte, 0, (1<<20)+maxLen)
	chunk := make([]byte, 1<<20)
	for {
		k, err := f.Read(chunk)
		buf = append(buf, chunk[:k]...)
		for i, n := range ns {
			if !found[i] && bytes.Contains(buf, []byte(n.b)) {
				found[i] = true
			}
		}
		if len(buf) > maxLen {
			buf = append(buf[:0], buf[len(buf)-maxLen:]...)
		}
		if err == io.EOF {
			return found, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// probeReferences looks for removed ids in every column of every table. The
// audit log and the tombstones are exempt: they hold ids (never content) as
// the proof of what happened. Virtual FTS tables are covered by their shadow
// tables.
func (p *plan) probeReferences(ctx context.Context, db *sql.DB) (Probe, error) {
	pr := Probe{Name: ProbeReferences}
	conn, err := db.Conn(ctx) // pin the connection that owns the temp table
	if err != nil {
		return pr, err
	}
	defer conn.Close()
	exec := func(q string, args ...any) error { _, err := conn.ExecContext(ctx, q, args...); return err }
	if err := exec(`CREATE TEMP TABLE IF NOT EXISTS forget_probe_ids(id TEXT PRIMARY KEY)`); err != nil {
		return pr, err
	}
	defer exec(`DROP TABLE IF EXISTS temp.forget_probe_ids`)
	if err := exec(`DELETE FROM temp.forget_probe_ids`); err != nil {
		return pr, err
	}
	for _, id := range sortedKeys(p.gone) {
		if err := exec(`INSERT INTO temp.forget_probe_ids(id) VALUES (?)`, id); err != nil {
			return pr, err
		}
	}
	rows, err := conn.QueryContext(ctx, `SELECT name FROM main.sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		AND name NOT IN ('audit_log', 'tombstones') AND sql NOT LIKE 'CREATE VIRTUAL TABLE%' ORDER BY name`)
	if err != nil {
		return pr, err
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return pr, err
		}
		tables = append(tables, n)
	}
	if err := rows.Close(); err != nil {
		return pr, err
	}
	for _, t := range tables {
		cols, err := columns(ctx, conn, t)
		if err != nil {
			return pr, err
		}
		for _, c := range cols {
			var n int
			q := fmt.Sprintf(`SELECT count(*) FROM main.%q AS t JOIN temp.forget_probe_ids AS f ON instr(CAST(t.%q AS TEXT), f.id) > 0`, t, c)
			if err := conn.QueryRowContext(ctx, q).Scan(&n); err != nil {
				return pr, err
			}
			pr.Checked++
			if n > 0 {
				pr.Failures = append(pr.Failures, fmt.Sprintf("%s.%s: %d references to removed ids", t, c, n))
			}
		}
	}
	return pr, nil
}

func columns(ctx context.Context, conn *sql.Conn, table string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// probeSnapshots confirms the snapshot files listed before deletion are gone.
func (p *plan) probeSnapshots() Probe {
	pr := Probe{Name: ProbeSnapshots, Checked: len(p.snapshots)}
	for _, f := range p.snapshots {
		if _, err := os.Stat(f); err == nil {
			pr.Failures = append(pr.Failures, "snapshot still present: "+filepath.Base(f))
		}
	}
	return pr
}
