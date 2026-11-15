// Package config loads chanda.yaml. Every threshold and default cited in
// BASE.md lives here; code must read values from Config, never hard-code them.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DBPath    string `yaml:"db_path"`
	Namespace string `yaml:"namespace"` // default namespace when a call omits it
	IDSeed    string `yaml:"id_seed"`   // entropy seed for deterministic ids

	Write   WriteConfig   `yaml:"write"`
	Read    ReadConfig    `yaml:"read"`
	Embed   EmbedConfig   `yaml:"embed"`
	Facts   FactsConfig   `yaml:"facts"`
	Sleep   SleepConfig   `yaml:"sleep"`
	Ranking RankingConfig `yaml:"ranking"`
	LLM     LLMConfig     `yaml:"llm"`
	MCP     MCPConfig     `yaml:"mcp"`
	Forget  ForgetConfig  `yaml:"forget"`
	Store   StoreConfig   `yaml:"store"`
}

// StoreConfig tunes the SQLite connection (decision 0100). Neither value
// changes the file's content, only how much of it is kept in memory.
type StoreConfig struct {
	CacheMB int `yaml:"cache_mb"` // page cache of the connection (PRAGMA cache_size); 0 keeps SQLite's 2 MB
	MmapMB  int `yaml:"mmap_mb"`  // memory-mapped reads (PRAGMA mmap_size); 0 = off
}

type WriteConfig struct {
	MinChars            int               `yaml:"min_chars"`            // admission: too short below this
	MaxChars            int               `yaml:"max_chars"`            // admission: over the size limit
	MaxChunkChars       int               `yaml:"max_chunk_chars"`      // structural chunker splits long paragraphs above this
	SecretPatterns      []string          `yaml:"secret_patterns"`      // extra regexes masked on top of the built-ins
	InstructionPatterns []string          `yaml:"instruction_patterns"` // extra regexes for instruction_like
	NearDupThreshold    float64           `yaml:"near_dup_threshold"`   // cosine, default 0.95
	Extractors          []ExtractorConfig `yaml:"extractors"`           // deterministic fact extractors; default: header ("extractors: []" turns them off)
	IngestExtensions    []string          `yaml:"ingest_extensions"`    // file extensions ingested from a folder
	// IngestRoots are the folders the ingest tool may read when called over
	// MCP (symlinks resolved; empty = the MCP ingest tool refuses every
	// path). The CLI, a person at a terminal, may ingest anywhere.
	IngestRoots []string `yaml:"ingest_roots"`
	// NearDupCandidates is how many events (FTS5 BM25 top-N of the new
	// event's text) the near-duplicate guard compares a new event with.
	NearDupCandidates int `yaml:"near_dup_candidates"`
	// NearDupThresholds overrides NearDupThreshold per embedding model
	// (decision 0125), keyed like read.evidence_floors: model id, then its
	// family (the part before "@"). Calibrated with "chanda eval -calibrate".
	// When the file does not set near_dup_threshold, Load (and Default)
	// resolve it from this map for embed.model_id.
	NearDupThresholds map[string]float64 `yaml:"near_dup_thresholds"`
	// NearDupLexical is the lexical path of the near-duplicate guard per
	// embedding model (decision 0138), keyed like near_dup_thresholds: a
	// newer text that repeats every word of an older one and adds at most
	// max_added_words is a near-duplicate above min_cosine (signatures
	// still identical). A model without an entry has the plain rule only.
	NearDupLexical map[string]NearDupLexical `yaml:"near_dup_lexical"`
}

// NearDupLexical configures the lexical path of the near-duplicate guard
// for one embedding model. min_cosine 0 turns it off.
type NearDupLexical struct {
	MaxAddedWords int     `yaml:"max_added_words"`
	MinCosine     float64 `yaml:"min_cosine"`
}

// Lexical returns the lexical path of the near-duplicate guard for an
// embedding model: the entry of NearDupLexical for the model id, else for
// its family, else the zero value (off).
func (w WriteConfig) Lexical(modelID string) NearDupLexical {
	return perModel(w.NearDupLexical, modelID, NearDupLexical{})
}

// DefaultNearDupThreshold is write.near_dup_threshold for a model without an
// entry in write.near_dup_thresholds (BASE.md "Near-duplicate guard").
const DefaultNearDupThreshold = 0.95

