package norm

import "testing"

func TestAliasNormalization_LowercaseNoAccentsNoPunctuation(t *testing.T) {
	cases := map[string]string{
		"José  O'Brien-Silva": "jose o brien silva",
		"ACME, Inc.":          "acme inc",
		"  São   Paulo ":      "sao paulo",
		"Café":               "cafe", // decomposed accent
		"Ünïcödé":             "unicode",
		"...":                 "",
	}
	for in, want := range cases {
		if got := Alias(in); got != want {
			t.Errorf("Alias(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPredicateNormalization_SnakeCase(t *testing.T) {
	cases := map[string]string{
		"Job Title":      "job_title",
		"jobTitle":       "job_title",
		"job-title":      "job_title",
		"  JOB_TITLE  ":  "job_title",
		"Endereço":       "endereco",
		"current price!": "current_price",
		"phone2Number":   "phone2_number",
	}
	for in, want := range cases {
		if got := Predicate(in); got != want {
			t.Errorf("Predicate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNumberRule(t *testing.T) {
	cases := map[string]string{
		"1.000,50":    "1000.5",
		"1,000.50":    "1000.5",
		"1000.50":     "1000.5",
		"1000,50":     "1000.5",
		"1.000":       "1000",
		"1,000":       "1000",
		"1.000.000":   "1000000",
		"1.000.000,5": "1000000.5",
		"10,5":        "10.5",
		"10.0":        "10",
		"0,500":       "0.5",
		"1234.567":    "1234.567",
		"1.2.3":       "1.2.3",
		"10.0.0.1":    "10.0.0.1",
		"1,5.3":       "1,5.3",
		"007":         "007",
		"42":          "42",
	}
	for in, want := range cases {
		if got := Number(in); got != want {
			t.Errorf("Number(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDateRule_ISOWhereUnambiguous(t *testing.T) {
	cases := map[string]string{
		"2026-03-01":      "2026-03-01",
		"2026/3/1":        "2026-03-01",
		"2026.03.01":      "2026-03-01",
		"25/12/2026":      "2026-12-25",
		"12/25/2026":      "2026-12-25",
		"10/10/2026":      "2026-10-10",
		"01/03/2026":      "01/03/2026", // ambiguous
		"31/02/2026":      "31/02/2026", // invalid
		"since 1/31/2026": "since 2026-01-31",
	}
	for in, want := range cases {
		if got := Dates(in); got != want {
			t.Errorf("Dates(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValueNormalization(t *testing.T) {
	pairs := [][2]string{
		{"  Senior   Engineer ", "senior engineer"},
		{"R$ 1.000,50", "r$ 1000.5"},
		{"r$ 1000.50", "r$ 1000.5"},
		{"25/12/2026", "2026-12-25"},
		{"2026-12-25", "2026-12-25"},
		{"Café", "café"}, // NFC, accents kept
	}
	for _, p := range pairs {
		if got := Value(p[0]); got != p[1] {
			t.Errorf("Value(%q) = %q, want %q", p[0], got, p[1])
		}
	}
	if Value("1.000,50") != Value("1000.50") {
		t.Error("1.000,50 and 1000.50 must normalize equally")
	}
}
