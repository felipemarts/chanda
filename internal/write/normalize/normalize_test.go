package normalize

import "testing"

// BASE.md write step 1: "Unicode NFC, collapsed whitespace, standardized line
// breaks. SHA-256 is computed over the normalized text."
func TestNormalize_Step1(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"only whitespace", " \t\r\n \n ", ""},
		{"NFC composes decomposed accents", "São Paulo é", "São Paulo é"},
		{"CRLF becomes LF", "a\r\nb", "a\nb"},
		{"lone CR becomes LF", "a\rb", "a\nb"},
		{"runs of spaces and tabs collapse", "a  \t b\t\tc", "a b c"},
		{"NBSP is whitespace", "a  b", "a b"},
		{"lines are trimmed", "  a  \n\t b \t", "a\nb"},
		{"paragraph break kept", "a\n\nb", "a\n\nb"},
		{"many blank lines collapse to one", "a\n\n\n\n\nb", "a\n\nb"},
		{"whitespace-only lines count as blank", "a\n  \n\t\n \nb", "a\n\nb"},
		{"leading and trailing blank lines removed", "\n\n a \n\n", "a"},
		{"invalid utf8 replaced", "a\xffb", "a�b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.in)
			if got != tc.want {
				t.Fatalf("Text(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if again := Text(got); again != got {
				t.Fatalf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestNormalize_HashOverNormalizedText(t *testing.T) {
	a := Hash(Text("São  Paulo\r\n"))
	b := Hash(Text("São Paulo"))
	if a != b {
		t.Fatalf("equivalent inputs hash differently: %s %s", a, b)
	}
	if got := Hash(""); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("Hash(\"\") = %s", got)
	}
	if len(a) != 64 {
		t.Fatalf("hash length %d", len(a))
	}
}
