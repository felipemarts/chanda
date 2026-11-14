package e2e

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/felipemarts/chanda-mcp/internal/core"
	"github.com/felipemarts/chanda-mcp/internal/llm"
)

var (
	reEvent = regexp.MustCompile(`(?s)<event id="([^"]+)"[^>]*>\n(.*?)\n</event>`)
	reClaim = regexp.MustCompile(`^(.+?)'s (\w+) is (.+?)\.?$`)
)

// scriptedLLM is sleep's phase-2 provider in the end-to-end tests: it
// extracts "<Subject>'s <attribute> is <value>." from each event, keeps
// judged items pending and skips lessons.
func scriptedLLM() *llm.Fake {
	return &llm.Fake{Handler: func(req core.LLMRequest) llm.FakeResponse {
		switch {
		case strings.Contains(req.System, "You extract facts"):
			type fact struct {
				EventIDs    []string `json:"event_ids"`
				Subject     string   `json:"subject"`
				SubjectType string   `json:"subject_type"`
				Predicate   string   `json:"predicate"`
				Value       string   `json:"value"`
				ValidFrom   string   `json:"valid_from"`
			}
			out := struct {
				Facts []fact `json:"facts"`
			}{Facts: []fact{}}
			for _, m := range reEvent.FindAllStringSubmatch(req.Prompt, -1) {
				if c := reClaim.FindStringSubmatch(strings.TrimSpace(m[2])); c != nil {
					out.Facts = append(out.Facts, fact{EventIDs: []string{m[1]}, Subject: c[1], Predicate: c[2], Value: c[3]})
				}
			}
			b, _ := json.Marshal(out)
			return llm.FakeResponse{JSON: string(b)}
		case strings.Contains(req.System, "lessons learned"):
			return llm.FakeResponse{JSON: `{"skip":true,"page_id":"","title":"","item":""}`}
		default:
			return llm.FakeResponse{JSON: `{"verdicts":[]}`}
		}
	}}
}
