package sleep

import (
	"fmt"
	"sort"
	"strings"
)

// renderReport writes the readable run report (sleep_runs.report). It holds
// ids, counts, predicate-free action labels and cost — never record content,
// so forgetting a record never has to rewrite reports.
func renderReport(r *Result) string {
	var b strings.Builder
	c := r.Counts
	fmt.Fprintf(&b, "# Sleep run %s\n\n", r.RunID)
	fmt.Fprintf(&b, "- Namespace: %s\n- Status: %s\n- Phases: %s\n- Budget: %d tokens, %d events\n",
		r.Namespace, r.Status, joinInts(r.Phases), r.Budget.Tokens, r.Budget.Events)
	if r.Snapshot != "" {
		fmt.Fprintf(&b, "- Snapshot: %s\n", r.Snapshot)
	}

	b.WriteString("\n## Phase 1\n\n")
	fmt.Fprintf(&b, "- Integrity violations: %d\n", c.IntegrityViolations)
	list(&b, "Merge proposals (pending)", c.MergeProposals)
	list(&b, "Facts marked stale_source", c.StaleFacts)
	list(&b, "Pages rebuilt", c.PagesRebuilt)
	list(&b, "Gate questions proposed (awaiting a human)", c.GateProposed)
	if l := c.Learning; l.Trained != "" || l.Evaluated != "" || len(l.Notices) > 0 {
		fmt.Fprintf(&b, "- Ranker: trained %s, evaluated %s, passed %t, promoted %t, baseline %.2f, learned %.2f, lambda %.2f",
			orNone(l.Trained), orNone(l.Evaluated), l.Passed, l.Promoted, l.Baseline, l.Learned, l.Lambda)
		if len(l.Notices) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(l.Notices, ", "))
		}
		b.WriteByte('\n')
	}

	if len(r.Phases) == 2 {
		b.WriteString("\n## Phase 2\n\n")
		if r.Gate.Reverted {
			b.WriteString("Everything below was staged and then discarded by the gate.\n\n")
		}
		fmt.Fprintf(&b, "- Events extracted: %d\n", c.EventsExtracted)
		list(&b, "Events skipped (larger than the whole budget)", c.EventsSkipped)
		fmt.Fprintf(&b, "- Facts from extraction: %s\n", actions(c.FactActions))
		fmt.Fprintf(&b, "- Candidates dropped (bad origin or empty field): %d\n", c.CandidatesDrop)
		fmt.Fprintf(&b, "- Verdicts: %s\n", actions(c.Verdicts))
		list(&b, "Created (quarantined facts activated)", c.FactsActivated)
		list(&b, "Superseded (closed by a judged fact or a merge)", c.FactsSuperseded)
		list(&b, "Kept as history", c.FactsHistory)
		list(&b, "Rejected facts", c.FactsRejected)
		list(&b, "Verdicts refused by the zone and cardinality rules", c.VerdictsRefused)
		list(&b, "Merged (proposals accepted)", c.MergesApplied)
		list(&b, "Quarantined by a merge (lower zone)", c.FactsQuarantined)
		list(&b, "Merge proposals rejected", c.MergesRejected)
		list(&b, "Pages created", c.PagesCreated)
		list(&b, "Page items added", c.PageItemsAdded)
		fmt.Fprintf(&b, "- Lesson clusters skipped by the model: %d\n", c.LessonsSkipped)
		fmt.Fprintf(&b, "- Invalid LLM answers discarded (no retry): %d\n", c.InvalidAnswers)
		fmt.Fprintf(&b, "- LLM errors: %d\n", c.LLMErrors)
	}

	if r.Gate.Ran {
		b.WriteString("\n## Gate\n\n")
		fmt.Fprintf(&b, "- Score before: %.2f, after: %.2f, tolerance: %.2f points\n", r.Gate.Before, r.Gate.After, r.Gate.Tolerance)
		fmt.Fprintf(&b, "- Integrity violations before: %d, after: %d\n", r.Gate.ViolationsBefore, r.Gate.ViolationsAfter)
		if r.Gate.Reverted {
			fmt.Fprintf(&b, "- Result: reverted (%s); staged changes discarded, database back to the snapshot state\n", r.Gate.Reason)
		} else {
			b.WriteString("- Result: passed; staged changes committed\n")
		}
	}

	b.WriteString("\n## Cost\n\n")
	fmt.Fprintf(&b, "- LLM calls: %d\n- Tokens: %d in, %d out, %d total\n",
		r.Cost.LLMCalls, r.Cost.InputTokens, r.Cost.OutputTokens, r.Cost.Tokens())

	if len(r.Notices) > 0 {
		b.WriteString("\n## Notices\n\n")
		for _, n := range r.Notices {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func list(b *strings.Builder, title string, ids []string) {
	fmt.Fprintf(b, "- %s: %d", title, len(ids))
	if len(ids) > 0 {
		fmt.Fprintf(b, " (%s)", strings.Join(ids, ", "))
	}
	b.WriteByte('\n')
}

func actions(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(parts, ", ")
}

func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprint(x)
	}
	return strings.Join(parts, ", ")
}
