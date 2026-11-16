// Command chanda-agent-example shows, in Go, the three moments an agent
// runtime owes Chanda (BASE.md "Why MCP alone is not enough"):
//
//  1. before the first turn, call context with the user's request and put the
//     returned block in front of the first prompt;
//  2. during the task, let the agent call search, get and timeline;
//  3. on finishing, call log_session with the turns and outcome with the
//     retrieval id, the task value and the ids the agent used.
//
// It spawns `chanda serve --profiles read,standard` over stdio with the
// official go-sdk client, runs a stand-in agent that only echoes, and prints
// a JSON report on stdout. When Chanda is unavailable the agent still runs,
// without a pre-context. Nothing is retried.
//
//	go run ./adapters/generic/go --chanda ./chanda --request "When is the Orion deadline?"
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipemarts/chanda/pkg/chanda"
)

// Memory is the agent's handle on Chanda during a task. It records every
// call and the ids the agent opened with Get.
type Memory struct {
	cs     *mcp.ClientSession
	ns     string
	Calls  []Call
	opened map[string]bool
}

// Call is one tool call as the adapter saw it.
type Call struct {
	Tool string `json:"tool"`
	OK   bool   `json:"ok"`
	Code string `json:"code,omitempty"`
}

// envelope is chanda.Envelope with data left raw for typed decoding.
type envelope struct {
	OK      bool              `json:"ok"`
	Data    json.RawMessage   `json:"data"`
	Notices []string          `json:"notices"`
	AuditID int64             `json:"audit_id,omitempty"`
	Error   *chanda.ErrorBody `json:"error,omitempty"`
}

// codeTransport marks failures that produced no envelope (spawn, protocol).
const codeTransport chanda.Code = "transport"

// call sends one tool call and decodes data into out. A failed envelope
// becomes a *chanda.Error carrying its stable code.
func (m *Memory) call(ctx context.Context, tool string, in, out any) error {
	if m == nil || m.cs == nil {
		return chanda.Errf(codeTransport, "%s: chanda is not connected", tool)
	}
	args := map[string]any{}
	b, err := json.Marshal(in)
	if err == nil {
		err = json.Unmarshal(b, &args)
	}
	if err != nil {
		return err
	}
	if m.ns != "" {
		args["namespace"] = m.ns
	}
	res, err := m.cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		m.Calls = append(m.Calls, Call{Tool: tool, Code: string(codeTransport)})
		return chanda.Errf(codeTransport, "%s: %v", tool, err)
	}
	var env envelope
	if err := decodeEnvelope(res, &env); err != nil {
		m.Calls = append(m.Calls, Call{Tool: tool, Code: string(codeTransport)})
		return chanda.Errf(codeTransport, "%s: %v", tool, err)
	}
	if !env.OK {
		code := chanda.CodeInternal
		msg := "envelope not ok"
		if env.Error != nil {
			code, msg = env.Error.Code, env.Error.Message
		}
		m.Calls = append(m.Calls, Call{Tool: tool, Code: string(code)})
		return chanda.Errf(code, "%s: %s", tool, msg)
	}
	m.Calls = append(m.Calls, Call{Tool: tool, OK: true})
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// decodeEnvelope reads the envelope from structuredContent, else content[0].
func decodeEnvelope(res *mcp.CallToolResult, env *envelope) error {
	if res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil && json.Unmarshal(b, env) == nil && (env.OK || env.Error != nil) {
			return nil
		}
	}
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok && json.Unmarshal([]byte(t.Text), env) == nil && (env.OK || env.Error != nil) {
			return nil
		}
	}
	return errors.New("the answer is not a Chanda envelope")
}

// Search runs a ranked search.
func (m *Memory) Search(ctx context.Context, query string) (chanda.SearchOutput, error) {
	var out chanda.SearchOutput
	err := m.call(ctx, chanda.ToolSearch, chanda.SearchInput{Query: query}, &out)
	return out, err
}

// Get opens one record; it counts as used.
func (m *Memory) Get(ctx context.Context, id string) (chanda.GetOutput, error) {
	var out chanda.GetOutput
	err := m.call(ctx, chanda.ToolGet, chanda.GetInput{ID: id}, &out)
	if err == nil {
		m.opened[id] = true
	}
	return out, err
}

// Timeline returns the history of a subject.
func (m *Memory) Timeline(ctx context.Context, subject, predicate string) (chanda.TimelineOutput, error) {
	var out chanda.TimelineOutput
	err := m.call(ctx, chanda.ToolTimeline, chanda.TimelineInput{Subject: subject, Predicate: predicate}, &out)
	return out, err
}

// Agent is any agent loop: it gets the first prompt and the memory handle,
// and returns its transcript and an outcome value in [-1, 1] (nil: none).
type Agent func(ctx context.Context, prompt string, mem *Memory) (turns []chanda.Turn, value *float64, err error)

// Report is what RunTask did.
type Report struct {
	Prompt       string   `json:"prompt"`
	Injected     bool     `json:"injected"`
	RetrievalID  string   `json:"retrieval_id,omitempty"`
	ContextError string   `json:"context_error,omitempty"`
	LogError     string   `json:"log_error,omitempty"`
	UsedIDs      []string `json:"used_ids"`
	OutcomeError string   `json:"outcome_error,omitempty"`
	Calls        []Call   `json:"calls"`
}

