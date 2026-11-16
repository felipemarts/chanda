package chanda

// Tool names (BASE.md "MCP interface and agent integration"), in table order.
const (
	ToolContext      = "context"
	ToolSearch       = "search"
	ToolGet          = "get"
	ToolTimeline     = "timeline"
	ToolExplain      = "explain"
	ToolStats        = "stats"
	ToolRemember     = "remember"
	ToolAssertFact   = "assert_fact"
	ToolIngest       = "ingest"
	ToolLogSession   = "log_session"
	ToolOutcome      = "outcome"
	ToolFlag         = "flag"
	ToolFlush        = "flush"
	ToolForget       = "forget"
	ToolPromote      = "promote"
	ToolSleepRun     = "sleep_run"
	ToolVerify       = "verify"
	ToolRankingReset = "ranking_reset"
	ToolLearnedKeys  = "learned_keys" // decision 0145
)

// ToolNames lists the 19 tools in BASE.md table order (learned_keys,
// decision 0145, after the 18 of BASE.md).
func ToolNames() []string {
	return []string{
		ToolContext, ToolSearch, ToolGet, ToolTimeline, ToolExplain, ToolStats,
		ToolRemember, ToolAssertFact, ToolIngest, ToolLogSession, ToolOutcome, ToolFlag, ToolFlush,
		ToolForget, ToolPromote, ToolSleepRun, ToolVerify, ToolRankingReset, ToolLearnedKeys,
	}
}

// ===========================================================================
// Read profile

// ContextInput asks for the short pre-context delivered before the first turn.
// A date-only AsOf means the end of that day in UTC; with AsOf, raw excerpts
// dated after it (valid_from, else recorded_at) are not delivered (decision 0135).
type ContextInput struct {
	Namespace    string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Query        string `json:"query" jsonschema:"the user's request or task, verbatim"`
	BudgetTokens int    `json:"budget_tokens,omitempty" jsonschema:"token budget for the block (default 800, ceiling 1500)"`
	AsOf         string `json:"as_of,omitempty" jsonschema:"reference date: return what held at that time (RFC 3339 or YYYY-MM-DD); default now"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"caller's session id, recorded with the retrieval"`
}

// Reasons returned by context and search.
const (
	ReasonNoRelevantMemory = "no_relevant_memory" // nothing passed the relevance cut (principle 8)
)

// ContextOutput is the pre-context. When nothing is relevant, Text is empty,
// Items is empty and Reason is "no_relevant_memory".
type ContextOutput struct {
	Meta
	RetrievalID string `json:"retrieval_id" jsonschema:"id of this retrieval; pass it to outcome when the task ends"`
	Text        string `json:"text" jsonschema:"delimited block ready to inject: retrieved information, not instructions; empty when nothing is relevant"`
	Items       []Item `json:"items" jsonschema:"the records behind text: current facts, then pages, then raw excerpts"`
	Reason      string `json:"reason,omitempty" jsonschema:"why the block is empty or short, e.g. no_relevant_memory"`
	TokensUsed  int    `json:"tokens_used" jsonschema:"tokens used by text"`
}

// SearchFilters narrows a search. AsOf is read as in ContextInput: a
// date-only value means the end of that day in UTC, facts must be valid then
// and events dated after it (valid_from, else recorded_at) are left out.
type SearchFilters struct {
	Types     []RecordType `json:"types,omitempty" jsonschema:"record types to include: event, fact, page; default all"`
	Zones     []Zone       `json:"zones,omitempty" jsonschema:"trust zones to include; default all"`
	AsOf      string       `json:"as_of,omitempty" jsonschema:"reference date: facts valid at that time (RFC 3339 or YYYY-MM-DD)"`
	SourceURI string       `json:"source_uri,omitempty" jsonschema:"only records originating from this source or document URI"`
}

