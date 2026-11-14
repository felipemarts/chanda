package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// upstreamPath is the only path the proxy forwards to, relative to
// Config.Upstream.
const upstreamPath = "/chat/completions"

// ReplayHeader is set on responses when a store is configured: "hit" for an
// answer served from the store, "miss" for one forwarded upstream under
// -replay.
const ReplayHeader = "X-Chanda-Eval-Replay"

// storeVersion is the schema version of a stored entry.
const storeVersion = 1

var keyRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// StoreUsage is the upstream usage of a stored call.
type StoreUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CacheHitTokens   int64 `json:"cache_hit_tokens"`
	CacheMissTokens  int64 `json:"cache_miss_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
}

// StoreEntry is one recorded upstream call: DIR/<key[:2]>/<key>.json.gz.
//
// Key is the hex sha256 of Request, the exact body forwarded upstream after
// the proxy's rewrites (temperature 0, thinking, max_tokens, stream
// removed). It equals the request_sha256 of the JSONL log. The body carries
// the model, and the upstream path is the constant upstreamPath; both are
// stored and checked again on lookup. The tag, the client's credentials and
// the time are not part of the key.
type StoreEntry struct {
	Version           int             `json:"version"`
	Key               string          `json:"key"`
	Model             string          `json:"model"`                        // requested model id
	UpstreamPath      string          `json:"upstream_path"`                // "/chat/completions"
	FirstTag          string          `json:"first_tag"`                    // tag of the call that recorded it
	Time              string          `json:"time"`                         // request start, UTC RFC 3339
	PriceWindow       string          `json:"price_window"`                 // peak or offpeak, when recorded
	Usage             StoreUsage      `json:"usage"`                        // as reported by upstream
	CostUSD           float64         `json:"cost_usd"`                     // what the call cost when recorded
	FinishReason      string          `json:"finish_reason"`                // stop or length
	ResponseModel     string          `json:"response_model,omitempty"`     // "model" of the response
	SystemFingerprint string          `json:"system_fingerprint,omitempty"` // of the response, if any
	ContentType       string          `json:"content_type"`                 // upstream Content-Type
	Request           json.RawMessage `json:"request"`                      // forwarded body, verbatim
	Response          string          `json:"response"`                     // upstream body, verbatim
}

// Store is a content-addressed directory of recorded calls. Writes go
// through a temp file and a link (or rename), so concurrent writers and
// readers never see a partial file.
type Store struct {
	dir string
}

// OpenStore creates dir if needed.
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("empty store directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// StoreKey is the store key of a forwarded body (see StoreEntry).
func StoreKey(forwarded []byte) string {
	sum := sha256.Sum256(forwarded)
	return hex.EncodeToString(sum[:])
}

func (s *Store) path(key string) string {
	return filepath.Join(s.dir, key[:2], key+".json.gz")
}

// Get returns the entry for key. It returns an error wrapping
// fs.ErrNotExist when there is none, and another error when the file exists
// but is unreadable or does not match the key.
func (s *Store) Get(key string) (*StoreEntry, error) {
	if !keyRE.MatchString(key) {
		return nil, fmt.Errorf("bad store key %q", key)
	}
	e, err := readEntry(s.path(key))
	if err != nil {
		return nil, err
	}
	if e.Key != key || StoreKey(e.Request) != key {
		return nil, fmt.Errorf("store entry %s does not match its key", key)
	}
	return e, nil
}

func readEntry(path string) (*StoreEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var e StoreEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &e, nil
}

// Put writes e unless a valid entry for its key exists already, so the
// first tag seen is kept. It reports whether it wrote a new file.
func (s *Store) Put(e *StoreEntry) (bool, error) {
	if !keyRE.MatchString(e.Key) || StoreKey(e.Request) != e.Key {
		return false, fmt.Errorf("store entry key %q does not match its request", e.Key)
	}
	_, gerr := s.Get(e.Key)
	if gerr == nil {
		return false, nil
	}
	replace := !errors.Is(gerr, fs.ErrNotExist) // unreadable: overwrite it
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	enc := json.NewEncoder(zw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return false, err
	}
	if err := zw.Close(); err != nil {
		return false, err
	}
	final := s.path(e.Key)
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*.json.gz")
	if err != nil {
		return false, err
	}
	_, werr := tmp.Write(buf.Bytes())
	serr := tmp.Sync()
	cerr := tmp.Close()
	defer os.Remove(tmp.Name()) // no-op after a rename
	if err := errors.Join(werr, serr, cerr); err != nil {
		return false, err
	}
	if !replace {
		// A hard link fails if another writer got there first: the first
		// entry wins and stays untouched.
		err := os.Link(tmp.Name(), final)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		// Filesystems without hard links: fall back to rename.
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return false, err
	}
	return true, nil
}

// storable reports whether a 200 response may be recorded: a non-empty
// message content, finish_reason stop or length, and no refusal. It also
// returns the response's model and system_fingerprint.
func storable(body []byte) (model, fingerprint string, ok bool) {
	if !utf8.Valid(body) {
		return "", "", false
	}
	var r struct {
		Model             string `json:"model"`
		SystemFingerprint string `json:"system_fingerprint"`
		Choices           []struct {
			FinishReason string `json:"finish_reason"`
			Message      *struct {
				Content *string `json:"content"`
				Refusal *string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &r); err != nil || len(r.Choices) != 1 {
		return "", "", false
	}
	c := r.Choices[0]
	if c.FinishReason != "stop" && c.FinishReason != "length" {
		return "", "", false
	}
	if c.Message == nil || c.Message.Content == nil || strings.TrimSpace(*c.Message.Content) == "" {
		return "", "", false
	}
	if c.Message.Refusal != nil && strings.TrimSpace(*c.Message.Refusal) != "" {
		return "", "", false
	}
	return r.Model, r.SystemFingerprint, true
}

// StoreStats summarises a store directory.
type StoreStats struct {
	Dir        string              `json:"dir"`
	Entries    int64               `json:"entries"`
	Bytes      int64               `json:"bytes"` // compressed, on disk
	CostUSD    float64             `json:"cost_usd"`
	Unreadable int64               `json:"unreadable"`
	ByModel    map[string]*StatRow `json:"by_model"`
	ByFirstTag map[string]*StatRow `json:"by_first_tag"`
}

// StatRow is one group of StoreStats.
type StatRow struct {
	Entries int64   `json:"entries"`
	Bytes   int64   `json:"bytes"`
	CostUSD float64 `json:"cost_usd"`
}

// Stats walks the store and decodes every entry.
func (s *Store) Stats() (*StoreStats, error) {
	st := &StoreStats{Dir: s.dir, ByModel: map[string]*StatRow{}, ByFirstTag: map[string]*StatRow{}}
	add := func(m map[string]*StatRow, k string, size int64, cost float64) {
		r := m[k]
		if r == nil {
			r = &StatRow{}
			m[k] = r
		}
		r.Entries++
		r.Bytes += size
		r.CostUSD += cost
	}
	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".json.gz") || strings.HasPrefix(name, ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e, err := readEntry(path)
		if err != nil || e.Key+".json.gz" != name {
			st.Unreadable++
			return nil
		}
		st.Entries++
		st.Bytes += info.Size()
		st.CostUSD += e.CostUSD
		add(st.ByModel, e.Model, info.Size(), e.CostUSD)
		add(st.ByFirstTag, e.FirstTag, info.Size(), e.CostUSD)
		return nil
	})
	return st, err
}

// WriteText prints the stats as a small table.
func (st *StoreStats) WriteText(w io.Writer) {
	fmt.Fprintf(w, "store %s: %d entries, %d bytes, recorded cost %.6f USD", st.Dir, st.Entries, st.Bytes, st.CostUSD)
	if st.Unreadable > 0 {
		fmt.Fprintf(w, ", %d unreadable files", st.Unreadable)
	}
	fmt.Fprintln(w)
	for _, g := range []struct {
		name string
		m    map[string]*StatRow
	}{{"model", st.ByModel}, {"first tag", st.ByFirstTag}} {
		keys := make([]string, 0, len(g.m))
		for k := range g.m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(w, "by %s:\n", g.name)
		for _, k := range keys {
			r := g.m[k]
			fmt.Fprintf(w, "  %-24s %8d entries %12d bytes %12.6f USD\n", k, r.Entries, r.Bytes, r.CostUSD)
		}
	}
}