// NearDup returns the near-duplicate cosine threshold for an embedding
// model: the entry of NearDupThresholds for the model id, else for its
// family, else NearDupThreshold.
func (w WriteConfig) NearDup(modelID string) float64 {
	return perModel(w.NearDupThresholds, modelID, w.NearDupThreshold)
}

// perModel looks modelID up in m, then its family (before "@"), else
// returns fallback.
func perModel[T any](m map[string]T, modelID string, fallback T) T {
	if v, ok := m[modelID]; ok && modelID != "" {
		return v
	}
	if i := strings.IndexByte(modelID, '@'); i > 0 {
		if v, ok := m[modelID[:i]]; ok {
			return v
		}
	}
	return fallback
}

// ExtractorConfig declares one deterministic fact extractor (write path, no LLM).
// Kind: "header" (metadata header key: value), "kv_table" (markdown 2-col table),
// "regex" (Pattern with named groups subject, value; Predicate fixed or group "predicate").
type ExtractorConfig struct {
	Name      string `yaml:"name"`
	Kind      string `yaml:"kind"`
	Pattern   string `yaml:"pattern"`
	Predicate string `yaml:"predicate"`
	Subject   string `yaml:"subject"`
}

type ReadConfig struct {
	KeywordTopK          int     `yaml:"keyword_top_k"` // 50 per record type
	VectorTopK           int     `yaml:"vector_top_k"`  // 50
	RRFK                 int     `yaml:"rrf_k"`         // 60
	RerankTopN           int     `yaml:"rerank_top_n"`  // 30
	RerankEnabled        bool    `yaml:"rerank_enabled"`
	RelevanceThreshold   float64 `yaml:"relevance_threshold"`    // context-mode cut
	MaxPerDocument       int     `yaml:"max_per_document"`       // 3
	SearchDefaultLimit   int     `yaml:"search_default_limit"`   // 20 items
	ContextDefaultTokens int     `yaml:"context_default_tokens"` // 800
	ContextMaxTokens     int     `yaml:"context_max_tokens"`     // 1500
	// Abstention evidence (decision 0020): a context item needs lexical or
	// semantic evidence on top of the relevance threshold.
	MinTermCoverage      float64 `yaml:"min_term_coverage"`       // matched query terms / min(terms, cap)
	CoverageTermCap      int     `yaml:"coverage_term_cap"`       // long queries need at most this many matches
	MinCosine            float64 `yaml:"min_cosine"`              // semantic evidence floor; calibrate per embedding model
	MaxQueryTerms        int     `yaml:"max_query_terms"`         // terms kept from free text for MATCH
	SearchMaxLimit       int     `yaml:"search_max_limit"`        // ceiling on search limit
	ContextMinItemTokens int     `yaml:"context_min_item_tokens"` // smallest truncated item worth including
	// EvidenceFloors overrides MinTermCoverage and MinCosine per embedding
	// model (decision 0121), keyed by model id or by its family, the part
	// before "@" ("e5-small" for "e5-small@hugot0.8.6+simd"). Calibrated on
	// the training split with "chanda eval -calibrate C3".
	EvidenceFloors map[string]EvidenceFloor `yaml:"evidence_floors"`

	// Context-mode selection knobs (decision 0137). Each default keeps the
	// block exactly as it was before them; the defaults are left for the
	// selection study (eval/selection/PROTOCOL.md) to decide. omitempty keeps
	// the configuration hash of a file that does not set them unchanged.
	//
	// ContextItemMaxTokens: an item whose rendered block (label included)
	// would cost more is shortened to fit it; 0 = no per-item cap.
	ContextItemMaxTokens int `yaml:"context_item_max_tokens,omitempty"`
	// ContextItemCapAfter: the first N items of the block (the best ranked)
	// are exempt from context_item_max_tokens and keep their whole text
	// (budget permitting); the cap applies from item N+1. 0 = cap every
	// item.
	ContextItemCapAfter int `yaml:"context_item_cap_after,omitempty"`
	// ContextTruncate is how an item is shortened, for the per-item cap and
	// for the last item that only partly fits the budget: "head" (or empty,
	// the default) keeps the start; "best_window" keeps the contiguous run
	// of sentences that best matches the query terms.
	ContextTruncate string `yaml:"context_truncate,omitempty"`
	// ContextMaxPerDocument is the per-document limit in context mode:
	// 0 = read.max_per_document, -1 = no limit. Session turns carry no
	// document, so neither limit applies to them.
	ContextMaxPerDocument int `yaml:"context_max_per_document,omitempty"`
	// ContextMaxPerSession: at most this many raw excerpts of one session
	// (events.session_id) in a context block; 0 = no limit.
	ContextMaxPerSession int `yaml:"context_max_per_session,omitempty"`
	// ContextShowRole adds the speaker role of a session turn to its label
	// ("role: user").
	ContextShowRole bool `yaml:"context_show_role,omitempty"`
}

