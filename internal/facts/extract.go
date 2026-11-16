package facts

import (
	"context"
	"database/sql"
	"path"
	"regexp"
	"strings"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/facts/norm"
)

// Extractor kinds (cfg.Write.Extractors[].Kind).
const (
	KindHeader  = "header"   // front-matter or "Key: value" lines at the top of a chunk
	KindKVTable = "kv_table" // two-column Markdown table: key | value
	KindRegex   = "regex"    // named groups subject, value and optionally predicate
)

// Event is the minimal view of a raw event the extractors read. The
// integration adapts the write path's event to it. TrustZone is informative:
// Assert reads the authoritative zone from the events table.
type Event struct {
	ID          string
	Namespace   string
	Content     string
	TrustZone   core.Zone
	RecordedAt  string
	DocDate     string // document date, if the event is a document chunk
	ValidFrom   string // date given in the call, if any
	HeadingPath string // "H1 > H2" for document chunks
	DocURI      string // document uri, if the event is a document chunk (its file name is the last-resort subject)
}

// docName is the file name of a document uri without its extension
// ("kb/notes/project-atlas.md" -> "project-atlas"), or "".
func docName(uri string) string {
	uri = strings.TrimRight(strings.TrimSpace(uri), "/")
	if uri == "" {
		return ""
	}
	base := path.Base(uri)
	if ext := path.Ext(base); ext != base {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

// Candidate is one fact found by a deterministic extractor.
type Candidate struct {
	Extractor string
	Subject   string
	Predicate string
	Value     string
}

type extractor struct {
	name string
	cfg  config.ExtractorConfig
	re   *regexp.Regexp
	only string // normalized predicate filter for header and kv_table
}

// subjectKeys are header keys that name the subject instead of being facts.
var subjectKeys = map[string]bool{"title": true, "name": true, "subject": true, "titulo": true, "nome": true, "assunto": true}

// ValidateExtractors reports the first invalid extractor configuration.
func ValidateExtractors(cfgs []config.ExtractorConfig) error {
	_, err := compileExtractors(cfgs)
	return err
}

func compileExtractors(cfgs []config.ExtractorConfig) ([]extractor, error) {
	var out []extractor
	for i, c := range cfgs {
		x := extractor{name: c.Name, cfg: c, only: norm.Predicate(c.Predicate)}
		if x.name == "" {
			x.name = c.Kind
		}
		switch c.Kind {
		case KindHeader, KindKVTable:
		case KindRegex:
			re, err := regexp.Compile(c.Pattern)
			if err != nil {
				return nil, core.Wrap(core.CodeInvalidArgument, err, "extractor "+x.name+": bad pattern")
			}
			groups := map[string]bool{}
			for _, n := range re.SubexpNames() {
				groups[n] = true
			}
			if !groups["value"] || (!groups["subject"] && strings.TrimSpace(c.Subject) == "") ||
				(!groups["predicate"] && x.only == "") {
				return nil, core.Errf(core.CodeInvalidArgument,
					"extractor %s: pattern needs group value, group subject or a subject, group predicate or a predicate", x.name)
			}
			x.re = re
		default:
			return nil, core.Errf(core.CodeInvalidArgument, "extractor %d: unknown kind %q", i, c.Kind)
		}
		out = append(out, x)
	}
	return out, nil
}

var (
	reHeaderLine = regexp.MustCompile(`^\s*(\p{L}[\p{L}\p{N} _\-]{0,40}?)\s*:\s+(\S.*?)\s*$`)
	reHeading    = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$`)
	reTableSep   = regexp.MustCompile(`^:?-{1,}:?$`)
)

func lines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func headingText(line string) (string, bool) {
	m := reHeading.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func lastHeading(path string) string {
	parts := strings.Split(path, ">")
	return strings.TrimSpace(parts[len(parts)-1])
}

// Extract runs the configured extractors over ev and returns the candidates
// in configuration order, then order of appearance, without repeats. It
// touches no database.
func (s *Service) Extract(ev Event) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	add := func(x extractor, subj, pred, val string) {
		subj, val = strings.TrimSpace(subj), strings.Trim(strings.TrimSpace(val), `"'`)
		p := norm.Predicate(pred)
		if norm.Alias(subj) == "" || p == "" || norm.Value(val) == "" {
			return
		}
		if x.cfg.Kind != KindRegex && x.only != "" && p != x.only {
			return
		}
		k := norm.Alias(subj) + "\x00" + p + "\x00" + norm.Value(val)
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, Candidate{Extractor: x.name, Subject: subj, Predicate: p, Value: val})
	}
	for _, x := range s.exts {
		switch x.cfg.Kind {
		case KindHeader:
			extractHeader(x, ev, add)
		case KindKVTable:
			extractTables(x, ev, add)
		case KindRegex:
			for _, m := range x.re.FindAllStringSubmatch(ev.Content, -1) {
				subj, pred, val := x.cfg.Subject, "", ""
				for i, n := range x.re.SubexpNames() {
					switch n {
					case "subject":
						if m[i] != "" {
							subj = m[i]
						}
					case "predicate":
						pred = m[i]
					case "value":
						val = m[i]
					}
				}
				if pred == "" {
					pred = x.cfg.Predicate
				}
				add(x, subj, pred, val)
			}
		}
	}
	return out
}