// RunTask wraps agent with the three moments. cs may be nil (Chanda could not
// be reached): the agent then runs with the bare request.
func RunTask(ctx context.Context, cs *mcp.ClientSession, ns string, agent Agent, request, sessionID string, budget int, log *slog.Logger) (Report, error) {
	mem := &Memory{cs: cs, ns: ns, opened: map[string]bool{}}
	rep := Report{Prompt: request, UsedIDs: []string{}}

	// 1. Before the first turn.
	var pre chanda.ContextOutput
	if err := mem.call(ctx, chanda.ToolContext, chanda.ContextInput{Query: request, BudgetTokens: budget, SessionID: sessionID}, &pre); err != nil {
		rep.ContextError = string(chanda.CodeOf(err))
		log.Warn("chanda context failed; running without pre-context", "code", rep.ContextError, "err", err)
	} else {
		rep.RetrievalID = pre.RetrievalID
		if pre.Text != "" {
			rep.Prompt = pre.Text + "\n\n" + request
			rep.Injected = true
		}
	}

	// 2. During the task.
	turns, value, err := agent(ctx, rep.Prompt, mem)
	if err != nil {
		rep.Calls = mem.Calls
		return rep, err
	}

	// 3. On finishing. The injected block is not logged back as session content.
	if cs != nil {
		for i := range turns {
			if turns[i].Role == "user" && rep.Injected {
				turns[i].Content = strings.TrimSpace(strings.ReplaceAll(turns[i].Content, pre.Text, ""))
			}
		}
		if err := mem.call(ctx, chanda.ToolLogSession, chanda.LogSessionInput{SessionID: sessionID, Turns: turns}, nil); err != nil {
			rep.LogError = string(chanda.CodeOf(err))
			log.Warn("chanda log_session failed", "code", rep.LogError, "err", err)
		}
		if rep.RetrievalID != "" && value != nil {
			rep.UsedIDs = usedIDs(pre.Items, mem.opened, turns)
			in := chanda.OutcomeInput{RetrievalID: rep.RetrievalID, Value: *value, UsedIDs: rep.UsedIDs}
			if err := mem.call(ctx, chanda.ToolOutcome, in, nil); err != nil {
				rep.OutcomeError = string(chanda.CodeOf(err))
				log.Warn("chanda outcome failed", "code", rep.OutcomeError, "err", err)
			}
		}
	}
	rep.Calls = mem.Calls
	return rep, nil
}

// usedIDs returns, sorted, the delivered ids the agent opened or cited in an
// assistant turn.
func usedIDs(items []chanda.Item, opened map[string]bool, turns []chanda.Turn) []string {
	out := []string{}
	for _, it := range items {
		used := opened[it.ID]
		for _, t := range turns {
			if t.Role == "assistant" && strings.Contains(t.Content, it.ID) {
				used = true
			}
		}
		if used && !slices.Contains(out, it.ID) {
			out = append(out, it.ID)
		}
	}
	slices.Sort(out)
	return out
}

// echoAgent stands in for a real model: it answers with what it received.
func echoAgent(value *float64) Agent {
	return func(ctx context.Context, prompt string, mem *Memory) ([]chanda.Turn, *float64, error) {
		answer := fmt.Sprintf("(example agent) received a prompt of %d characters", len(prompt))
		return []chanda.Turn{{Role: "user", Content: prompt}, {Role: "assistant", Content: answer}}, value, nil
	}
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("chanda-agent-example", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bin := fs.String("chanda", "chanda", "chanda binary")
	cfg := fs.String("config", "", "chanda.yaml passed to chanda serve")
	ns := fs.String("namespace", "", "namespace (default: the server's)")
	request := fs.String("request", "", "the user's request")
	session := fs.String("session", "example-session", "session id")
	budget := fs.Int("budget", 0, "context token budget (0: server default)")
	valueFlag := fs.String("value", "", "outcome value in [-1, 1]; empty sends no outcome")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *request == "" {
		fmt.Fprintln(stderr, "chanda-agent-example: --request is required")
		return 2
	}
	var value *float64
	if *valueFlag != "" {
		v, err := strconv.ParseFloat(*valueFlag, 64)
		if err != nil || v < -1 || v > 1 {
			fmt.Fprintln(stderr, "chanda-agent-example: --value must be a number in [-1, 1]")
			return 2
		}
		value = &v
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))

	serveArgs := []string{"serve", "--profiles", "read,standard"}
	if *cfg != "" {
		serveArgs = append(serveArgs, "--config", *cfg)
	}
	cmd := exec.Command(*bin, serveArgs...)
	cmd.Stderr = stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "chanda-agent-example", Version: "0.1.0"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		log.Warn("chanda unavailable; running without memory", "err", err)
		cs = nil
	} else {
		defer cs.Close()
	}
	rep, err := RunTask(ctx, cs, *ns, echoAgent(value), *request, *session, *budget, log)
	if err != nil {
		fmt.Fprintln(stderr, "chanda-agent-example:", err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return 1
	}
	return 0
}