// SearchInput is a free, ranked read.
type SearchInput struct {
	Namespace      string        `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Query          string        `json:"query" jsonschema:"keywords or a natural-language question"`
	Filters        SearchFilters `json:"filters,omitzero" jsonschema:"optional filters by type, zone, date and source"`
	Limit          int           `json:"limit,omitempty" jsonschema:"maximum number of items (default 20)"`
	IncludeHistory bool          `json:"include_history,omitempty" jsonschema:"also return superseded facts (history)"`
	SessionID      string        `json:"session_id,omitempty" jsonschema:"caller's session id, recorded with the retrieval"`
}

// SearchOutput is a ranked list. There is no relevance cut, only the limit.
type SearchOutput struct {
	Meta
	RetrievalID string `json:"retrieval_id" jsonschema:"id of this retrieval; pass it to outcome or explain"`
	Items       []Item `json:"items" jsonschema:"results by descending score"`
	Reason      string `json:"reason,omitempty" jsonschema:"set when items is empty, e.g. no_relevant_memory"`
}

// GetInput fetches one record.
type GetInput struct {
	Namespace       string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	ID              string `json:"id" jsonschema:"record id (evt_, fct_, pg_, pgi_, ent_, src_ or doc_ prefix)"`
	ExpandNeighbors bool   `json:"expand_neighbors,omitempty" jsonschema:"also return adjacent chunks/turns and related facts"`
}

// Edge is one provenance edge from the requested record to an origin.
type Edge struct {
	Relation   string     `json:"relation" jsonschema:"derived_from, confirmed_by, duplicate_of, supersedes, cites or mentions"`
	SourceType RecordType `json:"source_type" jsonschema:"type of the origin record"`
	SourceID   string     `json:"source_id" jsonschema:"id of the origin record"`
}

// FactDetail is present when the record is a fact.
type FactDetail struct {
	Subject      string  `json:"subject" jsonschema:"subject entity id"`
	SubjectName  string  `json:"subject_name,omitempty" jsonschema:"canonical name of the subject"`
	Predicate    string  `json:"predicate" jsonschema:"attribute name"`
	Value        string  `json:"value" jsonschema:"attribute value as asserted"`
	Confidence   float64 `json:"confidence" jsonschema:"confidence in [0,1]; rises with confirmations"`
	SupersededBy string  `json:"superseded_by,omitempty" jsonschema:"id of the fact that closed this one"`
}

// GetOutput is one record with its origins.
type GetOutput struct {
	Meta
	Item       Item        `json:"item" jsonschema:"the record"`
	RecordedAt string      `json:"recorded_at" jsonschema:"when Chanda learned of it"`
	Fact       *FactDetail `json:"fact,omitempty" jsonschema:"structured fields, for facts"`
	Provenance []Edge      `json:"provenance" jsonschema:"provenance edges to its origins, sorted"`
	Neighbors  []Item      `json:"neighbors,omitempty" jsonschema:"adjacent or related records, when expand_neighbors is set"`
}

// TimelineInput asks for the history of a subject (and optionally one attribute).
type TimelineInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Subject   string `json:"subject" jsonschema:"entity id or name (aliases are resolved)"`
	Predicate string `json:"predicate,omitempty" jsonschema:"attribute; omit for every attribute of the subject"`
	AsOf      string `json:"as_of,omitempty" jsonschema:"reference date: only the facts that held then (RFC 3339 or YYYY-MM-DD); default the whole history"`
}

// TimelineEntry is one fact in a timeline.
type TimelineEntry struct {
	FactID       string `json:"fact_id" jsonschema:"fact id"`
	Predicate    string `json:"predicate" jsonschema:"attribute"`
	Value        string `json:"value" jsonschema:"value"`
	ValidFrom    string `json:"valid_from" jsonschema:"start of validity"`
	ValidTo      string `json:"valid_to,omitempty" jsonschema:"end of validity; absent means current"`
	RecordedAt   string `json:"recorded_at" jsonschema:"when Chanda learned of it"`
	Status       string `json:"status" jsonschema:"active, superseded, quarantined, stale_source, rejected or flagged"`
	Zone         Zone   `json:"zone" jsonschema:"trust zone"`
	SupersededBy string `json:"superseded_by,omitempty" jsonschema:"fact that closed this one"`
	Origin       Origin `json:"origin" jsonschema:"origin of this change"`
}

// TimelineOutput lists facts from the oldest to the current one.
type TimelineOutput struct {
	Meta
	Subject     string          `json:"subject" jsonschema:"resolved subject entity id"`
	SubjectName string          `json:"subject_name,omitempty" jsonschema:"canonical name of the subject"`
	Entries     []TimelineEntry `json:"entries" jsonschema:"facts ordered by predicate, then valid_from, oldest first"`
}

// ExplainInput asks why a record ranked where it did in a retrieval.
type ExplainInput struct {
	Namespace   string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	RetrievalID string `json:"retrieval_id" jsonschema:"retrieval id returned by context or search"`
	RecordID    string `json:"record_id" jsonschema:"record to explain"`
}

// StageScores holds the score of a record at each read-path stage; a stage
// that did not run is absent.
type StageScores struct {
	BM25    *float64 `json:"bm25,omitempty" jsonschema:"keyword score"`
	Cosine  *float64 `json:"cosine,omitempty" jsonschema:"vector similarity"`
	RRF     *float64 `json:"rrf,omitempty" jsonschema:"reciprocal rank fusion score"`
	Rerank  *float64 `json:"rerank,omitempty" jsonschema:"cross-encoder score"`
	Utility *float64 `json:"utility,omitempty" jsonschema:"learned utility of the record"`
	Learned *float64 `json:"learned,omitempty" jsonschema:"learned ranker score (shadow or live)"`
	Final   *float64 `json:"final,omitempty" jsonschema:"final score"`
}

// ExplainOutput is the per-stage breakdown.
type ExplainOutput struct {
	Meta
	RetrievalID string             `json:"retrieval_id" jsonschema:"retrieval id"`
	RecordID    string             `json:"record_id" jsonschema:"record id"`
	Mode        string             `json:"mode" jsonschema:"search or context"`
	Query       string             `json:"query" jsonschema:"query text of the retrieval"`
	Candidate   bool               `json:"candidate" jsonschema:"whether the record was among the candidates"`
	Delivered   bool               `json:"delivered" jsonschema:"whether the record was delivered"`
	Rank        int                `json:"rank,omitempty" jsonschema:"1-based position among candidates by final score"`
	Scores      StageScores        `json:"scores" jsonschema:"score at each stage"`
	Features    map[string]float64 `json:"features,omitempty" jsonschema:"ranking features used"`
	Reason      string             `json:"reason" jsonschema:"why it was or was not delivered, e.g. below_threshold, budget, diversity_cap, filtered"`
}

// StatsInput asks for namespace statistics.
type StatsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
}

// Counts are row counts in one namespace.
type Counts struct {
	Sources       int64 `json:"sources"`
	Documents     int64 `json:"documents"`
	Events        int64 `json:"events"`
	RetiredEvents int64 `json:"retired_events"`
	Entities      int64 `json:"entities"`
	Facts         int64 `json:"facts"`
	ActiveFacts   int64 `json:"active_facts"`
	Quarantined   int64 `json:"quarantined_facts"`
	Pages         int64 `json:"pages"`
	Embeddings    int64 `json:"embeddings"`
	Retrievals    int64 `json:"retrievals"`
	Outcomes      int64 `json:"outcomes"`
	Tombstones    int64 `json:"tombstones"`
}

// SleepSummary describes one sleep run.
type SleepSummary struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status" jsonschema:"running, completed, partial, reverted or failed"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
}

// RankingState is the learning state of a namespace.
type RankingState struct {
	Enabled     bool    `json:"enabled" jsonschema:"learning switched on"`
	Lambda      float64 `json:"lambda" jsonschema:"weight of utility in the final score (0 = baseline)"`
	ActiveModel string  `json:"active_model,omitempty" jsonschema:"promoted ranker model id"`
	ShadowModel string  `json:"shadow_model,omitempty" jsonschema:"ranker model computed in shadow mode"`
}

// StatsOutput summarizes a namespace.
type StatsOutput struct {
	Meta
	Namespace    string        `json:"namespace"`
	Counts       Counts        `json:"counts"`
	DBSizeBytes  int64         `json:"db_size_bytes" jsonschema:"size of the database file"`
	EmbedPending int64         `json:"embed_pending" jsonschema:"records waiting in the embedding queue"`
	EmbedModel   string        `json:"embed_model,omitempty" jsonschema:"embedding model id in use"`
	LastSleep    *SleepSummary `json:"last_sleep,omitempty" jsonschema:"most recent sleep run"`
	Ranking      RankingState  `json:"ranking"`
}

// ===========================================================================
// Standard profile

// RememberInput writes free text.
type RememberInput struct {
	Namespace string    `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Content   string    `json:"content" jsonschema:"text to remember, self-contained"`
	Source    SourceRef `json:"source,omitzero" jsonschema:"input channel and source; default is the calling session (internal zone)"`
	ValidFrom string    `json:"valid_from,omitempty" jsonschema:"date from which the content holds (RFC 3339 or YYYY-MM-DD)"`
	SessionID string    `json:"session_id,omitempty" jsonschema:"caller's session id"`
}