// Values of read.context_truncate.
const (
	TruncateHead       = "head"
	TruncateBestWindow = "best_window"
)

// BestWindow reports whether context items are shortened by best_window.
func (r ReadConfig) BestWindow() bool { return r.ContextTruncate == TruncateBestWindow }

// ContextPerDocument is the per-document item limit in context mode; 0
// means no limit.
func (r ReadConfig) ContextPerDocument() int {
	switch {
	case r.ContextMaxPerDocument < 0:
		return 0
	case r.ContextMaxPerDocument > 0:
		return r.ContextMaxPerDocument
	}
	return r.MaxPerDocument
}

// EvidenceFloor is the evidence a context item needs for one embedding
// model: lexical coverage or cosine at or above its floor (decision 0020).
type EvidenceFloor struct {
	MinTermCoverage float64 `yaml:"min_term_coverage"`
	MinCosine       float64 `yaml:"min_cosine"` // above 1: cosine alone never suffices
}

// Floors returns the evidence floors for an embedding model: the entry of
// EvidenceFloors for the model id, else for its family (before "@"), else
// MinTermCoverage and MinCosine.
func (r ReadConfig) Floors(modelID string) EvidenceFloor {
	return perModel(r.EvidenceFloors, modelID, EvidenceFloor{MinTermCoverage: r.MinTermCoverage, MinCosine: r.MinCosine})
}

type EmbedConfig struct {
	Provider          string `yaml:"provider"` // "hash" (deterministic, built-in) or "hugot"
	ModelID           string `yaml:"model_id"`
	ModelPath         string `yaml:"model_path"` // local directory; never downloaded at read/write time
	Dim               int    `yaml:"dim"`
	BatchSize         int    `yaml:"batch_size"`
	RerankerModelPath string `yaml:"reranker_model_path"`
	PollIntervalMS    int    `yaml:"poll_interval_ms"` // background indexer re-checks embed_queue this often
	ScanWorkers       int    `yaml:"scan_workers"`     // goroutines per vector scan; 0 = GOMAXPROCS
}

type FactsConfig struct {
	SinglePredicates  []string `yaml:"single_predicates"` // initial catalog
	ConfirmBoost      float64  `yaml:"confirm_boost"`     // confidence increase on same-value repeat
	InitialConfidence float64  `yaml:"initial_confidence"`
}

type SleepConfig struct {
	IdleMinutes     int     `yaml:"idle_minutes"`   // 30
	VolumeEvents    int     `yaml:"volume_events"`  // 200
	IntervalHours   int     `yaml:"interval_hours"` // 24
	BudgetTokens    int     `yaml:"budget_tokens"`
	BudgetEvents    int     `yaml:"budget_events"`
	GateTolerance   float64 `yaml:"gate_tolerance"`    // 2 points
	LessonMinEvents int     `yaml:"lesson_min_events"` // 3
	SnapshotDir     string  `yaml:"snapshot_dir"`
	MergeSimilarity float64 `yaml:"merge_similarity"`
	// MergeSimilarities overrides MergeSimilarity for the cosine of entity
	// name vectors, per embedding model (decision 0125), keyed like
	// read.evidence_floors. Name (edit) similarity does not depend on the
	// model and always uses MergeSimilarity.
	MergeSimilarities map[string]float64 `yaml:"merge_similarities"`

	CheckEveryMinutes int     `yaml:"check_every_minutes"` // scheduler poll period
	BurstSeconds      int     `yaml:"burst_seconds"`       // a write this recent means a burst: no run starts (0 disables)
	BatchEvents       int     `yaml:"batch_events"`        // events per LLM call (extraction batch, lesson cluster cap)
	LessonSimilarity  float64 `yaml:"lesson_similarity"`   // Jaccard over key terms for "similar events"
	LessonPoolEvents  int     `yaml:"lesson_pool_events"`  // most recent uncited events considered for lessons
	MergeMaxEntities  int     `yaml:"merge_max_entities"`  // above this, vector merge proposals are skipped
	GateProposals     int     `yaml:"gate_proposals"`      // gate questions phase 1 may propose per run (0 disables)
	Scheduler         bool    `yaml:"scheduler"`           // run sleep on its own (App.Start); false: on demand only
	StaleRunMinutes   int     `yaml:"stale_run_minutes"`   // a run still "running" after this long is recovered as failed (startup, next run); 0 disables
}

