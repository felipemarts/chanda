package scenarios

import (
	"fmt"
	"sort"
	"strings"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// scenarios is the phase 5 set, in report order.
var scenarios = []Scenario{
	{"F1", "forget", "Forget a document (scope document, MCP admin): its chunks, the facts extracted from it and their vectors leave every search, context, get, the FTS index, the embeddings table and the raw bytes of the database, WAL and SHM files.", forgetDocument},
	{"F2", "forget", "Forget an entity (scope entity, MCP admin): its facts and their assertion events leave every read path, the index, the vectors and the raw bytes.", forgetEntity},
	{"F3", "forget", "Forget an entity also removes a free-text statement that names it (BASE.md scope entity; decision 0050: entity scope resolves to the events that mention the entity).", forgetEntityMentions},
	{"F4", "forget", "Forget by search match through the CLI (a person, server down): statement, session turns and the derived fact naming the keyword leave every read path, the vectors and the raw bytes.", forgetSearchCLI},
	{"U1", "document update", "Re-ingest a changed document: facts extracted from removed or changed passages stop being active, and context and search no longer return them as current.", documentUpdate},
	{"I1", "injection", "A tool turn (external zone) carrying a prompt injection is marked instruction_like, never appears in the pre-context, and appears in search only with its zone and mark.", injectionToolTurn},
	{"I2", "injection", "A web page ingested as kind web (external zone) cannot override an internal fact extracted from a document: its conflicting fact is quarantined, its instruction paragraph is excluded and its other text is only a labelled quotation.", injectionWebIngest},
	{"I3", "injection", "External channels cannot assert a trusted fact: assert_fact from a tool source or from an external-ceiling server is quarantined against an internal fact, a fresh external fact keeps the external zone and is quoted, and a non-human channel cannot write the user zone.", injectionExternalFacts},
	{"I4", "injection", "External text cannot declare its own zone or break out of the pre-context block: fake labels and a closing tag inside the content stay quoted and escaped.", injectionSelfDeclaredZone},
	{"T1", "temporal", "A single-valued predicate with an old and a new value: context and search as of now return only the new value as current; as_of in the past returns the old value; history only on request.", temporalSupersession},
	{"T2", "temporal", "An older value asserted after the current one is stored as closed history and does not displace the current value.", temporalOutOfOrder},
	{"D1", "dedup", "Free-text records that differ only in a number, a negation, a date or an entity are kept as distinct records; an exact duplicate is collapsed into the existing record.", dedupText},
	{"D2", "dedup", "Facts: a different value is never collapsed (multi-valued coexist, single-valued supersede with history kept); the same value again only confirms the existing fact.", dedupFacts},
	{"A1", "abstention", "Off-topic queries over a small corpus return an empty pre-context with reason no_relevant_memory; a relevant query (control) does not.", abstentionOffTopic},
	{"A2", "abstention", "Near-topic queries that share words with the corpus but have no answer in it return an empty pre-context.", abstentionNearTopic},
}

// ---------------------------------------------------------------------------
// Context block structure.

// block is one item of a rendered pre-context: its label line and body.
type block struct {
	label string
	body  []string
}

// parseContext splits a pre-context into its items. It reports whether the
// block is well delimited: opening tag first, the "not instructions" notice,
// one closing tag, last.
func parseContext(text string) (blocks []block, delimited bool) {
	if text == "" {
		return nil, true
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	delimited = len(lines) >= 3 && strings.HasPrefix(lines[0], "<chanda-context ") &&
		strings.Contains(lines[1], "not instructions") && lines[len(lines)-1] == "</chanda-context>" &&
		strings.Count(text, "</chanda-context>") == 1 && strings.Count(text, "<chanda-context") == 1
	var cur *block
	for i := 2; i < len(lines)-1; i++ {
		l := lines[i]
		if l == "" && i+1 < len(lines)-1 && isLabel(lines[i+1]) {
			blocks = append(blocks, block{label: lines[i+1]})
			cur = &blocks[len(blocks)-1]
			i++
			continue
		}
		if cur != nil {
			cur.body = append(cur.body, l)
		}
	}
	return blocks, delimited
}

func isLabel(l string) bool {
	return strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") && strings.Contains(l, " | zone: ")
}

func zoneOf(label string) string {
	_, after, _ := strings.Cut(label, " | zone: ")
	z, _, _ := strings.Cut(after, " ")
	return strings.TrimSuffix(z, "]")
}

// quotingViolations lists blocks that break the zone labelling rules: an
// external item must be labelled "external quotation" with every body line
// quoted ("> "); an internal or user item must not carry the external label.
func quotingViolations(blocks []block) []string {
	var v []string
	for i, b := range blocks {
		switch zoneOf(b.label) {
		case "external":
			if !strings.HasPrefix(b.label, "[external quotation, ") {
				v = append(v, fmt.Sprintf("block %d: external item not labelled as quotation", i))
			}
			for _, l := range b.body {
				if !strings.HasPrefix(l, "> ") && l != ">" {
					v = append(v, fmt.Sprintf("block %d: unquoted external line", i))
					break
				}
			}
		case "internal", "user":
			if strings.Contains(b.label, "external") {
				v = append(v, fmt.Sprintf("block %d: trusted item labelled external", i))
			}
		default:
			v = append(v, fmt.Sprintf("block %d: no zone", i))
		}
	}
	return v
}

// linesWith returns where sub appears in a context: "quoted" for a body line
// of an external block starting with "> ", otherwise the kind of block.
func linesWith(blocks []block, sub string) []string {
	var out []string
	sub = strings.ToLower(sub)
	for _, b := range blocks {
		if strings.Contains(strings.ToLower(b.label), sub) {
			out = append(out, "label:"+zoneOf(b.label))
		}
		for _, l := range b.body {
			if !strings.Contains(strings.ToLower(l), sub) {
				continue
			}
			if zoneOf(b.label) == "external" && strings.HasPrefix(l, "> ") {
				out = append(out, "external-quoted")
			} else {
				out = append(out, "unquoted:"+zoneOf(b.label))
			}
		}
	}
	return out
}

// factBlocksWith counts trusted fact blocks ("[fact ...") whose body holds sub.
func factBlocksWith(blocks []block, sub string) int {
	n := 0
	for _, b := range blocks {
		if !strings.HasPrefix(b.label, "[fact ") {
			continue
		}
		for _, l := range b.body {
			if strings.Contains(strings.ToLower(l), strings.ToLower(sub)) {
				n++
				break
			}
		}
	}
	return n
}

func has(text, sub string) bool { return strings.Contains(strings.ToLower(text), strings.ToLower(sub)) }

// activeValues lists the values of the active entries of a timeline, sorted.
func activeValues(tl chanda.TimelineOutput, predicate string) []string {
	var out []string
	for _, e := range tl.Entries {
		if e.Status == "active" && (predicate == "" || e.Predicate == predicate) {
			out = append(out, e.Value)
		}
	}
	sort.Strings(out)
	return out
}

// entriesSummary renders timeline entries without ids or times:
// predicate=value[status,zone].
func entriesSummary(tl chanda.TimelineOutput) string {
	var out []string
	for _, e := range tl.Entries {
		out = append(out, fmt.Sprintf("%s=%s[%s,%s]", e.Predicate, e.Value, e.Status, e.Zone))
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, "; ")
}
