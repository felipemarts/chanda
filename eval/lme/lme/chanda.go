package lme

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// ChandaOptions configures one per-question Chanda server.
type ChandaOptions struct {
	Exe    string       // binary that runs "chanda serve" through cli.Run ("lme" itself: "lme chanda serve")
	Args   []string     // arguments before "serve" (e.g. ["chanda"])
	Embed  EmbedOptions // passed to the lme-cached provider through the environment
	Budget int          // read.context_max_tokens (the 2,000-token budget needs it above Chanda's 1,500 default)
	// Sleep (condition E): admin profile for sleep_run and the LLM endpoint.
	Admin     bool
	LLMURL    string
	LLMModel  string
	LLMKeyEnv string
	// SleepBudget caps each sleep_run (zero: Chanda's sleep.budget_tokens
	// and sleep.budget_events defaults, 200,000 tokens and 500 events).
	SleepBudget chanda.SleepBudget
}

// ChandaServer is a real chanda MCP server over stdio with its own database.
type ChandaServer struct {
	Dir    string
	DBPath string
	cs     *sdk.ClientSession
	logf   *os.File
}

// ConfigYAML is the chanda.yaml written for a per-question server. Every
// value not set here is Chanda's default. Deviations from the defaults, and
// why: embed.provider lme-cached (the real embedder behind the cache, same
// model id); read.context_max_tokens = the battery budget (default ceiling
// 1,500 would refuse 2,000); sleep.scheduler off (condition D is "without
// sleep"; E runs sleep explicitly through sleep_run); snapshot_dir inside the
// question's directory.
func (o ChandaOptions) ConfigYAML(dir string, modelID string, dim int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "db_path: %q\nnamespace: lme\n", filepath.Join(dir, "chanda.db"))
	fmt.Fprintf(&b, "embed:\n  provider: %s\n  model_id: %q\n  dim: %d\n", ProviderName, modelID, dim)
	if o.Embed.Kind == "e5" && o.Embed.E5Dir != "" {
		fmt.Fprintf(&b, "  model_path: %q\n", o.Embed.E5Dir)
	}
	fmt.Fprintf(&b, "read:\n  context_max_tokens: %d\n", max(o.Budget, 1500))
	fmt.Fprintf(&b, "sleep:\n  scheduler: false\n  snapshot_dir: %q\n", filepath.Join(dir, "snapshots"))
	profiles := "[read, standard]"
	if o.Admin {
		profiles = "[read, standard, admin]"
	}
	fmt.Fprintf(&b, "mcp:\n  profiles: %s\n", profiles)
	if o.LLMURL != "" {
		fmt.Fprintf(&b, "llm:\n  endpoint: %q\n  model: %q\n  response_format: json_object\n", o.LLMURL, o.LLMModel)
		if o.LLMKeyEnv != "" {
			fmt.Fprintf(&b, "  api_key_env: %q\n", o.LLMKeyEnv)
		}
	}
	return b.String()
}

// StartChanda writes dir/chanda.yaml and starts the server.
func StartChanda(ctx context.Context, o ChandaOptions, dir, modelID string, dim int) (*ChandaServer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cfg := filepath.Join(dir, "chanda.yaml")
	if err := os.WriteFile(cfg, []byte(o.ConfigYAML(dir, modelID, dim)), 0o644); err != nil {
		return nil, err
	}
	logf, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		return nil, err
	}
	args := append(append([]string{}, o.Args...), "serve", "--config", cfg)
	cmd := exec.Command(o.Exe, args...)
	cmd.Env = append(os.Environ(), o.Embed.Env()...)
	cmd.Stderr = logf
	client := sdk.NewClient(&sdk.Implementation{Name: "lme-harness", Version: "v1"}, nil)
	cs, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd, TerminateDuration: 10 * time.Second}, nil)
	if err != nil {
		logf.Close()
		return nil, fmt.Errorf("start chanda: %w (see %s)", err, logf.Name())
	}
	return &ChandaServer{Dir: dir, DBPath: filepath.Join(dir, "chanda.db"), cs: cs, logf: logf}, nil
}

// Call invokes a tool; a failed envelope is returned as an error.
func (c *ChandaServer) Call(ctx context.Context, tool string, args any, out any) error {
	res, err := c.cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return err
	}
	var env struct {
		chanda.Envelope
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("%s: %w (%.300s)", tool, err, b)
	}
	if !env.OK {
		if env.Error != nil {
			return fmt.Errorf("%s: %s: %s", tool, env.Error.Code, env.Error.Message)
		}
		return fmt.Errorf("%s: not ok (%.300s)", tool, b)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("%s data: %w", tool, err)
		}
	}
	return nil
}

