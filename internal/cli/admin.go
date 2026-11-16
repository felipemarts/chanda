package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/app"
	"github.com/felipemarts/chanda-mcp/internal/sleep"
	"github.com/felipemarts/chanda-mcp/pkg/chanda"
)

// gater is the review surface of the gate set and of learned ranking,
// implemented by *app.App (not MCP tools: only a person at the terminal
// may approve gate questions or switch learning).
type gater interface {
	GateList(ctx context.Context, namespace, status string) ([]app.GateQuestion, error)
	GateReview(ctx context.Context, in app.GateReviewInput) (app.GateReviewOutput, error)
	GateAdd(ctx context.Context, in app.GateAddInput) (app.GateQuestion, chanda.Meta, error)
	RankingToggle(ctx context.Context, namespace string, enabled bool) (chanda.Meta, error)
}

func asGater(api chanda.API) (gater, error) {
	g, ok := api.(gater)
	if !ok {
		return nil, chanda.Errf(chanda.CodeUnavailable, "this core has no gate set or ranking review")
	}
	return g, nil
}

// gateListOutput wraps the list so the envelope carries an object.
type gateListOutput struct {
	chanda.Meta
	Questions []app.GateQuestion `json:"questions"`
}

type gateAddOutput struct {
	chanda.Meta
	Question app.GateQuestion `json:"question"`
}

type rankingOutput struct {
	chanda.Meta
	Namespace string `json:"namespace"`
	Enabled   bool   `json:"enabled"`
}

const gateUsage = `usage: chanda gate list [--status proposed|approved|retired]
       chanda gate approve <id>
       chanda gate retire <id>
       chanda gate add --question q [--answer a]... [--record id]... [--as-of date]
(common flags: --config f --namespace ns --actor name)`

// cmdGate reviews the gate set (decision 0092): sleep proposes questions,
// only a person approves or retires them, or adds their own.
func (cl CLI) cmdGate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, gateUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	var c common
	fs := newFlags("gate "+sub, stderr, &c)
	var status, question, asOf string
	var answers, records listFlag
	switch sub {
	case "list":
		fs.StringVar(&status, "status", "", "proposed, approved or retired (default all)")
	case "add":
		fs.StringVar(&question, "question", "", "the question, as a user would ask it")
		fs.Var(&answers, "answer", "an expected answer substring (repeatable)")
		fs.Var(&records, "record", "an expected record id (repeatable)")
		fs.StringVar(&asOf, "as-of", "", "reference date of the question")
	case "approve", "retire":
	default:
		fmt.Fprintln(stderr, gateUsage)
		return 2
	}
	if code := parse(fs, rest); code >= 0 {
		return code
	}
	wantArgs := 0
	if sub == "approve" || sub == "retire" {
		wantArgs = 1
	}
	if fs.NArg() != wantArgs || (sub == "add" && strings.TrimSpace(question) == "") {
		fmt.Fprintln(stderr, gateUsage)
		return 2
	}
	return c.withAPI(ctx, cl.NewAPI, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		g, err := asGater(api)
		if err != nil {
			return nil, err
		}
		switch sub {
		case "list":
			qs, err := g.GateList(ctx, ns, status)
			return gateListOutput{Questions: qs}, err
		case "add":
			q, meta, err := g.GateAdd(ctx, app.GateAddInput{Namespace: ns, Question: question,
				Expected: sleep.GateExpected{RecordIDs: records, Answers: answers, AsOf: asOf}})
			return gateAddOutput{Meta: meta, Question: q}, err
		default:
			to := app.GateApproved
			if sub == "retire" {
				to = app.GateRetired
			}
			return g.GateReview(ctx, app.GateReviewInput{Namespace: ns, ID: fs.Arg(0), Status: to})
		}
	})
}

// cmdRanking switches learned ranking for a namespace (human only).
func (cl CLI) cmdRanking(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	usage := "usage: chanda ranking on|off [--config f] [--namespace ns]"
	if len(args) == 0 || (args[0] != "on" && args[0] != "off") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	on := args[0] == "on"
	var c common
	fs := newFlags("ranking", stderr, &c)
	if code := parse(fs, args[1:]); code >= 0 {
		return code
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	return c.withAPI(ctx, cl.NewAPI, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		g, err := asGater(api)
		if err != nil {
			return nil, err
		}
		meta, err := g.RankingToggle(ctx, ns, on)
		return rankingOutput{Meta: meta, Namespace: ns, Enabled: on}, err
	})
}

// cmdKeys is the learned_keys admin tool at the terminal (decision 0145):
// chanda keys enable|disable|reset|rebuild|stats. Every mutating action is
// audited under the --actor given (default the configured one).
func (cl CLI) cmdKeys(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	usage := "usage: chanda keys " + strings.Join(chanda.KeysActions(), "|") + " [--config f] [--namespace ns] [--actor name]"
	if len(args) == 0 || !slices.Contains(chanda.KeysActions(), args[0]) {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	action := args[0]
	var c common
	fs := newFlags("keys "+action, stderr, &c)
	if code := parse(fs, args[1:]); code >= 0 {
		return code
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	return c.withAPI(ctx, cl.NewAPI, stdout, stderr, func(ctx context.Context, api chanda.API, ns string) (any, error) {
		return api.LearnedKeys(ctx, chanda.LearnedKeysInput{Namespace: ns, Action: action})
	})
}

// listFlag collects a repeatable string flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }
