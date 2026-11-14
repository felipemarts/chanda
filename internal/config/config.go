// Package config loads chanda.yaml. Every threshold and default cited in
// BASE.md lives here; code must read values from Config, never hard-code them.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

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
}

type WriteConfig struct {
	MinChars            int               `yaml:"min_chars"`            // admission: too short below this
	MaxChars            int               `yaml:"max_chars"`            // admission: over the size limit
	MaxChunkChars       int               `yaml:"max_chunk_chars"`      // structural chunker splits long paragraphs above this
	SecretPatterns      []string          `yaml:"secret_patterns"`      // extra regexes masked on top of the built-ins
	InstructionPatterns []string          `yaml:"instruction_patterns"` // extra regexes for instruction_like
	NearDupThreshold    float64           `yaml:"near_dup_threshold"`   // cosine, default 0.95
	Extractors          []ExtractorConfig `yaml:"extractors"`           // deterministic fact extractors
	IngestExtensions    []string          `yaml:"ingest_extensions"`    // file extensions ingested from a folder
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
}

type LLMConfig struct {
	Endpoint       string `yaml:"endpoint"` // OpenAI-compatible base URL; empty disables phase 2
	Model          string `yaml:"model"`
	APIKeyEnv      string `yaml:"api_key_env"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type MCPConfig struct {
	Profiles   []string `yaml:"profiles"` // subset of read, standard, admin
	HTTPAddr   string   `yaml:"http_addr"`
	Actor      string   `yaml:"actor"`       // identity recorded in the audit log for this client
	HumanActor bool     `yaml:"human_actor"` // promote is allowed only when true
}

type ForgetConfig struct {
	TombstoneKeepsHash bool `yaml:"tombstone_keeps_hash"`
}

// Default returns the configuration with every default from BASE.md.
func Default() *Config {
	return &Config{
		DBPath:    "chanda.db",
		Namespace: "default",
		IDSeed:    "chanda",
		Write: WriteConfig{
			MinChars: 3, MaxChars: 1 << 20, MaxChunkChars: 2000, NearDupThreshold: 0.95,
			IngestExtensions: []string{".md", ".markdown", ".txt"},
		},
		Read: ReadConfig{
			KeywordTopK: 50, VectorTopK: 50, RRFK: 60, RerankTopN: 30, RerankEnabled: false,
			RelevanceThreshold: 0.0, MaxPerDocument: 3, SearchDefaultLimit: 20,
			ContextDefaultTokens: 800, ContextMaxTokens: 1500,
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
		},
		Ranking: RankingConfig{
			Enabled: true, MinExposures: 5, LambdaMax: 0.3, LambdaStep: 0.05, PromotionMargin: 1.0,
			UnusedPenalty: 0.25, OutcomeRatePerMinute: 60, MovingAverageAlpha: 0.1,
		},
		LLM:    LLMConfig{TimeoutSeconds: 120},
		MCP:    MCPConfig{Profiles: []string{"read", "standard"}, Actor: "agent"},
		Forget: ForgetConfig{TombstoneKeepsHash: false},
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
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil && err.Error() != "EOF" {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return c, c.Validate()
}

func (c *Config) Validate() error {
	if c.Read.ContextDefaultTokens > c.Read.ContextMaxTokens {
		return fmt.Errorf("read.context_default_tokens > context_max_tokens")
	}
	if c.Ranking.LambdaMax < 0 || c.Ranking.LambdaMax > 1 {
		return fmt.Errorf("ranking.lambda_max must be in [0,1]")
	}
	if c.Write.NearDupThreshold <= 0 || c.Write.NearDupThreshold > 1 {
		return fmt.Errorf("write.near_dup_threshold must be in (0,1]")
	}
	return nil
}

// Hash is the configuration hash required in evaluation reports.
func (c *Config) Hash() string {
	b, _ := yaml.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}
