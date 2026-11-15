package lme

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// "lme evidence": turn-level evidence recall of contexts files.

// EvidenceRow is the part of a contexts line the evidence report reads.
type EvidenceRow struct {
	QuestionID             string        `json:"question_id"`
	QuestionType           string        `json:"question_type"`
	Condition              string        `json:"condition"`
	EvidenceTurns          int           `json:"evidence_turns"`
	EvidenceTurnsInContext int           `json:"evidence_turns_in_context"`
	EvidenceTurnRecall     *float64      `json:"evidence_turn_recall"`
	ContextItems           []ContextItem `json:"context_items"`
	ChandaSet              []string      `json:"chanda_set"`
	ChandaConfigHash       string        `json:"chanda_config_hash"`
}

// Scored reports whether the row has evidence turns and a recall.
func (r *EvidenceRow) Scored() bool { return r.EvidenceTurns > 0 && r.EvidenceTurnRecall != nil }

// ReadEvidenceRows reads a contexts JSONL file. A file written before the
// turn-level fields existed is refused.
func ReadEvidenceRows(path string) ([]*EvidenceRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var out []*EvidenceRow
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if _, ok := raw["evidence_turns"]; !ok {
			return nil, fmt.Errorf("%s:%d: no evidence_turns field (file written before turn-level recall; regenerate it)", path, n)
		}
		r := new(EvidenceRow)
		if err := json.Unmarshal([]byte(line), r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// EvidenceGroup is one row of the per-type table. Every column is over the
// scored questions (at least one evidence turn) of the group.
type EvidenceGroup struct {
	Group          string  `json:"group"`
	N              int     `json:"n_scored"`
	MeanRecall     float64 `json:"mean_turn_recall"`
	FullRecall     float64 `json:"share_full_recall"` // questions whose every evidence turn reached the block
	MeanItems      float64 `json:"mean_items"`
	TokensPerItem  float64 `json:"mean_tokens_per_item"`  // pooled: item tokens / items
	AssistantShare float64 `json:"assistant_token_share"` // pooled: assistant item tokens / item tokens
	UserShare      float64 `json:"user_token_share"`      // pooled: user item tokens / item tokens
	TruncatedShare float64 `json:"truncated_item_share"`  // pooled: truncated items / items
	EvidenceTurns  int     `json:"evidence_turns"`        // pooled
	TurnsInContext int     `json:"evidence_turns_in_context"`
}

// EvidenceFile is the report of one contexts file.
type EvidenceFile struct {
	File       string           `json:"file"`
	Condition  string           `json:"condition"`
	Questions  int              `json:"questions"`
	ChandaSet  []string         `json:"chanda_set,omitempty"`
	ConfigHash []string         `json:"chanda_config_hashes,omitempty"`
	Groups     []*EvidenceGroup `json:"groups"` // question types in name order, then "all"
}

// SummarizeEvidence builds the per-type table of one file.
func SummarizeEvidence(file string, rows []*EvidenceRow) *EvidenceFile {
	ef := &EvidenceFile{File: file, Questions: len(rows)}
	conds, sets, hashes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	type acc struct {
		n, full, items, trunc, ev, in int
		recall                        float64
		tokens, asst, user            int
	}
	groups := map[string]*acc{}
	add := func(g string, r *EvidenceRow) {
		a := groups[g]
		if a == nil {
			a = &acc{}
			groups[g] = a
		}
		a.n++
		a.recall += *r.EvidenceTurnRecall
		if r.EvidenceTurnsInContext == r.EvidenceTurns {
			a.full++
		}
		a.ev += r.EvidenceTurns
		a.in += r.EvidenceTurnsInContext
		a.items += len(r.ContextItems)
		for _, it := range r.ContextItems {
			a.tokens += it.Tokens
			if it.Truncated {
				a.trunc++
			}
			switch it.Role {
			case "assistant":
				a.asst += it.Tokens
			case "user":
				a.user += it.Tokens
			}
		}
	}
	for _, r := range rows {
		conds[r.Condition] = true
		sets[strings.Join(r.ChandaSet, " ")] = true
		if r.ChandaConfigHash != "" {
			hashes[r.ChandaConfigHash] = true
		}
		if !r.Scored() {
			continue
		}
		add(r.QuestionType, r)
		add("all", r)
	}
	ef.Condition = strings.Join(sortedKeys(conds), ",")
	if s := sortedKeys(sets); len(s) == 1 && s[0] != "" {
		ef.ChandaSet = strings.Fields(s[0])
	} else if len(s) > 1 {
		ef.ChandaSet = []string{"(mixed: " + strings.Join(s, " | ") + ")"}
	}
	ef.ConfigHash = sortedKeys(hashes)
	names := sortedKeys(groups)
	sort.SliceStable(names, func(i, j int) bool { return names[j] == "all" && names[i] != "all" })
	for _, g := range names {
		a := groups[g]
		eg := &EvidenceGroup{Group: g, N: a.n, EvidenceTurns: a.ev, TurnsInContext: a.in}
		eg.MeanRecall = a.recall / float64(a.n)
		eg.FullRecall = float64(a.full) / float64(a.n)
		eg.MeanItems = float64(a.items) / float64(a.n)
		if a.items > 0 {
			eg.TokensPerItem = float64(a.tokens) / float64(a.items)
			eg.TruncatedShare = float64(a.trunc) / float64(a.items)
		}
		if a.tokens > 0 {
			eg.AssistantShare = float64(a.asst) / float64(a.tokens)
			eg.UserShare = float64(a.user) / float64(a.tokens)
		}
		ef.Groups = append(ef.Groups, eg)
	}
	return ef
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EvidencePair compares two files on the questions scored in both.
type EvidencePair struct {
	FileA     string   `json:"a"`
	FileB     string   `json:"b"`
	N         int      `json:"n_paired"`
	BothFull  int      `json:"both_full"`
	Neither   int      `json:"neither_full"`
	OnlyA     []string `json:"a_full_b_not"` // question ids
	OnlyB     []string `json:"b_full_a_not"`
	MeanA     float64  `json:"mean_turn_recall_a"`
	MeanB     float64  `json:"mean_turn_recall_b"`
	HigherA   int      `json:"a_higher_recall"` // questions where A's turn recall is higher
	HigherB   int      `json:"b_higher_recall"`
	SignTestP float64  `json:"sign_test_p_full"` // exact two-sided sign test on OnlyA vs OnlyB
	OnlyInA   int      `json:"scored_only_in_a"`
	OnlyInB   int      `json:"scored_only_in_b"`
}

// PairEvidence compares file a with file b, question by question.
func PairEvidence(fa string, a []*EvidenceRow, fb string, b []*EvidenceRow) *EvidencePair {
	p := &EvidencePair{FileA: fa, FileB: fb, OnlyA: []string{}, OnlyB: []string{}}
	bm := map[string]*EvidenceRow{}
	for _, r := range b {
		if r.Scored() {
			bm[r.QuestionID] = r
		}
	}
	seen := map[string]bool{}
	for _, ra := range a {
		if !ra.Scored() {
			continue
		}
		rb := bm[ra.QuestionID]
		if rb == nil {
			p.OnlyInA++
			continue
		}
		seen[ra.QuestionID] = true
		p.N++
		fa, fb := ra.EvidenceTurnsInContext == ra.EvidenceTurns, rb.EvidenceTurnsInContext == rb.EvidenceTurns
		switch {
		case fa && fb:
			p.BothFull++
		case fa:
			p.OnlyA = append(p.OnlyA, ra.QuestionID)
		case fb:
			p.OnlyB = append(p.OnlyB, ra.QuestionID)
		default:
			p.Neither++
		}
		p.MeanA += *ra.EvidenceTurnRecall
		p.MeanB += *rb.EvidenceTurnRecall
		switch {
		case *ra.EvidenceTurnRecall > *rb.EvidenceTurnRecall:
			p.HigherA++
		case *ra.EvidenceTurnRecall < *rb.EvidenceTurnRecall:
			p.HigherB++
		}
	}
	for id := range bm {
		if !seen[id] {
			p.OnlyInB++
		}
	}
	if p.N > 0 {
		p.MeanA /= float64(p.N)
		p.MeanB /= float64(p.N)
	}
	p.SignTestP = SignTestP(len(p.OnlyA), len(p.OnlyB))
	return p
}

// SignTestP is the exact two-sided sign test p-value for k successes
// against m failures (ties dropped): min(1, 2 * P(X <= min(k, m))) with X ~
// Binomial(k+m, 1/2). It is 1 when there is no discordant pair.
func SignTestP(k, m int) float64 {
	n := k + m
	if n == 0 {
		return 1
	}
	lo := min(k, m)
	// P(X <= lo) = sum_{i=0}^{lo} C(n, i) / 2^n, in floating point by a
	// running binomial coefficient scaled by 2^-n.
	c := 1.0
	for i := 0; i < n; i++ {
		c /= 2
	}
	sum := 0.0
	for i := 0; i <= lo; i++ {
		sum += c
		c = c * float64(n-i) / float64(i+1)
	}
	return min(1, 2*sum)
}

// EvidenceMarkdown renders the reports and pairs as text tables.
func EvidenceMarkdown(files []*EvidenceFile, pairs []*EvidencePair) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "## %s (condition %s, %d questions)\n\n", f.File, f.Condition, f.Questions)
		if len(f.ChandaSet) > 0 {
			fmt.Fprintf(&b, "chanda-set: %s\n", strings.Join(f.ChandaSet, " "))
		}
		if len(f.ConfigHash) > 0 {
			fmt.Fprintf(&b, "chanda config hash: %s\n", strings.Join(f.ConfigHash, ", "))
		}
		if len(f.ChandaSet) > 0 || len(f.ConfigHash) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("| type | n scored | turn recall | full recall | items | tokens/item | assistant tokens | truncated items |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, g := range f.Groups {
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %.1f | %.1f | %s | %s |\n", g.Group, g.N, pct(g.MeanRecall), pct(g.FullRecall),
				g.MeanItems, g.TokensPerItem, pct(g.AssistantShare), pct(g.TruncatedShare))
		}
		b.WriteString("\n")
	}
	for _, p := range pairs {
		fmt.Fprintf(&b, "## Paired: A = %s, B = %s\n\n", p.FileA, p.FileB)
		fmt.Fprintf(&b, "%d questions scored in both (only in A: %d, only in B: %d). Mean turn recall A %s, B %s; higher in A %d, in B %d.\n",
			p.N, p.OnlyInA, p.OnlyInB, pct(p.MeanA), pct(p.MeanB), p.HigherA, p.HigherB)
		fmt.Fprintf(&b, "Full recall: both %d, neither %d, A only %d, B only %d (exact sign test p = %.3g).\n",
			p.BothFull, p.Neither, len(p.OnlyA), len(p.OnlyB), p.SignTestP)
		if len(p.OnlyA) > 0 {
			fmt.Fprintf(&b, "A full, B not: %s\n", strings.Join(p.OnlyA, " "))
		}
		if len(p.OnlyB) > 0 {
			fmt.Fprintf(&b, "B full, A not: %s\n", strings.Join(p.OnlyB, " "))
		}
		b.WriteString("\n")
	}
	return b.String()
}
