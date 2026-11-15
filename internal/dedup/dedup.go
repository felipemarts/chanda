// Package dedup is the near-duplicate guard (BASE.md write step 10,
// "Near-duplicate guard").
//
// Two records are near-duplicates only if their cosine similarity exceeds
// the threshold (cfg.Write.NearDupThreshold, default 0.95; or, on the
// lexical path of decision 0138, a lower per-model floor when the newer
// text repeats every word of the older one and adds only a few) and their
// signatures — numbers, dates, entities and negation markers, after
// normalization — are identical. "The price is 10" and "the price is 12"
// are similar and are not duplicates; "O preço é 10" and "O preço não é 10"
// are not duplicates either.
//
// Order on the write path (principle 7, contradiction before duplication).
// The integration must call, inside the same transaction:
//
//  1. facts.Service.Assert / AfterEvent for the new record (step 9). A record
//     that changes a value becomes a new fact that supersedes, history or
//     quarantine — it is an update, not a copy.
//  2. Only then, for the new record's nearest neighbours above the
//     threshold (from the vector index), IsNearDuplicate on the two texts;
//     when it returns true, Guard.Mark(new → older) (step 10).
//
// A record is never deleted here: it receives a duplicate_of edge and the
// read path collapses it into the most recent record. Never mark a fact as
// a duplicate of a fact with a different value_norm: Assert already decided
// that case.
package dedup

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/felipemarts/chanda-mcp/internal/audit"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/env"
	"github.com/felipemarts/chanda-mcp/internal/facts/norm"
)

// Sig is the part of a text that must match exactly for two similar texts
// to count as duplicates. Numbers and Dates keep their order of appearance;
// Entities is a sorted set; Negations is a sorted multiset.
type Sig struct {
	Numbers   []string `json:"numbers"`
	Dates     []string `json:"dates"`
	Entities  []string `json:"entities"`
	Negations []string `json:"negations"`
}

// negations are the markers, after norm.Alias (accents stripped).
var negations = map[string]bool{
	"not": true, "no": true, "never": true, "cannot": true,
	"nao": true, "nunca": true, "nem": true, "sem": true, "jamais": true,
}

var reNumber = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// Signature extracts the signature of text. aliases are known entity names
// (for example from entity_aliases); each one found in the text, as whole
// words after norm.Alias, is an entity. Capitalized words that do not start
// a sentence, and all-caps words of two or more letters, are entities too.
func Signature(text string, aliases ...string) Sig {
	var s Sig
	t := norm.Dates(norm.NFC(text))
	s.Dates = norm.ISODate.FindAllString(t, -1)
	t = norm.ISODate.ReplaceAllString(t, " ")
	for _, n := range reNumber.FindAllString(t, -1) {
		s.Numbers = append(s.Numbers, norm.Number(n))
	}

	ents := map[string]bool{}
	sentenceStart := true
	for _, w := range words(t) {
		if w.text == "" { // sentence boundary marker
			sentenceStart = true
			continue
		}
		lw := norm.Alias(trimPossessive(w.text))
		switch {
		case negations[lw]:
			s.Negations = append(s.Negations, lw)
		case strings.HasSuffix(strings.ToLower(w.text), "n't"):
			s.Negations = append(s.Negations, "not")
		}
		first, _ := utf8.DecodeRuneInString(w.text)
		if unicode.IsUpper(first) && utf8.RuneCountInString(w.text) > 1 && !negations[lw] {
			if !sentenceStart || isAllCaps(w.text) {
				ents[lw] = true
			}
		}
		sentenceStart = false
	}
	padded := " " + norm.Alias(t) + " "
	for _, a := range aliases {
		if an := norm.Alias(a); an != "" && strings.Contains(padded, " "+an+" ") {
			ents[an] = true
		}
	}
	for e := range ents {
		if !strings.ContainsFunc(e, unicode.IsLetter) {
			continue
		}
		s.Entities = append(s.Entities, e)
	}
	sort.Strings(s.Entities)
	sort.Strings(s.Negations)
	return s
}

type word struct{ text string }

// words splits t into words (letters, digits, apostrophes) and inserts an
// empty word at each sentence boundary (. ! ? : ; or a line break).
func words(t string) []word {
	var out []word
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, word{strings.Trim(b.String(), "'’")})
			b.Reset()
		}
	}
	for _, r := range t {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '’' || unicode.Is(unicode.Mn, r):
			b.WriteRune(r)
		case strings.ContainsRune(".!?:;\n", r):
			flush()
			out = append(out, word{})
		default:
			flush()
		}
	}
	flush()
	return out
}

func trimPossessive(w string) string {
	for _, suf := range []string{"'s", "’s", "'S", "’S"} {
		if strings.HasSuffix(w, suf) {
			return strings.TrimSuffix(w, suf)
		}
	}
	return w
}

func isAllCaps(w string) bool {
	letters := 0
	for _, r := range w {
		if unicode.IsLetter(r) {
			if !unicode.IsUpper(r) {
				return false
			}
			letters++
		}
	}
	return letters >= 2
}

// Reasons returned by IsNearDuplicate.
const (
	ReasonNearDuplicate   = "near_duplicate"
	ReasonBelowThreshold  = "below_threshold"
	ReasonNumbersDiffer   = "numbers_differ"
	ReasonDatesDiffer     = "dates_differ"
	ReasonEntitiesDiffer  = "entities_differ"
	ReasonNegationDiffers = "negation_differs"
)

