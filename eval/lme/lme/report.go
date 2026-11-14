package lme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CondSummary aggregates one condition over a run.
type CondSummary struct {
	Groups       map[string]*Agg `json:"groups"` // "all", each type, "abstention" (count only)
	RetrievalMS  Dist            `json:"retrieval_ms"`
	IngestMS     Dist            `json:"ingest_ms"`
	DBBytes      *Dist           `json:"db_bytes,omitempty"`
	DiskBytes    *Dist           `json:"disk_bytes,omitempty"`
	NRanked      Dist            `json:"sessions_ranked"`
	BudgetRecall *float64        `json:"budget_recall,omitempty"` // RAG: mean evidence recall inside the context
	ContextToks  *Dist           `json:"context_tokens,omitempty"`
	Errors       int             `json:"errors"`
}

// Phase1Report is phase1.json.
type Phase1Report struct {
	Meta       map[string]any          `json:"meta"`
	Conditions map[string]*CondSummary `json:"conditions"`
}

// Summarize aggregates per-question results.
func Summarize(qs []*P1Question, conds []string) map[string]*CondSummary {
	out := map[string]*CondSummary{}
	for _, c := range conds {
		var rs []QResult
		var lat, ing, db, disk, nr, ctx []float64
		var br []float64
		cs := &CondSummary{}
		for _, q := range qs {
			pc := q.Conds[c]
			if pc == nil {
				continue
			}
			if pc.Err != "" {
				cs.Errors++
				continue
			}
			rs = append(rs, QResult{Type: q.QuestionType, Abstention: q.Abstention, HasOfficial: len(q.Official) > 0, M: pc.Metrics})
			lat = append(lat, pc.RetrievalMS)
			ing = append(ing, pc.IngestMS)
			nr = append(nr, float64(pc.NRanked))
			if pc.DBBytes > 0 {
				db = append(db, float64(pc.DBBytes))
			}
			if pc.DiskBytes > 0 {
				disk = append(disk, float64(pc.DiskBytes))
			}
			if pc.BudgetRecall != nil {
				br = append(br, *pc.BudgetRecall)
			}
			if pc.ContextTokens > 0 {
				ctx = append(ctx, float64(pc.ContextTokens))
			}
		}
		cs.Groups = Aggregate(rs)
		cs.RetrievalMS, cs.IngestMS, cs.NRanked = NewDist(lat), NewDist(ing), NewDist(nr)
		if len(db) > 0 {
			d := NewDist(db)
			cs.DBBytes = &d
		}
		if len(disk) > 0 {
			d := NewDist(disk)
			cs.DiskBytes = &d
		}
		if len(br) > 0 {
			m := NewDist(br).Mean
			cs.BudgetRecall = &m
		}
		if len(ctx) > 0 {
			d := NewDist(ctx)
			cs.ContextToks = &d
		}
		out[c] = cs
	}
	return out
}

// WritePhase1 writes phase1.json, phase1.md and phase1.questions.jsonl in dir.
func WritePhase1(dir string, rep *Phase1Report, qs []*P1Question, conds []string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "phase1.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	var lines strings.Builder
	for _, q := range qs {
		l, err := json.Marshal(q)
		if err != nil {
			return err
		}
		lines.Write(l)
		lines.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "phase1.questions.jsonl"), []byte(lines.String()), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "phase1.md"), []byte(Phase1Markdown(rep, conds)), 0o644)
}

func pct(x float64) string { return fmt.Sprintf("%.1f", 100*x) }

