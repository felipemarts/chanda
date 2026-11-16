package read

import "github.com/felipemarts/chanda/internal/window"

// Shortening a context item (decision 0137). read.context_truncate picks
// how: "head" keeps the start of the text (truncateToFit, the behaviour
// before the knob); "best_window" keeps the contiguous window of the text
// that best matches the query. Both serve the per-item cap
// (read.context_item_max_tokens) and the last item that only partly fits
// the budget.

// shortenItem shortens it.Text so its block costs at most target tokens,
// by head or by best window over the query terms. It refuses (ok=false)
// when fewer than minTokens remain or when nothing of the text would fit.
func shortenItem(it Item, target, minTokens int, bestWindow bool, terms []string) (Item, int, bool) {
	if !bestWindow {
		return truncateToFit(it, target, minTokens)
	}
	return bestWindowFit(it, target, minTokens, terms)
}

// bestWindowFit keeps the contiguous window of it.Text that best matches
// terms and whose block costs at most target tokens. The text is split into
// sentences; a sentence that cannot fit on its own is split into words, and
// a word that cannot fit on its own into fixed-size rune pieces (the
// window package, shared with the guided sleep). Among the longest windows
// that fit from each start, the one with the most distinct query terms
// wins, then the one with the most term occurrences, then the earliest. An
// ellipsis marks each end where text was cut, and the item is marked
// truncated. Deterministic: the score is lexical (the read path's own
// tokenizer and query terms) and the cost is estimateTokens of the
// rendered block, as the budget counts it.
func bestWindowFit(it Item, target, minTokens int, terms []string) (Item, int, bool) {
	if target < minTokens || target <= 0 {
		return it, 0, false
	}
	it.Truncated = true
	cost := func(text string) int {
		x := it
		x.Text = text
		return estimateTokens(renderItem(x))
	}
	text, c, ok := window.Best(it.Text, target, cost, tokenize, terms)
	if !ok {
		return it, 0, false
	}
	it.Text = text
	return it, c, true
}

// splitSentences and splitWords are the window package's splitters.
func splitSentences(text string) []string { return window.SplitSentences(text) }

func splitWords(s string) []string { return window.SplitWords(s) }