// MergeVectorSimilarity returns the cosine threshold of vector merge
// proposals for an embedding model: the entry of MergeSimilarities for the
// model id, else for its family, else MergeSimilarity.
func (s SleepConfig) MergeVectorSimilarity(modelID string) float64 {
	return perModel(s.MergeSimilarities, modelID, s.MergeSimilarity)
}

type RankingConfig struct {
	Enabled              bool    `yaml:"enabled"`
	MinExposures         int     `yaml:"min_exposures"` // 5
	LambdaMax            float64 `yaml:"lambda_max"`    // 0.3
	LambdaStep           float64 `yaml:"lambda_step"`
	PromotionMargin      float64 `yaml:"promotion_margin"`
	UnusedPenalty        float64 `yaml:"unused_penalty"` // 0.25
	OutcomeRatePerMinute int     `yaml:"outcome_rate_per_minute"`
	MovingAverageAlpha   float64 `yaml:"moving_average_alpha"`
	FlagPenalty          float64 `yaml:"flag_penalty"`       // beta added by flag (strong negative)
	RankerKind           string  `yaml:"ranker_kind"`        // "logreg" or "mlp"
	TrainIterations      int     `yaml:"train_iterations"`   // full-batch gradient steps
	LearningRate         float64 `yaml:"learning_rate"`      // gradient step size
	L2                   float64 `yaml:"l2"`                 // weight decay
	HiddenUnits          int     `yaml:"hidden_units"`       // MLP hidden layer width
	MinTrainExamples     int     `yaml:"min_train_examples"` // below this, Train refuses
	HoldoutFraction      float64 `yaml:"holdout_fraction"`   // most recent share of retrievals kept for the eval score
	ModelSeed            string  `yaml:"model_seed"`         // seed of the hash-based MLP init
}

