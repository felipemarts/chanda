package llm_test

import (
	"testing"

	"github.com/felipemarts/chanda/internal/llm"
)

const verdictSchema = `{
  "type": "object",
  "required": ["verdicts"],
  "additionalProperties": false,
  "properties": {
    "verdicts": {
      "type": "array", "maxItems": 2,
      "items": {
        "type": "object",
        "required": ["id", "verdict", "score"],
        "additionalProperties": false,
        "properties": {
          "id": {"type": "string", "minLength": 1},
          "verdict": {"type": "string", "enum": ["supersede", "coexist", "reject", "keep_pending"]},
          "score": {"type": "integer", "minimum": 0, "maximum": 10}
        }
      }
    }
  }
}`

// BASE.md Sleep safeguard "Validated output": the LLM answers in JSON with a
// fixed schema; anything else is invalid.
func TestValidatedOutputFixedSchema(t *testing.T) {
	cases := []struct {
		name  string
		doc   string
		valid bool
	}{
		{"conforming", `{"verdicts":[{"id":"fct_1","verdict":"reject","score":3}]}`, true},
		{"empty array", `{"verdicts":[]}`, true},
		{"not json", `the answer is reject`, false},
		{"trailing data", `{"verdicts":[]} extra`, false},
		{"missing required", `{}`, false},
		{"extra property", `{"verdicts":[],"note":"x"}`, false},
		{"verdict outside closed set", `{"verdicts":[{"id":"fct_1","verdict":"delete","score":1}]}`, false},
		{"wrong type", `{"verdicts":"reject"}`, false},
		{"empty id", `{"verdicts":[{"id":"","verdict":"reject","score":1}]}`, false},
		{"non integer", `{"verdicts":[{"id":"a","verdict":"reject","score":1.5}]}`, false},
		{"above maximum", `{"verdicts":[{"id":"a","verdict":"reject","score":11}]}`, false},
		{"too many items", `{"verdicts":[{"id":"a","verdict":"reject","score":1},{"id":"b","verdict":"reject","score":1},{"id":"c","verdict":"reject","score":1}]}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := llm.Validate([]byte(verdictSchema), []byte(c.doc))
			if c.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !c.valid && !llm.IsInvalidOutput(err) {
				t.Fatalf("expected ErrInvalidOutput, got %v", err)
			}
		})
	}
}

func TestValidateRejectsUnsupportedSchemaKeyword(t *testing.T) {
	err := llm.Validate([]byte(`{"type":"string","pattern":"^a"}`), []byte(`"a"`))
	if err == nil || llm.IsInvalidOutput(err) {
		t.Fatalf("unsupported keyword must be a schema error, got %v", err)
	}
}
