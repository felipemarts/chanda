package hugot

import (
	"path/filepath"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
)

// clipper cuts text so that it fits the model window. hugot v0.8.6 sets the
// tokenizer's MaxLen to max_position_embeddings (514 for XLM-R, two more than
// the usable 512) and go-huggingface ignores MaxLen anyway, so a long text
// overflows the position table and the run fails. The text is cut at the end
// of its last token that fits, keeping room for <s> and </s>, which is HF's
// truncation of the tail (decision 0034).
type clipper struct {
	tok    *hftokenizer.Tokenizer // own instance: spans on, special tokens off
	budget int                    // content tokens, without <s> and </s>
}

func newClipper(dir string, maxTokens int) (*clipper, error) {
	tk, err := hftokenizer.NewFromFile(nil, filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	if err := tk.With(api.EncodeOptions{IncludeSpans: true}); err != nil {
		return nil, err
	}
	return &clipper{tok: tk, budget: maxTokens - 2}, nil
}

// clip returns the longest prefix of s, ending on a token boundary, whose
// tokenisation has at most budget tokens. Re-tokenising a cut word can
// change its pieces, so it loops until the prefix fits.
func (c *clipper) clip(s string) string {
	for {
		enc := c.tok.EncodeWithAnnotations(s)
		if len(enc.IDs) <= c.budget {
			return s
		}
		end := enc.Spans[c.budget-1].End
		if end <= 0 || end >= len(s) {
			end = enc.Spans[c.budget-1].Start
		}
		if end <= 0 || end >= len(s) {
			return s // cannot shorten on a token boundary; let the model report it
		}
		s = s[:end]
	}
}