// RememberOutput reports the write.
type RememberOutput struct {
	Meta
	WriteResult
}

// AssertFactInput writes a structured fact. Without ValidFrom, a call whose
// SessionID names a session whose turns carried ts takes the latest of those
// dates; otherwise the time of the call (decision 0135).
type AssertFactInput struct {
	Namespace string    `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Subject   string    `json:"subject" jsonschema:"entity the fact is about (name or entity id)"`
	Predicate string    `json:"predicate" jsonschema:"attribute, snake_case, e.g. job_title, address, owner"`
	Value     string    `json:"value" jsonschema:"attribute value"`
	ValidFrom string    `json:"valid_from,omitempty" jsonschema:"date from which the fact holds; default the time of the call"`
	Source    SourceRef `json:"source,omitzero" jsonschema:"input channel and source; default is the calling session (internal zone)"`
	SessionID string    `json:"session_id,omitempty" jsonschema:"caller's session id"`
}

// AssertFactOutput reports the reconciliation decision.
type AssertFactOutput struct {
	Meta
	WriteResult
}

// IngestInput ingests a document or a folder.
type IngestInput struct {
	Namespace string    `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Path      string    `json:"path" jsonschema:"file or folder path on the server host"`
	Source    SourceRef `json:"source,omitzero" jsonschema:"source; default kind document (internal zone) with the path as uri"`
}