type LLMConfig struct {
	Endpoint       string `yaml:"endpoint"` // OpenAI-compatible base URL; empty disables phase 2
	Model          string `yaml:"model"`
	APIKeyEnv      string `yaml:"api_key_env"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	ResponseFormat string `yaml:"response_format"` // auto (json_schema, falling back to json_object), json_schema, json_object
	MaxTokens      int    `yaml:"max_tokens"`      // output ceiling per call
}

type MCPConfig struct {
	Profiles   []string `yaml:"profiles"` // subset of read, standard, admin
	HTTPAddr   string   `yaml:"http_addr"`
	Actor      string   `yaml:"actor"`       // identity recorded in the audit log for this client
	HumanActor bool     `yaml:"human_actor"` // promote is allowed only when true
	// MaxZone is the highest trust zone a write through this channel may get
	// (decision 0090): zone = MinZone(ZoneForSource(kind), max_zone).
	// "user" is allowed only with human_actor.
	MaxZone string `yaml:"max_zone"`
	// HTTPTokenEnv names the environment variable holding the bearer token
	// the Streamable HTTP transport requires (decision 0100). Unset or
	// empty: no authentication, and HTTP serves on loopback addresses only.
	HTTPTokenEnv string `yaml:"http_token_env"`
}

type ForgetConfig struct {
	TombstoneKeepsHash bool `yaml:"tombstone_keeps_hash"`
	ProbeTerms         int  `yaml:"probe_terms"`        // rarest tokens per removed event searched after deletion (3)
	ProbeNeedles       int  `yaml:"probe_needles"`      // byte strings per removed text scanned in the file (4)
	ProbeNeedleBytes   int  `yaml:"probe_needle_bytes"` // length of each byte string, at least 12 (16)
	ProbeVectorK       int  `yaml:"probe_vector_k"`     // neighbours checked per saved vector (50)
}

// Default returns the configuration with every default from BASE.md.
func Default() *Config {
	c := defaults()
	c.Write.NearDupThreshold = c.Write.NearDup(c.Embed.ModelID)
	return c
}

func defaults() *Config {
	return &Config{
		DBPath:    "chanda.db",
		Namespace: "default",
		IDSeed:    "chanda",
		Write: WriteConfig{
			MinChars: 3, MaxChars: 1 << 20, MaxChunkChars: 2000, NearDupThreshold: DefaultNearDupThreshold,
			IngestExtensions: []string{".md", ".markdown", ".txt"}, NearDupCandidates: 10,
			Extractors: []ExtractorConfig{{Name: "header", Kind: "header"}},
			// Calibrated on corpus pairs of synthetic-pt (decision 0125).
			NearDupThresholds: map[string]float64{"hash-v1-256": 0.937, "e5-small": 0.9875},
			// Lexical path, calibrated on the same pairs (decision 0138).
			NearDupLexical: map[string]NearDupLexical{
				"hash-v1-256": {MaxAddedWords: 3, MinCosine: 0.8693},
				"e5-small":    {MaxAddedWords: 3, MinCosine: 0.9288},
			},
		},
		Read: ReadConfig{
			KeywordTopK: 50, VectorTopK: 50, RRFK: 60, RerankTopN: 30, RerankEnabled: false,
			RelevanceThreshold: 0.0, MaxPerDocument: 3, SearchDefaultLimit: 20,
			ContextDefaultTokens: 800, ContextMaxTokens: 1500,
			MinTermCoverage: 0.5, CoverageTermCap: 4, MinCosine: 0.5, MaxQueryTerms: 32,
			SearchMaxLimit: 100, ContextMinItemTokens: 40,
			// Calibrated on the training split of synthetic-pt (decision 0124).
			EvidenceFloors: map[string]EvidenceFloor{
				"hash-v1-256": {MinTermCoverage: 0.25, MinCosine: 0.2359},
				"e5-small":    {MinTermCoverage: 0.25, MinCosine: 0.8160564601421356},
			},
		},
		Embed: EmbedConfig{Provider: "hash", ModelID: "hash-v1-256", Dim: 256, BatchSize: 32, PollIntervalMS: 1000},
		Facts: FactsConfig{
			SinglePredicates:  []string{"job_title", "address", "current_price", "owner", "email", "phone", "status", "manager", "employer", "location"},
			ConfirmBoost:      0.1,
			InitialConfidence: 0.5,
		},
		Sleep: SleepConfig{
			IdleMinutes: 30, VolumeEvents: 200, IntervalHours: 24,
			BudgetTokens: 200000, BudgetEvents: 500, GateTolerance: 2.0, LessonMinEvents: 3,
			SnapshotDir: "snapshots", MergeSimilarity: 0.92,
			// Vector side, calibrated on entity names (decision 0125); hash keeps 0.92.
			MergeSimilarities: map[string]float64{"e5-small": 0.972},
			CheckEveryMinutes: 5, BurstSeconds: 10, BatchEvents: 20, LessonSimilarity: 0.5,
			LessonPoolEvents: 1000, MergeMaxEntities: 2000, GateProposals: 10, Scheduler: true, StaleRunMinutes: 60,
		},
		Ranking: RankingConfig{
			Enabled: true, MinExposures: 5, LambdaMax: 0.3, LambdaStep: 0.05, PromotionMargin: 1.0,
			UnusedPenalty: 0.25, OutcomeRatePerMinute: 60, MovingAverageAlpha: 0.1,
			FlagPenalty: 10, RankerKind: "logreg", TrainIterations: 300, LearningRate: 0.5, L2: 0.001,
			HiddenUnits: 8, MinTrainExamples: 50, HoldoutFraction: 0.2, ModelSeed: "chanda-rank",
		},
		LLM:    LLMConfig{TimeoutSeconds: 120, ResponseFormat: "auto", MaxTokens: 2048},
		MCP:    MCPConfig{Profiles: []string{"read", "standard"}, Actor: "agent", MaxZone: "internal", HTTPTokenEnv: "CHANDA_MCP_TOKEN"},
		Store:  StoreConfig{CacheMB: 256, MmapMB: 0},
		Forget: ForgetConfig{TombstoneKeepsHash: false, ProbeTerms: 3, ProbeNeedles: 4, ProbeNeedleBytes: 16, ProbeVectorK: 50},
	}
}

// Load reads path over the defaults. A missing file returns the defaults.
func Load(path string) (*Config, error) {
	c := Default()
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	c = defaults()
	c.Write.NearDupThreshold = 0 // unset: resolved per model below
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil && err.Error() != "EOF" {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if c.Write.NearDupThreshold == 0 {
		// Not in the file: the calibrated value of the configured model
		// (decision 0125). The near-duplicate guard reads the scalar.
		c.Write.NearDupThreshold = DefaultNearDupThreshold
		c.Write.NearDupThreshold = c.Write.NearDup(c.Embed.ModelID)
	}
	return c, c.Validate()
}

func (c *Config) Validate() error {
	if c.Read.ContextDefaultTokens > c.Read.ContextMaxTokens {
		return fmt.Errorf("read.context_default_tokens > context_max_tokens")
	}
	if err := c.Read.validateContext(); err != nil {
		return err
	}
	if c.Ranking.LambdaMax < 0 || c.Ranking.LambdaMax > 1 {
		return fmt.Errorf("ranking.lambda_max must be in [0,1]")
	}
	if c.Write.NearDupThreshold <= 0 || c.Write.NearDupThreshold > 1 {
		return fmt.Errorf("write.near_dup_threshold must be in (0,1]")
	}
	if c.Write.NearDupCandidates < 0 {
		return fmt.Errorf("write.near_dup_candidates must be >= 0")
	}
	for k, l := range c.Write.NearDupLexical {
		if l.MinCosine < 0 || l.MinCosine > 1 || l.MaxAddedWords < 0 {
			return fmt.Errorf("write.near_dup_lexical[%q]: min_cosine must be in [0,1] and max_added_words >= 0", k)
		}
	}
	switch c.MCP.MaxZone {
	case "external", "internal":
	case "user":
		if !c.MCP.HumanActor {
			return fmt.Errorf("mcp.max_zone: user requires mcp.human_actor: true")
		}
	default:
		return fmt.Errorf("mcp.max_zone must be external, internal or user, not %q", c.MCP.MaxZone)
	}
	return nil
}

// validateContext checks the context-mode selection knobs (decision 0137).
func (r ReadConfig) validateContext() error {
	switch r.ContextTruncate {
	case "", TruncateHead, TruncateBestWindow:
	default:
		return fmt.Errorf("read.context_truncate must be head or best_window, not %q", r.ContextTruncate)
	}
	if r.ContextItemMaxTokens < 0 {
		return fmt.Errorf("read.context_item_max_tokens must be >= 0")
	}
	if r.ContextItemMaxTokens > 0 && r.ContextItemMaxTokens < r.ContextMinItemTokens {
		return fmt.Errorf("read.context_item_max_tokens must be 0 or >= read.context_min_item_tokens (%d)", r.ContextMinItemTokens)
	}
	if r.ContextItemCapAfter < 0 {
		return fmt.Errorf("read.context_item_cap_after must be >= 0")
	}
	if r.ContextMaxPerDocument < -1 {
		return fmt.Errorf("read.context_max_per_document must be >= -1")
	}
	if r.ContextMaxPerSession < 0 {
		return fmt.Errorf("read.context_max_per_session must be >= 0")
	}
	return nil
}

// Hash is the configuration hash required in evaluation reports. It covers
// what decides behaviour, not where files live or how the server listens:
// db_path, sleep.snapshot_dir, write.ingest_roots, mcp.http_addr,
// mcp.http_token_env and store (memory tuning) are left out, so the same
// configuration hashes the same on every machine (and the hash logged with
// each retrieval keeps the database byte-identical).
func (c *Config) Hash() string {
	cp := *c
	cp.DBPath, cp.Sleep.SnapshotDir, cp.Write.IngestRoots, cp.MCP.HTTPAddr = "", "", nil, ""
	cp.MCP.HTTPTokenEnv, cp.Store = "", StoreConfig{}
	if cp.Read.ContextTruncate == TruncateHead {
		cp.Read.ContextTruncate = "" // the default, spelled out
	}
	b, _ := yaml.Marshal(&cp)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}
