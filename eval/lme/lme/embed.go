package lme

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"

	_ "modernc.org/sqlite"

	"github.com/felipemarts/chanda-mcp/embedders/hugot"
	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/embed"
	"github.com/felipemarts/chanda-mcp/pkg/chanda/embedreg"
)

// HashDim is the dimension of Chanda's default hash embedder (model id
// hash-v1-256); the battery uses the default so the model id is Chanda's.
const HashDim = 256

// Roles of an embedded text. e5 prefixes queries with "query: " and stored
// text with "passage: "; the hash embedder ignores the role.
const (
	RolePassage = "passage"
	RoleQuery   = "query"
)

// EmbedOptions selects the embedder implementation.
type EmbedOptions struct {
	Kind  string // "hash" or "e5"
	E5Dir string // e5-small model directory (chanda-hugot models pull e5-small DIR)
	URL   string // base URL of a running "lme embedserve"; used instead of loading e5 in process
	Cache string // embedding cache file; "" disables the cache
}

// roleAdapter gives an embedder without roles an EmbedQuery equal to Embed,
// which is what Chanda (internal/read/vector.go) and internal/eval do when
// the embedder is not a core.RoleEmbedder.
type roleAdapter struct{ core.Embedder }

func (r roleAdapter) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return r.Embed(ctx, texts)
}

// OpenInner opens the real embedder (no cache).
func OpenInner(ctx context.Context, o EmbedOptions) (core.RoleEmbedder, error) {
	if o.URL != "" {
		r, err := OpenRemote(ctx, o.URL)
		if err != nil {
			return nil, err
		}
		if o.Kind == "hash" && r.ModelID() != embed.HashModelID(HashDim) || o.Kind == "e5" && !isE5(r.ModelID()) {
			return nil, fmt.Errorf("embed server %s serves %s, not %s", o.URL, r.ModelID(), o.Kind)
		}
		return r, nil
	}
	switch o.Kind {
	case "hash":
		h, err := embed.NewHash(HashDim)
		if err != nil {
			return nil, err
		}
		return roleAdapter{h}, nil
	case "e5":
		if o.E5Dir == "" {
			return nil, fmt.Errorf("e5: no model directory (-e5-dir, or $CHANDA_HUGOT_MODELS/e5-small)")
		}
		return hugot.Open(ctx, o.E5Dir)
	}
	return nil, fmt.Errorf("unknown embedder %q (want hash or e5)", o.Kind)
}

func isE5(id string) bool {
	m, _ := hugot.LookupModel("e5-small")
	return len(id) >= len(m.Name) && id[:len(m.Name)] == m.Name
}

// DefaultE5Dir is $CHANDA_HUGOT_MODELS/e5-small when that variable is set.
func DefaultE5Dir() string {
	if d := os.Getenv("CHANDA_HUGOT_MODELS"); d != "" {
		return filepath.Join(d, "e5-small")
	}
	return ""
}

// OpenEmbedder opens the configured embedder, wrapped by the cache when
// o.Cache is set.
func OpenEmbedder(ctx context.Context, o EmbedOptions) (*Cached, error) {
	inner, err := OpenInner(ctx, o)
	if err != nil {
		return nil, err
	}
	var c *Cache
	if o.Cache != "" {
		if c, err = OpenCache(o.Cache); err != nil {
			return nil, err
		}
	}
	return &Cached{inner: inner, cache: c}, nil
}

// ---------------------------------------------------------------------------
// Cache.

// Cache stores vectors keyed by (model_id, role, sha256(text)) in SQLite, so
// LongMemEval sessions that repeat across questions are embedded once. The
// role is part of the key because e5 embeds the same text differently as a
// query and as a passage. Several processes may share the file (WAL).
type Cache struct{ db *sql.DB }

// OpenCache opens (creating) the cache file.
func OpenCache(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(60000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS emb (
		model TEXT NOT NULL, role TEXT NOT NULL, sha TEXT NOT NULL, dim INTEGER NOT NULL, vec BLOB NOT NULL,
		PRIMARY KEY (model, role, sha)) WITHOUT ROWID`); err != nil {
		db.Close()
		return nil, fmt.Errorf("embedding cache %s: %w", path, err)
	}
	return &Cache{db: db}, nil
}

// Close closes the cache.
func (c *Cache) Close() error { return c.db.Close() }

// Get returns the cached vectors (nil where missing).
func (c *Cache) Get(ctx context.Context, model, role string, shas []string) ([][]float32, error) {
	out := make([][]float32, len(shas))
	st, err := c.db.PrepareContext(ctx, `SELECT dim, vec FROM emb WHERE model = ? AND role = ? AND sha = ?`)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	for i, h := range shas {
		var dim int
		var b []byte
		switch err := st.QueryRowContext(ctx, model, role, h).Scan(&dim, &b); err {
		case nil:
			if len(b) != 4*dim {
				return nil, fmt.Errorf("embedding cache: corrupt vector %s/%s/%s", model, role, h)
			}
			out[i] = DecodeVec(b)
		case sql.ErrNoRows:
		default:
			return nil, err
		}
	}
	return out, nil
}

// Put stores vectors (existing keys are kept: vectors are deterministic).
func (c *Cache) Put(ctx context.Context, model, role string, shas []string, vecs [][]float32) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for i, h := range shas {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO emb(model, role, sha, dim, vec) VALUES (?, ?, ?, ?, ?)`,
			model, role, h, len(vecs[i]), EncodeVec(vecs[i])); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// EncodeVec is little-endian float32 bytes (also the OpenAI base64 payload).
func EncodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// DecodeVec reverses EncodeVec.
func DecodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

// ---------------------------------------------------------------------------
// Cached embedder.

// Cached wraps an embedder with the cache. Its ModelID and Dim are the
// wrapped embedder's, so Chanda stores exactly the same model id as without
// the cache.
type Cached struct {
	inner        core.RoleEmbedder
	cache        *Cache
	hits, misses atomic.Int64
}

var _ core.RoleEmbedder = (*Cached)(nil)

func (c *Cached) ModelID() string { return c.inner.ModelID() }
func (c *Cached) Dim() int        { return c.inner.Dim() }

// Embed embeds stored text (e5: "passage: ").
func (c *Cached) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return c.get(ctx, RolePassage, texts, c.inner.Embed)
}