// DocumentResult reports one ingested document.
type DocumentResult struct {
	URI           string `json:"uri" jsonschema:"document uri"`
	DocID         string `json:"doc_id,omitempty" jsonschema:"document id"`
	Version       int    `json:"version" jsonschema:"current version after ingestion"`
	Action        string `json:"action" jsonschema:"created, updated, unchanged, retired (left the ingested folder) or rejected"`
	Reason        string `json:"reason,omitempty" jsonschema:"why, when rejected or unchanged"`
	ChunksAdded   int    `json:"chunks_added"`
	ChunksKept    int    `json:"chunks_kept"`
	ChunksRetired int    `json:"chunks_retired"`
	FactsStale    int    `json:"facts_stale" jsonschema:"facts marked stale_source because their chunk was retired"`
}

// IngestOutput lists every document handled, sorted by uri.
type IngestOutput struct {
	Meta
	Documents []DocumentResult `json:"documents"`
}

// Turn is one session turn. TS (RFC 3339 or YYYY-MM-DD) becomes the
// valid_from of the turn's event; recorded_at stays the write time. An
// unparsable TS fails the call with invalid_argument (decision 0135).
type Turn struct {
	Role    string `json:"role" jsonschema:"user, assistant, system or tool; tool turns enter the external zone"`
	Content string `json:"content" jsonschema:"turn text"`
	TS      string `json:"ts,omitempty" jsonschema:"when the turn happened"`
	Name    string `json:"name,omitempty" jsonschema:"tool name, for tool turns; the turn is stored under the source tool:<name>"`
}

