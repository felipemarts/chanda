package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"unicode/utf8"
)

// Validate checks doc against a JSON Schema restricted to the subset sleep
// uses: type, properties, required, additionalProperties (boolean),
// items, enum, minItems, maxItems, minLength, maxLength, minimum, maximum.
// Annotation keywords (title, description, $schema) are ignored. Any other
// keyword is a schema error, so a schema never validates more weakly than it
// reads. Errors wrap ErrInvalidOutput when doc does not conform.
func Validate(schema, doc []byte) error {
	var s any
	if err := decodeStrict(schema, &s); err != nil {
		return fmt.Errorf("llm: schema is not JSON: %v", err)
	}
	var d any
	if err := decodeStrict(doc, &d); err != nil {
		return fmt.Errorf("%w: not a single JSON value: %v", ErrInvalidOutput, err)
	}
	return validate(s, d, "$")
}

func decodeStrict(b []byte, v *any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after JSON value")
	}
	return nil
}

var annotations = map[string]bool{"title": true, "description": true, "$schema": true}

func validate(schema, v any, path string) error {
	s, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("llm: schema at %s is not an object", path)
	}
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic error messages
	for _, k := range keys {
		switch k {
		case "type", "properties", "required", "additionalProperties", "items", "enum",
			"minItems", "maxItems", "minLength", "maxLength", "minimum", "maximum":
		default:
			if !annotations[k] {
				return fmt.Errorf("llm: unsupported schema keyword %q at %s", k, path)
			}
		}
	}
	if t, ok := s["type"]; ok {
		if err := checkType(t, v, path); err != nil {
			return err
		}
	}
	if e, ok := s["enum"]; ok {
		list, ok := e.([]any)
		if !ok {
			return fmt.Errorf("llm: enum at %s is not an array", path)
		}
		found := false
		for _, x := range list {
			if jsonEqual(x, v) {
				found = true
				break
			}
		}
		if !found {
			return invalid(path, "value not in enum")
		}
	}
	switch x := v.(type) {
	case map[string]any:
		return validateObject(s, x, path)
	case []any:
		if err := checkCount(s, "minItems", "maxItems", len(x), path); err != nil {
			return err
		}
		if it, ok := s["items"]; ok {
			for i, el := range x {
				if err := validate(it, el, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case string:
		if err := checkCount(s, "minLength", "maxLength", utf8.RuneCountInString(x), path); err != nil {
			return err
		}
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return invalid(path, "bad number")
		}
		if m, ok := num(s["minimum"]); ok && f < m {
			return invalid(path, "below minimum")
		}
		if m, ok := num(s["maximum"]); ok && f > m {
			return invalid(path, "above maximum")
		}
	}
	return nil
}

func validateObject(s map[string]any, x map[string]any, path string) error {
	props, _ := s["properties"].(map[string]any)
	if req, ok := s["required"].([]any); ok {
		for _, r := range req {
			name, _ := r.(string)
			if _, ok := x[name]; !ok {
				return invalid(path, fmt.Sprintf("missing required property %q", name))
			}
		}
	}
	names := make([]string, 0, len(x))
	for k := range x {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		ps, ok := props[k]
		if !ok {
			if ap, set := s["additionalProperties"]; set {
				if b, isBool := ap.(bool); isBool && !b {
					return invalid(path, fmt.Sprintf("unexpected property %q", k))
				}
			}
			continue
		}
		if err := validate(ps, x[k], path+"."+k); err != nil {
			return err
		}
	}
	return nil
}

func checkType(t, v any, path string) error {
	var types []string
	switch tt := t.(type) {
	case string:
		types = []string{tt}
	case []any:
		for _, x := range tt {
			if s, ok := x.(string); ok {
				types = append(types, s)
			}
		}
	default:
		return fmt.Errorf("llm: bad type keyword at %s", path)
	}
	for _, want := range types {
		if hasType(want, v) {
			return nil
		}
	}
	return invalid(path, fmt.Sprintf("expected type %v", types))
}

func hasType(want string, v any) bool {
	switch want {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		f, err := n.Float64()
		return err == nil && f == math.Trunc(f)
	}
	return false
}

func checkCount(s map[string]any, minKey, maxKey string, n int, path string) error {
	if m, ok := num(s[minKey]); ok && float64(n) < m {
		return invalid(path, minKey+" not met")
	}
	if m, ok := num(s[maxKey]); ok && float64(n) > m {
		return invalid(path, maxKey+" exceeded")
	}
	return nil
}

func num(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

func jsonEqual(a, b any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(ab, bb)
}

func invalid(path, msg string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidOutput, path, msg)
}