type addFunc func(x extractor, subj, pred, val string)

// extractHeader reads a front-matter block (between "---" lines) or the
// "Key: value" lines at the top of the chunk, after any leading headings.
// Subject: the configured one, else a title/name/subject key, else the
// nearest heading, else the last element of HeadingPath, else the
// document file name without extension.
func extractHeader(x extractor, ev Event, add addFunc) {
	ls := lines(ev.Content)
	i := 0
	for i < len(ls) && strings.TrimSpace(ls[i]) == "" {
		i++
	}
	heading := ""
	var pairs [][2]string
	if i < len(ls) && strings.TrimSpace(ls[i]) == "---" {
		end := -1
		for j := i + 1; j < len(ls); j++ {
			if strings.TrimSpace(ls[j]) == "---" {
				end = j
				break
			}
		}
		if end < 0 {
			return
		}
		for _, l := range ls[i+1 : end] {
			if m := reHeaderLine.FindStringSubmatch(l); m != nil && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
				pairs = append(pairs, [2]string{m[1], m[2]})
			}
		}
	} else {
		for ; i < len(ls); i++ {
			if h, ok := headingText(ls[i]); ok {
				heading = h
				continue
			}
			if strings.TrimSpace(ls[i]) != "" {
				break
			}
		}
		for ; i < len(ls); i++ {
			m := reHeaderLine.FindStringSubmatch(ls[i])
			if m == nil {
				break
			}
			pairs = append(pairs, [2]string{m[1], m[2]})
		}
	}
	subj := strings.TrimSpace(x.cfg.Subject)
	subjKey := ""
	if subj == "" {
		for _, p := range pairs {
			if k := norm.Predicate(p[0]); subjectKeys[k] {
				subj, subjKey = p[1], k
				break
			}
		}
	}
	if subj == "" {
		subj = heading
	}
	if subj == "" {
		subj = lastHeading(ev.HeadingPath)
	}
	if subj == "" {
		subj = docName(ev.DocURI)
	}
	if subj == "" {
		return
	}
	for _, p := range pairs {
		if subjKey != "" && norm.Predicate(p[0]) == subjKey {
			continue
		}
		add(x, subj, p[0], p[1])
	}
}

func tableCells(line string) []string {
	l := strings.TrimSpace(line)
	if !strings.HasPrefix(l, "|") {
		return nil
	}
	l = strings.TrimSuffix(strings.TrimPrefix(l, "|"), "|")
	cells := strings.Split(l, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// extractTables reads two-column Markdown tables (header row, separator row,
// then key | value rows). Subject: the configured one, else the nearest
// heading above the table, else the last element of HeadingPath, else the
// document file name without extension.
func extractTables(x extractor, ev Event, add addFunc) {
	ls := lines(ev.Content)
	heading := ""
	for i := 0; i < len(ls); i++ {
		if h, ok := headingText(ls[i]); ok {
			heading = h
			continue
		}
		head := tableCells(ls[i])
		if head == nil || i+1 >= len(ls) {
			continue
		}
		sep := tableCells(ls[i+1])
		if len(head) != 2 || len(sep) != 2 || !reTableSep.MatchString(sep[0]) || !reTableSep.MatchString(sep[1]) {
			continue
		}
		subj := strings.TrimSpace(x.cfg.Subject)
		if subj == "" {
			subj = heading
		}
		if subj == "" {
			subj = lastHeading(ev.HeadingPath)
		}
		if subj == "" {
			subj = docName(ev.DocURI)
		}
		j := i + 2
		for ; j < len(ls); j++ {
			row := tableCells(ls[j])
			if row == nil {
				break
			}
			if len(row) == 2 && subj != "" {
				add(x, subj, row[0], row[1])
			}
		}
		i = j - 1
	}
}

// AfterEvent is the write-path hook: it runs the extractors over ev and
// asserts each candidate with ev as its only origin, inside tx. It returns
// one result per candidate, in order.
func (s *Service) AfterEvent(ctx context.Context, tx *sql.Tx, ev Event) ([]AssertResult, error) {
	if s.extErr != nil {
		return nil, s.extErr
	}
	var out []AssertResult
	for _, c := range s.Extract(ev) {
		res, err := s.Assert(ctx, tx, AssertInput{
			Namespace: ev.Namespace, Subject: c.Subject, Predicate: c.Predicate, Value: c.Value,
			ValidFrom: ev.ValidFrom, DocDate: ev.DocDate, OriginEventIDs: []string{ev.ID}, Actor: "extractor:" + c.Extractor,
		})
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}