// LogSessionInput writes the turns of a session.
type LogSessionInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	SessionID string `json:"session_id" jsonschema:"session id; re-sending the same turns is idempotent"`
	Turns     []Turn `json:"turns" jsonschema:"turns in order"`
}

// TurnResult reports the write of one turn.
type TurnResult struct {
	Ordinal int `json:"ordinal" jsonschema:"0-based position in turns"`
	WriteResult
}

// LogSessionOutput reports each turn.
type LogSessionOutput struct {
	Meta
	SessionID string       `json:"session_id"`
	Turns     []TurnResult `json:"turns"`
}

// OutcomeInput reports how a task that used a retrieval went.
type OutcomeInput struct {
	Namespace   string   `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	RetrievalID string   `json:"retrieval_id" jsonschema:"retrieval id returned by context or search"`
	Value       float64  `json:"value" jsonschema:"task outcome from -1 (failed) to 1 (succeeded)"`
	UsedIDs     []string `json:"used_ids,omitempty" jsonschema:"ids of delivered records that were actually used"`
}

// OutcomeOutput reports the recorded outcome.
type OutcomeOutput struct {
	Meta
	OutcomeID string `json:"outcome_id"`
	Credited  int    `json:"credited" jsonschema:"records whose utility was updated"`
	// KeysCreated: learned keys this outcome made (decision 0145; only with
	// write.learned_keys on and value > 0).
	KeysCreated int `json:"keys_created,omitempty" jsonschema:"learned query keys created by this outcome"`
}

// FlagInput marks a record as wrong or outdated.
type FlagInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	ID        string `json:"id" jsonschema:"record id"`
	Reason    string `json:"reason" jsonschema:"why it is wrong or outdated"`
}

// FlagOutput reports the flag.
type FlagOutput struct {
	Meta
	FlagID string `json:"flag_id"`
	ID     string `json:"id" jsonschema:"flagged record"`
	Status string `json:"status" jsonschema:"record status after the flag"`
}

// FlushInput waits for the index to catch up with writes.
type FlushInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"maximum wait in milliseconds; default waits until done"`
}

// FlushOutput reports the index state.
type FlushOutput struct {
	Meta
	Indexed  int  `json:"indexed" jsonschema:"records indexed during this call"`
	Pending  int  `json:"pending" jsonschema:"records still waiting"`
	Complete bool `json:"complete" jsonschema:"true when nothing is pending"`
}

// ===========================================================================
// Administration profile

// Forget scope kinds.
const (
	ScopeRecord    = "record"
	ScopeDocument  = "document"
	ScopeSource    = "source"
	ScopeEntity    = "entity"
	ScopeDateRange = "date_range"
	ScopeSearch    = "search"
)

// ScopeKinds lists the forget scope kinds.
func ScopeKinds() []string {
	return []string{ScopeRecord, ScopeDocument, ScopeSource, ScopeEntity, ScopeDateRange, ScopeSearch}
}

