package write

import (
	"strings"
	"testing"

	"github.com/felipemarts/chanda-mcp/internal/config"
	"github.com/felipemarts/chanda-mcp/internal/core"
)

func newAdmitter(t *testing.T, mut func(*config.WriteConfig)) *Admitter {
	t.Helper()
	cfg := config.Default().Write
	if mut != nil {
		mut(&cfg)
	}
	a, err := NewAdmitter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// BASE.md step 3: "Rules reject content that is empty, too short or over the size limit."
func TestAdmit_RejectsEmptyShortAndOversized(t *testing.T) {
	a := newAdmitter(t, func(c *config.WriteConfig) { c.MinChars = 5; c.MaxChars = 20 })
	tests := []struct {
		name, in, reason string
	}{
		{"empty", "", ReasonEmpty},
		{"whitespace only is empty after normalization", " \n\t\r\n ", ReasonEmpty},
		{"too short", "abcd", ReasonTooShort},
		{"too short after collapsing whitespace", "ab    cd", ""},
		{"min length accepted", "abcde", ""},
		{"max length accepted", strings.Repeat("x", 20), ""},
		{"over the limit", strings.Repeat("x", 21), ReasonTooLong},
		{"length counts runes, not bytes", strings.Repeat("ç", 20), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.Admit(tc.in, core.ZoneUser)
			if tc.reason == "" {
				if err != nil {
					t.Fatalf("unexpected rejection: %v", err)
				}
				return
			}
			if core.CodeOf(err) != core.CodeRejected || RejectReason(err) != tc.reason {
				t.Fatalf("got %v, want rejected/%s", err, tc.reason)
			}
		})
	}
}

// BASE.md step 3: "Secrets (keys, tokens) are masked by regular expression."
func TestAdmit_MasksSecrets(t *testing.T) {
	a := newAdmitter(t, func(c *config.WriteConfig) { c.SecretPatterns = []string{`\bACME-[0-9]{6}\b`} })
	tests := []struct {
		name, in, want string
	}{
		{"aws access key", "key AKIAIOSFODNN7EXAMPLE here", "key [REDACTED] here"},
		{"github classic token", "ghp_" + strings.Repeat("a1B2", 9), "[REDACTED]"},
		{"github fine-grained token", "use github_pat_11ABCDEFG0123456789_abcdefXYZ now", "use [REDACTED] now"},
		{"openai style key", "OPENAI sk-proj-abcdefghijklmnop1234", "OPENAI [REDACTED]"},
		{"anthropic style key", "sk-ant-api03-abcdefghijklmnopqrstu", "[REDACTED]"},
		{"bearer token keeps scheme", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345", "Authorization: Bearer [REDACTED]"},
		{"jwt", "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "token [REDACTED]"},
		{"private key block", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\nabc\n-----END RSA PRIVATE KEY-----\nafter", "[REDACTED]\nafter"},
		{"password assignment keeps key", "password=hunter2 ok", "password=[REDACTED] ok"},
		{"api_key colon with quotes", `api_key: "abc 123"`, "api_key: [REDACTED]"},
		{"json style key", `{"apiKey": "zzz"}`, `{"apiKey": [REDACTED]}`},
		{"portuguese senha", "senha: segredo123", "senha: [REDACTED]"},
		{"configured pattern", "ticket ACME-123456 open", "ticket [REDACTED] open"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := a.Admit(tc.in, core.ZoneInternal)
			if err != nil {
				t.Fatal(err)
			}
			if got.Content != tc.want {
				t.Fatalf("content = %q, want %q", got.Content, tc.want)
			}
			if !has(got.Flags, core.FlagSecretsMasked) {
				t.Fatalf("flags = %v, want secrets_masked", got.Flags)
			}
			// Masking is idempotent: admitting the masked text masks nothing new.
			again, _ := a.Admit(got.Content, core.ZoneInternal)
			if again.Content != got.Content || has(again.Flags, core.FlagSecretsMasked) {
				t.Fatalf("not idempotent: %q flags %v", again.Content, again.Flags)
			}
		})
	}
}

func TestAdmit_NoSecretsNoMask(t *testing.T) {
	a := newAdmitter(t, nil)
	for _, in := range []string{
		"The password policy requires twelve characters.",
		"Bearer tokens are described in RFC 6750.",
		"Ask for the sk- prefix in the docs.",
		"A senha deve ter doze caracteres.",
	} {
		got, err := a.Admit(in, core.ZoneInternal)
		if err != nil {
			t.Fatal(err)
		}
		if got.Content != in || len(got.Flags) != 0 {
			t.Errorf("%q -> %q flags %v", in, got.Content, got.Flags)
		}
	}
}

func TestAdmit_HashIsOverMaskedNormalizedContent(t *testing.T) {
	a := newAdmitter(t, nil)
	x, _ := a.Admit("password=one   value", core.ZoneUser)
	y, _ := a.Admit("password=two value\r\n", core.ZoneUser)
	if x.Content != y.Content || x.Hash != y.Hash {
		t.Fatalf("masked equal texts differ: %+v %+v", x, y)
	}
}

func TestNewAdmitter_InvalidPattern(t *testing.T) {
	cfg := config.Default().Write
	cfg.SecretPatterns = []string{"("}
	if _, err := NewAdmitter(cfg); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("got %v", err)
	}
	cfg = config.Default().Write
	cfg.InstructionPatterns = []string{"["}
	if _, err := NewAdmitter(cfg); core.CodeOf(err) != core.CodeInvalidArgument {
		t.Fatalf("got %v", err)
	}
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