// EmbedQuery embeds queries (e5: "query: ").
func (c *Cached) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return c.get(ctx, RoleQuery, texts, c.inner.EmbedQuery)
}

// EmbedRole dispatches on role.
func (c *Cached) EmbedRole(ctx context.Context, role string, texts []string) ([][]float32, error) {
	if role == RoleQuery {
		return c.EmbedQuery(ctx, texts)
	}
	return c.Embed(ctx, texts)
}

// Stats returns cache hits and misses (texts).
func (c *Cached) Stats() (hits, misses int64) { return c.hits.Load(), c.misses.Load() }

// Close closes the cache and the inner embedder when it has a Close.
func (c *Cached) Close() error {
	if cl, ok := c.inner.(interface{ Close() error }); ok {
		_ = cl.Close()
	}
	if c.cache != nil {
		return c.cache.Close()
	}
	return nil
}

func (c *Cached) get(ctx context.Context, role string, texts []string, fn func(context.Context, []string) ([][]float32, error)) ([][]float32, error) {
	if c.cache == nil {
		c.misses.Add(int64(len(texts)))
		return fn(ctx, texts)
	}
	shas := make([]string, len(texts))
	for i, t := range texts {
		shas[i] = SHA256(t)
	}
	model := c.inner.ModelID()
	out, err := c.cache.Get(ctx, model, role, shas)
	if err != nil {
		return nil, err
	}
	var missT, missH []string
	var missI []int
	for i, v := range out {
		if v == nil {
			missT, missH, missI = append(missT, texts[i]), append(missH, shas[i]), append(missI, i)
		}
	}
	c.hits.Add(int64(len(texts) - len(missT)))
	c.misses.Add(int64(len(missT)))
	if len(missT) == 0 {
		return out, nil
	}
	vs, err := fn(ctx, missT)
	if err != nil {
		return nil, err
	}
	if len(vs) != len(missT) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d texts", len(vs), len(missT))
	}
	if err := c.cache.Put(ctx, model, role, missH, vs); err != nil {
		return nil, err
	}
	for j, i := range missI {
		out[i] = vs[j]
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Chanda provider: "lme-cached", for the chanda server this binary runs.

// ProviderName is the embed.provider value of the caching provider.
const ProviderName = "lme-cached"

// Environment variables read by the provider (set by the harness when it
// starts "lme chanda serve").
const (
	EnvInner = "LME_EMBED_INNER" // hash | e5
	EnvE5Dir = "LME_E5_DIR"
	EnvURL   = "LME_EMBED_URL"
	EnvCache = "LME_EMBED_CACHE"
)

// Env returns the provider environment for o.
func (o EmbedOptions) Env() []string {
	return []string{EnvInner + "=" + o.Kind, EnvE5Dir + "=" + o.E5Dir, EnvURL + "=" + o.URL, EnvCache + "=" + o.Cache}
}

// RegisterProvider registers "lme-cached" in pkg/chanda/embedreg: the real
// embedder named by the environment, behind the cache. A configured model_id
// or dim must equal the wrapped embedder's.
func RegisterProvider() {
	embedreg.Register(ProviderName, func(cfg embedreg.Config) (embedreg.Embedder, error) {
		o := EmbedOptions{Kind: os.Getenv(EnvInner), E5Dir: os.Getenv(EnvE5Dir), URL: os.Getenv(EnvURL), Cache: os.Getenv(EnvCache)}
		if o.E5Dir == "" {
			o.E5Dir = cfg.ModelPath
		}
		e, err := OpenEmbedder(context.Background(), o)
		if err != nil {
			return nil, core.Wrap(core.CodeUnavailable, err, ProviderName)
		}
		if cfg.ModelID != "" && cfg.ModelID != e.ModelID() {
			return nil, core.Errf(core.CodeInvalidArgument, "%s: embed.model_id %q but the wrapped embedder is %q", ProviderName, cfg.ModelID, e.ModelID())
		}
		if cfg.Dim != 0 && cfg.Dim != e.Dim() {
			return nil, core.Errf(core.CodeInvalidArgument, "%s: embed.dim %d but the wrapped embedder has %d", ProviderName, cfg.Dim, e.Dim())
		}
		return e, nil
	})
}