// Phase1Markdown renders the report tables.
func Phase1Markdown(rep *Phase1Report, conds []string) string {
	var b strings.Builder
	b.WriteString("# LongMemEval phase 1: retrieval without LLM\n\n")
	keys := make([]string, 0, len(rep.Meta))
	for k := range rep.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s: %v\n", k, rep.Meta[k])
	}
	b.WriteString("\nRecall@k = share of the evidence sessions (answer_session_ids) among the first k distinct sessions ranked; MRR = 1/rank of the first evidence session. ")
	b.WriteString("Abstention questions are excluded from every metric (official README: they have no evidence location). ")
	b.WriteString("Official = LongMemEval session-level recall_any/recall_all/ndcg_any (src/retrieval/eval_utils.py) over the official targets; questions without a target are skipped, as run_retrieval.py does.\n\n")
	b.WriteString("## Summary (all non-abstention questions)\n\n")
	b.WriteString("| condition | n | recall@5 | recall@10 | MRR | official n | recall_any@5 | recall_all@5 | ndcg_any@5 | recall_any@10 | recall_all@10 | ndcg_any@10 |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, c := range conds {
		cs := rep.Conditions[c]
		a := cs.Groups["all"]
		if a == nil {
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %.3f | %d | %s | %s | %.3f | %s | %s | %.3f |\n", c, a.N,
			pct(a.Recall["@5"]), pct(a.Recall["@10"]), a.MRR, a.NOfficial,
			pct(a.RecallAny["@5"]), pct(a.RecallAll["@5"]), a.NDCGAny["@5"],
			pct(a.RecallAny["@10"]), pct(a.RecallAll["@10"]), a.NDCGAny["@10"])
	}
	b.WriteString("\n## By question type (recall@5 / recall@10 / MRR)\n\n")
	b.WriteString("| type | n |")
	for _, c := range conds {
		fmt.Fprintf(&b, " %s |", c)
	}
	b.WriteString("\n| --- | --- |")
	for range conds {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	var groups []string
	if len(conds) > 0 {
		groups = Groups(rep.Conditions[conds[0]].Groups)
	}
	for _, g := range groups {
		if g == Abstention {
			continue
		}
		n := 0
		if a := rep.Conditions[conds[0]].Groups[g]; a != nil {
			n = a.N
		}
		fmt.Fprintf(&b, "| %s | %d |", g, n)
		for _, c := range conds {
			a := rep.Conditions[c].Groups[g]
			if a == nil {
				b.WriteString(" - |")
				continue
			}
			fmt.Fprintf(&b, " %s / %s / %.3f |", pct(a.Recall["@5"]), pct(a.Recall["@10"]), a.MRR)
		}
		b.WriteString("\n")
	}
	if a := rep.Conditions[conds[0]].Groups[Abstention]; a != nil {
		fmt.Fprintf(&b, "\nAbstention questions run but not scored: %d.\n", a.N)
	}
	b.WriteString("\n## Cost and latency\n\n")
	b.WriteString("| condition | retrieval p50 ms | retrieval p95 ms | ingest/index p50 ms | ingest/index p95 ms | sessions ranked (mean) | DB bytes p50 | DB bytes max | budget recall | context tokens (mean) | errors |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, c := range conds {
		cs := rep.Conditions[c]
		db50, dbmax, brs, ctx := "-", "-", "-", "-"
		if cs.DiskBytes != nil {
			db50, dbmax = fmt.Sprintf("%.0f", cs.DiskBytes.Median), fmt.Sprintf("%.0f", cs.DiskBytes.Max)
		}
		if cs.BudgetRecall != nil {
			brs = pct(*cs.BudgetRecall)
		}
		if cs.ContextToks != nil {
			ctx = fmt.Sprintf("%.0f", cs.ContextToks.Mean)
		}
		fmt.Fprintf(&b, "| %s | %.1f | %.1f | %.0f | %.0f | %.1f | %s | %s | %s | %s | %d |\n", c,
			cs.RetrievalMS.Median, cs.RetrievalMS.P95, cs.IngestMS.Median, cs.IngestMS.P95, cs.NRanked.Mean, db50, dbmax, brs, ctx, cs.Errors)
	}
	b.WriteString("\nRetrieval latency: BM25 = the FTS5 query; RAG = query embedding + exact scan + sort + budget fill (chunk embeddings are ingest); Chanda = the search tool call round trip over MCP stdio. ")
	b.WriteString("Chanda ingest = every log_session call plus flush (embedding included). DB bytes = Chanda's .db + -wal + -shm on disk after the run.\n")
	return b.String()
}
