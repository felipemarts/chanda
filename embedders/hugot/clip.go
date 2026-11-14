package hugot

import (
	"path/filepath"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
)

// clipper cuts text so that it fits the model window. hugot v0.8.6 sets the
// tokenizer's MaxLen to max_position_embeddings (514 for XLM-R, two more than
// the usable 512) and go-huggingface ignores MaxLen anyway, so a long text
// overflows the position table and the run fails. The text is cut after its
// last whole word that fits, keeping room for <s> and </s> (decision 0034).
// Token spans are not used: go-huggingface v0.4.32 reports wrong spans for
// some tokenizer.json files (every span {0, 11} on MiniLM).
type clipper struct {
	tok    *hftokenizer.Tokenizer // own instance, special tokens off
	budget int                    // content tokens, without <s> and </s>
}

func newClipper(dir string, maxTokens int) (*clipper, error) {
	tk, err := hftokenizer.NewFromFile(nil, filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	if err := tk.With(api.EncodeOptions{}); err != nil {
		return nil, err
	}
	return &clipper{tok: tk, budget: maxTokens - 2}, nil
}

func (c *clipper) fits(s string) bool { return len(c.tok.Encode(s)) <= c.budget }

// clip returns s when it fits, else its longest prefix ending at a word end
// that fits (binary search: words tokenise independently, so the count grows
// with the prefix), else, for a first word longer than the window, its
// longest fitting prefix ending on a rune boundary.
func (c *clipper) clip(s string) string {
	if c.fits(s) {
		return s
	}
	var ends []int // byte offsets just after each word
	for i, r := range s {
		if !unicode.IsSpace(r) {
			j := i + utf8.RuneLen(r)
			if next, _ := utf8.DecodeRuneInString(s[j:]); j == len(s) || unicode.IsSpace(next) {
				ends = append(ends, j)
			}
		}
	}
	if k := sort.Search(len(ends), func(k int) bool { return !c.fits(s[:ends[k]]) }); k > 0 {
		return s[:ends[k-1]]
	}
	var cuts []int
	for i := range s {
		cuts = append(cuts, i)
	}
	k := sort.Search(len(cuts), func(k int) bool { return !c.fits(s[:cuts[k]]) })
	return s[:cuts[max(k-1, 0)]]
}
