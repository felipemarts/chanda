// Package llm implements core.LLMProvider, the only door through which an LLM
// touches Chanda (BASE.md "Sleep", principles 2 and 3: never on the read or
// synchronous write path).
//
// Every answer is JSON validated against the fixed schema of the request.
// An invalid answer is an error wrapping ErrInvalidOutput and is never
// retried automatically; the caller discards it. Token usage is returned
// even with that error, so the budget still counts what was spent.
package llm

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
)

// ErrInvalidOutput marks an answer that is not JSON or does not match the
// request schema. Callers test it with errors.Is.
var ErrInvalidOutput = errors.New("llm: invalid output")

// IsInvalidOutput reports whether err is an invalid-answer error (as opposed
// to a transport or configuration failure).
func IsInvalidOutput(err error) bool { return errors.Is(err, ErrInvalidOutput) }

// EstimateTokens is a deterministic, conservative token estimate (one token
// per four characters, rounded up) used to keep a call inside the budget
// before it is sent.
func EstimateTokens(s string) int { return (utf8.RuneCountInString(s) + 3) / 4 }

// FromConfig returns the configured provider, or nil (with no error) when
// cfg.Endpoint is empty: Chanda then runs without phase 2 of sleep.
// getenv reads the API key from cfg.APIKeyEnv (pass os.Getenv).
func FromConfig(cfg config.LLMConfig, getenv func(string) string) (core.LLMProvider, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, nil
	}
	p, err := NewOpenAICompatible(cfg, getenv)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func invalidOutput(detail string) error {
	return core.Wrap(core.CodeInvalidArgument, errors.Join(ErrInvalidOutput, errors.New(detail)), "llm answer discarded")
}