// Close stops the server.
func (c *ChandaServer) Close() error {
	err := c.cs.Close()
	c.logf.Close()
	return err
}

// DiskBytes is the size of the database file and its WAL/SHM side files.
func DiskBytes(dbPath string) int64 {
	var n int64
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if st, err := os.Stat(p); err == nil {
			n += st.Size()
		}
	}
	return n
}

// ChandaSessionID is the opaque session id given to Chanda for haystack
// session i. The dataset ids are not used: evidence sessions are named
// "answer_..." in the dataset, and Chanda prints the session id in the
// context block ("source: ..."), which would leak the gold location.
func ChandaSessionID(i int) string { return fmt.Sprintf("s%03d", i) }

// Ingested maps Chanda record ids back to haystack session indexes.
type Ingested struct {
	EventSessions map[string][]int // event id -> haystack session indexes (a duplicate id may serve several)
	Turns         int
	Created       int
	Duplicates    int
	Rejected      int
	RejectReasons map[string]int
	NearDups      int
	Elapsed       time.Duration // log_session calls + flush
	FlushMS       float64
}

// IngestQuestion logs every haystack session (turn ts = session date) in
// chronological order (211 of the 500 S haystacks are not sorted by date,
// although the README says they are) and flushes the index.
func (c *ChandaServer) IngestQuestion(ctx context.Context, q *Question) (*Ingested, error) {
	in := &Ingested{EventSessions: map[string][]int{}, RejectReasons: map[string]int{}}
	start := time.Now()
	for _, i := range ChronologicalOrder(q) {
		sess := q.HaystackSessions[i]
		ts, err := RFC3339(q.HaystackDates[i])
		if err != nil {
			return nil, err
		}
		turns := make([]chanda.Turn, len(sess))
		for j, t := range sess {
			turns[j] = chanda.Turn{Role: t.Role, Content: t.Content, TS: ts}
		}
		var out chanda.LogSessionOutput
		if err := c.Call(ctx, chanda.ToolLogSession, chanda.LogSessionInput{SessionID: ChandaSessionID(i), Turns: turns}, &out); err != nil {
			return nil, fmt.Errorf("session %d: %w", i, err)
		}
		for _, tr := range out.Turns {
			in.Turns++
			switch tr.Action {
			case chanda.ActionCreated:
				in.Created++
			case chanda.ActionDuplicate:
				in.Duplicates++
			case chanda.ActionNearDuplicate:
				in.NearDups++
			case "rejected":
				in.Rejected++
				in.RejectReasons[tr.Reason]++
			}
			if tr.ID != "" && !containsInt(in.EventSessions[tr.ID], i) {
				in.EventSessions[tr.ID] = append(in.EventSessions[tr.ID], i)
			}
		}
	}
	fs := time.Now()
	var f chanda.FlushOutput
	if err := c.Call(ctx, chanda.ToolFlush, chanda.FlushInput{}, &f); err != nil {
		return nil, err
	}
	if !f.Complete {
		return nil, fmt.Errorf("flush incomplete: %+v", f)
	}
	in.FlushMS = ms(time.Since(fs))
	in.Elapsed = time.Since(start)
	return in, nil
}

// ChronologicalOrder returns the haystack indexes sorted by session date,
// ties in haystack order.
func ChronologicalOrder(q *Question) []int {
	idx := make([]int, len(q.HaystackSessions))
	ts := make([]time.Time, len(idx))
	for i := range idx {
		idx[i] = i
		ts[i], _ = ParseDate(q.HaystackDates[i])
	}
	sort.SliceStable(idx, func(a, b int) bool { return ts[idx[a]].Before(ts[idx[b]]) })
	return idx
}

func containsInt(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ItemSessions maps a delivered item to haystack session indexes: through
// its own id when it is an event, else through Origin.EventIDs (facts, pages).
func (in *Ingested) ItemSessions(it chanda.Item) []int {
	var out []int
	add := func(id string) {
		for _, s := range in.EventSessions[id] {
			if !containsInt(out, s) {
				out = append(out, s)
			}
		}
	}
	add(it.ID)
	for _, e := range it.Origin.EventIDs {
		add(e)
	}
	return out
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
