package write

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/felipemarts/chanda/internal/config"
	"github.com/felipemarts/chanda/internal/core"
	"github.com/felipemarts/chanda/internal/write/normalize"
)

// Redacted replaces every masked secret.
const Redacted = "[REDACTED]"

// Rejection reasons (write step 3). They are the message of a core.CodeRejected error.
const (
	ReasonEmpty    = "empty"
	ReasonTooShort = "too_short"
	ReasonTooLong  = "too_long"
)

// Built-in secret patterns, applied in order. When a pattern has a group
// named "secret", only that group is replaced (so "password=x" keeps its key).
var builtinSecretPatterns = []string{
	`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`, // PEM/PGP private key blocks
	`(?i)\bbearer\s+(?P<secret>[A-Za-z0-9\-._~+/]{20,}=*)`,                                                   // Authorization: Bearer <token>
	`\beyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}`,                                        // JWT
	`\b(?:AKIA|ASIA|ABIA|ACCA|AGPA|AIDA|AIPA|AKPA|ANPA|ANVA|AROA|APKA)[A-Z0-9]{16}\b`,                        // AWS access key id
	`\b(?:gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{22,255})\b`,                                  // GitHub tokens
	`\bsk-(?:ant-|proj-|live-|test-)?[A-Za-z0-9_\-]{16,}`,                                                    // OpenAI / Anthropic / Stripe style keys
	`\bxox[abposr]-[A-Za-z0-9-]{10,}`,                                                                        // Slack tokens
	`\bAIza[0-9A-Za-z_\-]{35}\b`,                                                                             // Google API keys
	`(?i)\b(?:password|passwd|pwd|passphrase|secret|client[_-]?secret|api[_-]?key|apikey|access[_-]?key|secret[_-]?key|access[_-]?token|auth[_-]?token|refresh[_-]?token|private[_-]?key|aws_secret_access_key|senha)["']?\s*[:=]\s*(?P<secret>"[^"\n]+"|'[^'\n]+'|\[REDACTED\]|[^\s,;'"{}]+)`, // generic assignments
}

// Admitter applies the admission rules of write step 3. It is safe for
// concurrent use and holds only compiled configuration.
type Admitter struct {
	minChars, maxChars int
	secrets            []*regexp.Regexp
	instructions       []*regexp.Regexp // extra, from config
}

// NewAdmitter compiles the built-in rules plus cfg.SecretPatterns and
// cfg.InstructionPatterns. An invalid pattern is a CodeInvalidArgument error.
func NewAdmitter(cfg config.WriteConfig) (*Admitter, error) {
	a := &Admitter{minChars: cfg.MinChars, maxChars: cfg.MaxChars}
	for _, p := range append(append([]string{}, builtinSecretPatterns...), cfg.SecretPatterns...) {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, core.Wrap(core.CodeInvalidArgument, err, fmt.Sprintf("write.secret_patterns: %q", p))
		}
		a.secrets = append(a.secrets, re)
	}
	for _, p := range cfg.InstructionPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, core.Wrap(core.CodeInvalidArgument, err, fmt.Sprintf("write.instruction_patterns: %q", p))
		}
		a.instructions = append(a.instructions, re)
	}
	return a, nil
}

// Admitted is text that passed admission.
type Admitted struct {
	Content          string   // normalized, secrets masked
	Hash             string   // SHA-256 hex of Content
	Flags            []string // sorted core.Flag* values
	InstructionRules []string // names of matched instruction rules (empty unless zone is external)
}

// Admit runs write steps 1 and 3 over raw text destined for zone z: normalize,
// length rules, secret masking and, for the external zone only, the
// instruction-like mark. A refused text returns a core.CodeRejected error
// whose message is one of the Reason* constants.
func (a *Admitter) Admit(raw string, z core.Zone) (Admitted, error) {
	text := normalize.Text(raw)
	if r := a.lengthReason(text); r != "" {
		return Admitted{}, rejected(r)
	}
	return a.Screen(text, z), nil
}

// lengthReason returns the rejection reason for normalized text, or "".
func (a *Admitter) lengthReason(text string) string {
	n := utf8.RuneCountInString(text)
	switch {
	case n == 0:
		return ReasonEmpty
	case n < a.minChars:
		return ReasonTooShort
	case a.maxChars > 0 && n > a.maxChars:
		return ReasonTooLong
	}
	return ""
}

// Screen masks secrets in already-normalized text and, when z is external,
// sets the instruction-like mark. It applies no length rule (document chunks
// are admitted at document level).
func (a *Admitter) Screen(text string, z core.Zone) Admitted {
	masked := a.MaskSecrets(text)
	var flags []string
	if masked != text {
		flags = append(flags, core.FlagSecretsMasked)
	}
	var rules []string
	if z == core.ZoneExternal || !z.Valid() {
		rules = instructionRules(masked, a.instructions)
		if len(rules) > 0 {
			flags = append(flags, core.FlagInstructionLike)
		}
	}
	sort.Strings(flags)
	return Admitted{Content: masked, Hash: normalize.Hash(masked), Flags: flags, InstructionRules: rules}
}

// MaskSecrets replaces every match of the secret patterns with Redacted.
func (a *Admitter) MaskSecrets(text string) string {
	for _, re := range a.secrets {
		text = maskOne(re, text)
	}
	return text
}

func maskOne(re *regexp.Regexp, s string) string {
	g := re.SubexpIndex("secret")
	if g < 0 {
		return re.ReplaceAllLiteralString(s, Redacted)
	}
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[2*g], m[2*g+1]
		if start < 0 {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(Redacted)
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func rejected(reason string) error { return core.Errf(core.CodeRejected, "%s", reason) }

// RejectReason returns the admission reason carried by err, or "" if err is
// not an admission rejection.
func RejectReason(err error) string {
	var e *core.Error
	if errors.As(err, &e) && e.Code == core.CodeRejected {
		return e.Message
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
