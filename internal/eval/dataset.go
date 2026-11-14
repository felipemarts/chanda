// Package eval is Chanda's evaluation environment (BASE.md "Evaluation"):
// dataset format, baselines B0-B3, LLM-free retrieval metrics, the own-suite
// registry, the gate-set score and the reproducible report.
//
// Every configuration plugs in through the System interface, so the real
// read path (C1..C5) is compared against the baselines at the same budget.
package eval

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/felipemarts/chanda-mcp/internal/core"
)

// Dataset file names inside a dataset directory.
const (
	CorpusFile    = "corpus.jsonl"
	QuestionsFile = "questions.jsonl"
	ManifestFile  = "dataset.json"
)

// Item types in the corpus.
const (
	ItemDocument = "document"
	ItemSession  = "session"
)

// Well-known question tags. Others are allowed if they match tagPattern.
const (
	TagAbstention = "abstention" // the corpus has no answer; the right return is empty
	TagTemporal   = "temporal"   // the answer depends on as_of
	TagUpdate     = "update"     // the answer changed in a later document version or statement
	TagInjection  = "injection"  // an external item tries to act as an instruction
	TagDedup      = "dedup"      // the answer appears in near-duplicate paraphrases
	TagSession    = "session"    // the answer only exists in a session
)

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

var validSource = map[core.SourceKind]bool{
	core.SourceDocument: true, core.SourceSession: true, core.SourceTool: true,
	core.SourceWeb: true, core.SourceUserStatement: true,
}

// Item is one corpus record, ingested in file order: a document version or a
// whole session. Versions of a document share ID and URI and appear in
// increasing Version order.
type Item struct {
	Type    string `json:"type"`              // document | session
	ID      string `json:"id"`                // stable across versions of a document
	URI     string `json:"uri"`               // stable across versions of a document
	Version int    `json:"version,omitempty"` // documents: 1, 2, ...
	Date    string `json:"date"`              // YYYY-MM-DD or RFC 3339
	Source  string `json:"source"`            // core.SourceKind: document, session, tool, web, user_statement
	Title   string `json:"title,omitempty"`
	Text    string `json:"text,omitempty"`  // documents
	Turns   []Turn `json:"turns,omitempty"` // sessions
}

// Turn is one session turn.
type Turn struct {
	Role string `json:"role"` // user | assistant | tool
	Text string `json:"text"`
}

// Gold is one piece of evidence: an item (DocID) and, optionally, a span that
// a retrieved excerpt must contain. Entries sharing Unit are alternatives for
// the same relevance unit (e.g. a paraphrase); an empty Unit is its own unit.
type Gold struct {
	DocID string `json:"doc_id"`
	Span  string `json:"span,omitempty"`
	Unit  string `json:"unit,omitempty"`
}

// Question is one evaluation query with its answer key.
type Question struct {
	ID             string   `json:"id"`
	Question       string   `json:"question"`
	AsOf           string   `json:"as_of,omitempty"`
	Gold           []Gold   `json:"gold,omitempty"`
	Answers        []string `json:"answers,omitempty"`          // any of these substrings in the context answers the question
	MustNotContain []string `json:"must_not_contain,omitempty"` // stale values, injected instructions
	Tags           []string `json:"tags,omitempty"`
}

// Answerable reports whether the corpus holds an answer.
func (q Question) Answerable() bool { return len(q.Gold) > 0 }

// HasTag reports whether q carries tag.
func (q Question) HasTag(tag string) bool {
	for _, t := range q.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Manifest describes a dataset.
type Manifest struct {
	Name        string `json:"name"`
	Language    string `json:"language,omitempty"`
	Description string `json:"description,omitempty"`
	Generator   string `json:"generator,omitempty"`
	Seed        string `json:"seed,omitempty"`
}

// Dataset is a corpus plus questions.
type Dataset struct {
	Manifest  Manifest
	Corpus    []Item
	Questions []Question
}

// LoadDataset reads and validates a dataset directory.
func LoadDataset(dir string) (*Dataset, error) {
	ds := &Dataset{Manifest: Manifest{Name: filepath.Base(dir)}}
	if b, err := os.ReadFile(filepath.Join(dir, ManifestFile)); err == nil {
		if err := json.Unmarshal(b, &ds.Manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", ManifestFile, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, CorpusFile))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if ds.Corpus, err = ReadJSONL[Item](f); err != nil {
		return nil, fmt.Errorf("%s: %w", CorpusFile, err)
	}
	g, err := os.Open(filepath.Join(dir, QuestionsFile))
	if err != nil {
		return nil, err
	}
	defer g.Close()
	if ds.Questions, err = ReadJSONL[Question](g); err != nil {
		return nil, fmt.Errorf("%s: %w", QuestionsFile, err)
	}
	if err := ds.Validate(); err != nil {
		return nil, err
	}
	return ds, nil
}

// ReadJSONL decodes one JSON object per non-empty line, rejecting unknown fields.
func ReadJSONL[T any](r io.Reader) ([]T, error) {
	var out []T
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		var v T
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, v)
	}
	return out, sc.Err()
}

// WriteDataset writes ds into dir in canonical form (one JSON object per line).
func WriteDataset(dir string, ds *Dataset) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m, err := json.MarshalIndent(ds.Manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), append(m, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, CorpusFile), jsonl(ds.Corpus), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, QuestionsFile), jsonl(ds.Questions), 0o644)
}