// ForgetScope selects what to forget. Kind decides which other fields are used:
// record/document/source/entity use Value (an id or uri/name); date_range uses
// From and To; search uses Query.
type ForgetScope struct {
	Kind  string `json:"kind" jsonschema:"record, document, source, entity, date_range or search"`
	Value string `json:"value,omitempty" jsonschema:"record id, document uri or id, source uri or id, or entity name or id"`
	From  string `json:"from,omitempty" jsonschema:"date_range start, inclusive (recorded_at)"`
	To    string `json:"to,omitempty" jsonschema:"date_range end, exclusive (recorded_at)"`
	Query string `json:"query,omitempty" jsonschema:"search scope: every event matching this keyword query"`
}

// ForgetInput deletes in cascade.
type ForgetInput struct {
	Namespace string      `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Scope     ForgetScope `json:"scope" jsonschema:"what to forget"`
	DryRun    bool        `json:"dry_run,omitempty" jsonschema:"report what would be removed without removing anything"`
}

// RemovedCounts counts what forget removed (or would remove).
type RemovedCounts struct {
	Events       int64 `json:"events"`
	Documents    int64 `json:"documents"`
	Sources      int64 `json:"sources"`
	Facts        int64 `json:"facts"`
	FactsKept    int64 `json:"facts_kept" jsonschema:"facts with other origins that survived, losing only the edge"`
	PagesRebuild int64 `json:"pages_rebuild" jsonschema:"pages taken out of reads until rebuilt by sleep"`
	PageItems    int64 `json:"page_items"`
	Embeddings   int64 `json:"embeddings"`
	FTSEntries   int64 `json:"fts_entries"`
	Provenance   int64 `json:"provenance_edges"`
	Utility      int64 `json:"utility"`
	Retrievals   int64 `json:"retrievals"`
	Outcomes     int64 `json:"outcomes"`
	Snapshots    int64 `json:"snapshots"`
	EntityLinks  int64 `json:"entity_links"`
	Keys         int64 `json:"keys" jsonschema:"learned query keys"`
}

// Probe is one absence check run after deletion.
type Probe struct {
	Name   string `json:"name" jsonschema:"fts, vector or file_scan"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty" jsonschema:"counts or ids, never content"`
}

// Verification is the report of forget step 7.
type Verification struct {
	Passed bool    `json:"passed" jsonschema:"true when every probe passed"`
	Probes []Probe `json:"probes"`
}

// ForgetOutput reports the deletion and its verification.
type ForgetOutput struct {
	Meta
	DryRun       bool          `json:"dry_run"`
	TombstoneID  string        `json:"tombstone_id,omitempty" jsonschema:"tombstone written (absent on dry_run)"`
	RootEvents   int64         `json:"root_events" jsonschema:"raw events the scope resolved to"`
	Removed      RemovedCounts `json:"removed"`
	Verification Verification  `json:"verification" jsonschema:"absence probes (empty on dry_run)"`
}

// PromoteInput raises a record's trust zone. Only a human actor may call it.
type PromoteInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	ID        string `json:"id" jsonschema:"record id"`
	Zone      Zone   `json:"zone" jsonschema:"target zone, higher than the current one: internal or user"`
	Reason    string `json:"reason,omitempty" jsonschema:"why the record is trusted now"`
}

// PromoteOutput reports the change.
type PromoteOutput struct {
	Meta
	ID   string `json:"id"`
	From Zone   `json:"from"`
	To   Zone   `json:"to"`
}

// SleepBudget caps a sleep run; zero means the configured default.
type SleepBudget struct {
	Tokens int `json:"tokens,omitempty" jsonschema:"LLM token ceiling"`
	Events int `json:"events,omitempty" jsonschema:"events processed ceiling"`
}

