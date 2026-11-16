package main

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/mcp"
	"github.com/felipemarts/chanda/pkg/chanda"
)

const block = "<memory>\n- [fct_A] Orion deadline is 2026-12-01\n</memory>"

// wiredAPI answers the calls the adapter makes; everything else is unavailable.
type wiredAPI struct {
	chanda.Unwired
	calls    []string
	logged   chanda.LogSessionInput
	outcome  chanda.OutcomeInput
	noMemory bool
}

func (w *wiredAPI) Context(_ context.Context, in chanda.ContextInput) (chanda.ContextOutput, error) {
	w.calls = append(w.calls, "context")
	if w.noMemory {
		return chanda.ContextOutput{RetrievalID: "ret_1", Items: []chanda.Item{}, Reason: chanda.ReasonNoRelevantMemory}, nil
	}
	return chanda.ContextOutput{RetrievalID: "ret_1", Text: block,
		Items: []chanda.Item{{ID: "fct_A", Type: chanda.RecFact}, {ID: "evt_B", Type: chanda.RecEvent}}}, nil
}

func (w *wiredAPI) Get(_ context.Context, in chanda.GetInput) (chanda.GetOutput, error) {
	w.calls = append(w.calls, "get")
	return chanda.GetOutput{Item: chanda.Item{ID: in.ID}, Provenance: []chanda.Edge{}}, nil
}

func (w *wiredAPI) LogSession(_ context.Context, in chanda.LogSessionInput) (chanda.LogSessionOutput, error) {
	w.calls = append(w.calls, "log_session")
	w.logged = in
	return chanda.LogSessionOutput{SessionID: in.SessionID, Turns: []chanda.TurnResult{}}, nil
}

func (w *wiredAPI) Outcome(_ context.Context, in chanda.OutcomeInput) (chanda.OutcomeOutput, error) {
	w.calls = append(w.calls, "outcome")
	w.outcome = in
	return chanda.OutcomeOutput{OutcomeID: "out_1"}, nil
}

// connect serves api over in-memory transports with the read and standard profiles.
func connect(t *testing.T, api chanda.API) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv, err := mcp.New(api, config.Default(), mcp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := sdk.NewInMemoryTransports()
	ss, err := srv.SDK().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestPreContextBeforeFirstTurn(t *testing.T) {
	api := &wiredAPI{}
	cs := connect(t, api)
	var seenCalls []string
	var seenPrompt string
	one := 1.0
	agent := func(ctx context.Context, prompt string, mem *Memory) ([]chanda.Turn, *float64, error) {
		seenCalls = slices.Clone(api.calls)
		seenPrompt = prompt
		if _, err := mem.Get(ctx, "evt_B"); err != nil {
			return nil, nil, err
		}
		return []chanda.Turn{{Role: "user", Content: prompt}, {Role: "assistant", Content: "per fct_A, December 1"}}, &one, nil
	}
	rep, err := RunTask(context.Background(), cs, "", agent, "When is the deadline?", "s-1", 0, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seenCalls, []string{"context"}) {
		t.Fatalf("calls before the first turn = %v, want [context]", seenCalls)
	}
	if !rep.Injected || seenPrompt != block+"\n\nWhen is the deadline?" {
		t.Fatalf("prompt = %q (injected %v)", seenPrompt, rep.Injected)
	}
	if want := []string{"context", "get", "log_session", "outcome"}; !slices.Equal(api.calls, want) {
		t.Fatalf("calls = %v, want %v", api.calls, want)
	}
	if api.logged.Turns[0].Content != "When is the deadline?" {
		t.Fatalf("logged first turn %q still carries the block", api.logged.Turns[0].Content)
	}
	if api.outcome.RetrievalID != "ret_1" || api.outcome.Value != 1 || !slices.Equal(api.outcome.UsedIDs, []string{"evt_B", "fct_A"}) {
		t.Fatalf("outcome = %+v", api.outcome)
	}
}

func TestNoRelevantMemoryInjectsNothing(t *testing.T) {
	api := &wiredAPI{noMemory: true}
	cs := connect(t, api)
	rep, err := RunTask(context.Background(), cs, "", echoAgent(nil), "hello there", "s-1", 0, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Injected || rep.Prompt != "hello there" {
		t.Fatalf("report = %+v", rep)
	}
	if want := []string{"context", "log_session"}; !slices.Equal(api.calls, want) { // value nil: no outcome
		t.Fatalf("calls = %v, want %v", api.calls, want)
	}
}

func TestUnwiredCoreRunsAgentWithoutInjection(t *testing.T) {
	cs := connect(t, chanda.Unwired{})
	one := 1.0
	rep, err := RunTask(context.Background(), cs, "", echoAgent(&one), "hello there", "s-1", 0, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Injected || rep.Prompt != "hello there" || rep.ContextError != string(chanda.CodeUnavailable) {
		t.Fatalf("report = %+v", rep)
	}
	want := []Call{{Tool: "context", Code: "unavailable"}, {Tool: "log_session", Code: "unavailable"}}
	if !slices.Equal(rep.Calls, want) {
		t.Fatalf("calls = %+v, want %+v", rep.Calls, want)
	}
}

func TestNoServerStillRunsAgent(t *testing.T) {
	rep, err := RunTask(context.Background(), nil, "", echoAgent(nil), "hello there", "s-1", 0, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Prompt != "hello there" || rep.ContextError != string(codeTransport) || len(rep.Calls) != 0 {
		t.Fatalf("report = %+v", rep)
	}
}