func jsonl[T any](vs []T) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, v := range vs {
		_ = enc.Encode(v) // plain structs of strings: cannot fail
	}
	return b.Bytes()
}

// Hash identifies the dataset content: SHA-256 over the canonical encoding of
// corpus and questions (the manifest is descriptive and excluded).
func (ds *Dataset) Hash() string {
	h := sha256.New()
	h.Write(jsonl(ds.Corpus))
	h.Write([]byte{0})
	h.Write(jsonl(ds.Questions))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ItemTime parses an item or question date.
func ItemTime(s string) (time.Time, error) { return core.ParseTime(s) }

// Validate checks the dataset invariants: unique ids, document versions in
// order with a stable URI, dates in non-decreasing ingestion order, gold
// evidence present in the corpus no later than as_of, and abstention
// questions without gold.
func (ds *Dataset) Validate() error {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	type docState struct {
		uri     string
		version int
		texts   []string // normalized text per version
		dates   []time.Time
	}
	docs := map[string]*docState{}
	uris := map[string]string{}
	var last time.Time
	for i, it := range ds.Corpus {
		where := fmt.Sprintf("corpus[%d] %q", i, it.ID)
		if it.ID == "" || it.URI == "" {
			bad("%s: id and uri are required", where)
			continue
		}
		t, err := ItemTime(it.Date)
		if err != nil {
			bad("%s: bad date %q", where, it.Date)
			continue
		}
		if t.Before(last) {
			bad("%s: date %s before previous item (corpus is in ingestion order)", where, it.Date)
		}
		last = t
		if !validSource[core.SourceKind(it.Source)] {
			bad("%s: unknown source %q", where, it.Source)
		}
		if other, ok := uris[it.URI]; ok && other != it.ID {
			bad("%s: uri %q already used by %q", where, it.URI, other)
		}
		uris[it.URI] = it.ID
		var text string
		switch it.Type {
		case ItemDocument:
			if strings.TrimSpace(it.Text) == "" || len(it.Turns) > 0 {
				bad("%s: a document has text and no turns", where)
			}
			text = it.Text
		case ItemSession:
			if it.Text != "" || len(it.Turns) == 0 {
				bad("%s: a session has turns and no text", where)
			}
			if it.Version > 1 {
				bad("%s: sessions are not versioned", where)
			}
			for _, tu := range it.Turns {
				if strings.TrimSpace(tu.Text) == "" || tu.Role == "" {
					bad("%s: empty turn", where)
				}
				text += tu.Text + "\n"
			}
		default:
			bad("%s: unknown type %q", where, it.Type)
		}
		st := docs[it.ID]
		if st == nil {
			st = &docState{uri: it.URI}
			docs[it.ID] = st
		}
		want := st.version + 1
		v := max(it.Version, 1)
		if v != want {
			bad("%s: version %d, want %d", where, v, want)
		}
		st.version = v
		st.texts = append(st.texts, Normalize(text))
		st.dates = append(st.dates, t)
	}

	qids := map[string]bool{}
	for i, q := range ds.Questions {
		where := fmt.Sprintf("questions[%d] %q", i, q.ID)
		if q.ID == "" || strings.TrimSpace(q.Question) == "" {
			bad("%s: id and question are required", where)
			continue
		}
		if qids[q.ID] {
			bad("%s: duplicate id", where)
		}
		qids[q.ID] = true
		asOf := last
		if q.AsOf != "" {
			t, err := ItemTime(q.AsOf)
			if err != nil {
				bad("%s: bad as_of %q", where, q.AsOf)
			} else {
				asOf = t
			}
		}
		for _, tag := range q.Tags {
			if !tagPattern.MatchString(tag) {
				bad("%s: bad tag %q", where, tag)
			}
		}
		if q.HasTag(TagAbstention) != !q.Answerable() {
			bad("%s: abstention questions, and only they, have no gold", where)
		}
		if !q.Answerable() && len(q.Answers) > 0 {
			bad("%s: answers without gold", where)
		}
		var goldText []string
		for _, g := range q.Gold {
			st := docs[g.DocID]
			if st == nil {
				bad("%s: gold doc %q not in corpus", where, g.DocID)
				continue
			}
			found := false
			for vi, txt := range st.texts {
				if st.dates[vi].After(asOf) {
					continue
				}
				if g.Span == "" || strings.Contains(txt, Normalize(g.Span)) {
					found = true
					goldText = append(goldText, txt)
				}
			}
			if !found {
				bad("%s: gold span %q not in %q on or before as_of", where, g.Span, g.DocID)
			}
		}
		for _, a := range q.Answers {
			na, ok := Normalize(a), false
			for _, txt := range goldText {
				ok = ok || strings.Contains(txt, na)
			}
			if !ok {
				bad("%s: answer %q not found in any gold item", where, a)
			}
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		if len(errs) > 20 {
			errs = append(errs[:20], fmt.Sprintf("... and %d more", len(errs)-20))
		}
		return core.Errf(core.CodeInvalidArgument, "invalid dataset %q:\n  %s", ds.Manifest.Name, strings.Join(errs, "\n  "))
	}
	return nil
}

// ItemText returns the full text of an item (session turns joined by newlines).
func ItemText(it Item) string {
	if it.Type != ItemSession {
		return it.Text
	}
	parts := make([]string, len(it.Turns))
	for i, t := range it.Turns {
		parts[i] = t.Text
	}
	return strings.Join(parts, "\n")
}