// SleepRunInput triggers sleep.
type SleepRunInput struct {
	Namespace string      `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Phases    []int       `json:"phases,omitempty" jsonschema:"phases to run: 1 (maintenance), 2 (LLM consolidation); default both when an LLM is configured"`
	Budget    SleepBudget `json:"budget,omitzero" jsonschema:"ceilings for this run"`
}

// SleepCost is what a run spent.
type SleepCost struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	LLMCalls     int `json:"llm_calls"`
}

// SleepRunOutput is the run report.
type SleepRunOutput struct {
	Meta
	RunID      string    `json:"run_id"`
	Status     string    `json:"status" jsonschema:"completed, partial (budget reached), reverted (gate failed) or failed"`
	Phases     []int     `json:"phases"`
	Created    int       `json:"created"`
	Superseded int       `json:"superseded"`
	Merged     int       `json:"merged"`
	Rejected   int       `json:"rejected"`
	Cost       SleepCost `json:"cost"`
	Report     string    `json:"report" jsonschema:"readable report of the run"`
}

// VerifyInput checks the audit chain and the provenance graph.
type VerifyInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
}

// AuditCheck is the audit chain result.
type AuditCheck struct {
	OK       bool   `json:"ok"`
	Entries  int64  `json:"entries"`
	BrokenAt int64  `json:"broken_at,omitempty" jsonschema:"first audit id that fails"`
	Reason   string `json:"reason,omitempty"`
}

// ProvenanceCheck is the provenance graph result.
type ProvenanceCheck struct {
	OK      bool     `json:"ok"`
	Checked int64    `json:"checked" jsonschema:"derived records checked"`
	Orphans []string `json:"orphans" jsonschema:"ids of derived records with no path to an event (first 100, sorted)"`
}

// VerifyOutput reports both checks. A failed check is data (ok=false), not an error.
type VerifyOutput struct {
	Meta
	OK         bool            `json:"ok" jsonschema:"true when both checks pass"`
	Audit      AuditCheck      `json:"audit"`
	Provenance ProvenanceCheck `json:"provenance"`
}

// RankingResetInput clears the learning state of a namespace.
type RankingResetInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
}

// RankingResetOutput reports what was cleared.
type RankingResetOutput struct {
	Meta
	UtilityRows int64   `json:"utility_rows"`
	Models      int64   `json:"models"`
	Lambda      float64 `json:"lambda" jsonschema:"lambda after the reset (0)"`
}

// Actions of the learned_keys tool (decision 0145).
const (
	KeysEnable  = "enable"  // runtime switch on: reads use keys (with read.learned_keys)
	KeysDisable = "disable" // runtime switch off: reads ignore keys
	KeysReset   = "reset"   // delete every key of the namespace
	KeysRebuild = "rebuild" // create the missing keys of every positive outcome recorded
	KeysStats   = "stats"   // counts only
)

// KeysActions lists the learned_keys actions.
func KeysActions() []string {
	return []string{KeysEnable, KeysDisable, KeysReset, KeysRebuild, KeysStats}
}

// LearnedKeysInput controls the learned query keys of a namespace.
type LearnedKeysInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"memory namespace; omit for the configured default"`
	Action    string `json:"action" jsonschema:"enable, disable, reset, rebuild or stats"`
}

// LearnedKeysOutput reports the action and the keys of the namespace after it.
type LearnedKeysOutput struct {
	Meta
	Action   string `json:"action"`
	Enabled  bool   `json:"enabled" jsonschema:"runtime switch of the namespace after the call"`
	Keys     int64  `json:"keys" jsonschema:"keys in the namespace after the call"`
	Records  int64  `json:"records" jsonschema:"distinct records with at least one key"`
	Embedded int64  `json:"embedded" jsonschema:"keys with a vector; run flush so new keys get theirs"`
	Pending  int64  `json:"pending" jsonschema:"keys waiting in the embedding queue"`
	Removed  int64  `json:"removed,omitempty" jsonschema:"reset: keys deleted"`
	Outcomes int64  `json:"outcomes,omitempty" jsonschema:"rebuild: positive outcomes replayed"`
	Created  int64  `json:"created,omitempty" jsonschema:"rebuild: keys created"`
	Existing int64  `json:"existing,omitempty" jsonschema:"rebuild: keys that already existed"`
}