// Compare reports whether two signatures are identical and, if not, the
// first component that differs.
func (s Sig) Compare(o Sig) (bool, string) {
	switch {
	case !slices.Equal(s.Numbers, o.Numbers):
		return false, ReasonNumbersDiffer
	case !slices.Equal(s.Dates, o.Dates):
		return false, ReasonDatesDiffer
	case !slices.Equal(s.Entities, o.Entities):
		return false, ReasonEntitiesDiffer
	case !slices.Equal(s.Negations, o.Negations):
		return false, ReasonNegationDiffers
	}
	return true, ReasonNearDuplicate
}

// IsNearDuplicate is true only if cosine > threshold and the signatures of
// a and b are identical after normalization. reason says why not.
func IsNearDuplicate(a, b string, cosine, threshold float64, aliases ...string) (bool, string) {
	if !(cosine > threshold) {
		return false, ReasonBelowThreshold
	}
	return Signature(a, aliases...).Compare(Signature(b, aliases...))
}

// Lexical is the second path of the guard (decision 0138): a newer text
// that repeats every word of an older one and adds at most MaxAddedWords
// words is a near-duplicate when its cosine is above MinCosine (lower than
// the plain threshold) and the signatures are identical. The zero value
// (MinCosine 0) turns the path off.
type Lexical struct {
	MaxAddedWords int
	MinCosine     float64
}

// On reports whether the lexical path is enabled.
func (l Lexical) On() bool { return l.MinCosine > 0 }

// Words returns the words of text as the lexical path compares them:
// lowercase, accents stripped, split at anything that is not a letter or a
// digit (norm.Alias), so letter case, spacing and punctuation do not count.
func Words(text string) []string { return strings.Fields(norm.Alias(text)) }

// Added reports whether newer contains every word of older, counted with
// repetition, and how many words newer has on top of them.
func Added(older, newer string) (int, bool) {
	count := map[string]int{}
	for _, w := range Words(newer) {
		count[w]++
	}
	for _, w := range Words(older) {
		if count[w] == 0 {
			return 0, false
		}
		count[w]--
	}
	added := 0
	for _, n := range count {
		added += n
	}
	return added, true
}

// Extends reports whether newer contains every word of older, counted with
// repetition, and at most maxAdded words more. Nothing older says is then
// missing from newer, which is the record the read path keeps when it
// collapses the two (the most recent one).
func Extends(older, newer string, maxAdded int) bool {
	n, ok := Added(older, newer)
	return ok && n <= maxAdded
}

// IsNearDuplicateOf decides whether newer is a near-duplicate of older: the
// plain rule (cosine > threshold), or the lexical path when it is on
// (cosine > lex.MinCosine and newer extends older by at most
// lex.MaxAddedWords words); either way the signatures must be identical.
func IsNearDuplicateOf(newer, older string, cosine, threshold float64, lex Lexical, aliases ...string) (bool, string) {
	if !(cosine > threshold) && !(lex.On() && cosine > lex.MinCosine && Extends(older, newer, lex.MaxAddedWords)) {
		return false, ReasonBelowThreshold
	}
	return Signature(newer, aliases...).Compare(Signature(older, aliases...))
}

// Guard writes duplicate_of edges.
type Guard struct{ e *env.Env }

// New returns a Guard bound to e.
func New(e *env.Env) *Guard { return &Guard{e: e} }

var tables = map[core.RecordType]string{
	core.RecEvent:    "events",
	core.RecFact:     "facts",
	core.RecPage:     "pages",
	core.RecPageItem: "page_items",
}

func exists(ctx context.Context, tx *sql.Tx, ns string, t core.RecordType, id string) error {
	table, ok := tables[t]
	if !ok {
		return core.Errf(core.CodeInvalidArgument, "record type %q cannot be a near-duplicate", t)
	}
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE namespace = ? AND id = ?`, ns, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Errf(core.CodeNotFound, "%s %s not found in namespace %s", t, id, ns)
	}
	return err
}

// Mark records that the new record is a near-duplicate of an existing,
// older one: a provenance edge new → existing with relation duplicate_of,
// audited as near_duplicate. Nothing is deleted. Marking the same pair
// twice changes nothing and returns audit id 0. Call it only after facts
// reconciliation (see the package documentation).
func (g *Guard) Mark(ctx context.Context, tx *sql.Tx, ns string, newType core.RecordType, newID string, existingType core.RecordType, existingID string) (int64, error) {
	if newType == existingType && newID == existingID {
		return 0, core.Errf(core.CodeInvalidArgument, "a record cannot duplicate itself")
	}
	if err := exists(ctx, tx, ns, newType, newID); err != nil {
		return 0, err
	}
	if err := exists(ctx, tx, ns, existingType, existingID); err != nil {
		return 0, err
	}
	var rev int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM provenance WHERE derived_type = ? AND derived_id = ?
		AND source_type = ? AND source_id = ? AND relation = ?`,
		string(existingType), existingID, string(newType), newID, core.RelDuplicateOf).Scan(&rev); err != nil {
		return 0, err
	}
	if rev > 0 {
		return 0, core.Errf(core.CodeConflict, "%s is already a duplicate of %s", existingID, newID)
	}
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO provenance(namespace, derived_type, derived_id, source_type, source_id, relation, created_at)
		VALUES (?,?,?,?,?,?,?)`, ns, string(newType), newID, string(existingType), existingID, core.RelDuplicateOf,
		core.FormatTime(g.e.Clock.Now()))
	if err != nil {
		return 0, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return 0, err
	}
	return g.e.Audit.Append(ctx, tx, audit.Entry{Namespace: ns, Actor: "system", Action: audit.ActNearDup, Target: newID,
		Details: map[string]any{"duplicate_of": existingID}})
}
