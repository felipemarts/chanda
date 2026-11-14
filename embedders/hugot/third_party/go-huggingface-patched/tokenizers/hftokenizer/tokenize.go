package hftokenizer

import (
	"github.com/gomlx/go-huggingface/tokenizers/api"
	"math"
	"strings"
)

// spanForSubword returns the original text span [origStart, origEnd] for a subword
// spanning bytes [startByte:endByte] within word.text.
func (word *wordWithOffset) spanForSubword(startByte, endByte int) api.TokenSpan {
	if len(word.offsets) == len(word.text) && endByte <= len(word.offsets) && startByte < endByte {
		origStart := word.offsets[startByte]
		origEnd := word.offsets[endByte-1] + 1
		return api.TokenSpan{Start: origStart, End: origEnd}
	}
	return api.TokenSpan{
		Start: word.start + startByte,
		End:   word.start + endByte,
	}
}

// tokenizeWordWithSpans tokenizes a single word and returns IDs with their offsets.
func (t *Tokenizer) tokenizeWordWithSpans(word wordWithOffset) ([]int, []api.TokenSpan) {
	// First check if word is an added token
	if id, ok := t.addedTokens[word.text]; ok {
		return []int{id}, []api.TokenSpan{{Start: word.start, End: word.end}}
	}

	switch t.tokenizer.Model.Type {
	case "WordPiece":
		return t.wordPieceTokenizeWithSpans(word)
	case "BPE":
		return t.bpeTokenizeWithSpans(word)
	case "Unigram":
		return t.unigramTokenizeWithSpans(word)
	default:
		// Fallback: try to find word in vocab
		if id, ok := t.tokenizer.Model.Vocab[word.text]; ok {
			return []int{id}, []api.TokenSpan{{Start: word.start, End: word.end}}
		}
		if t.unkID >= 0 {
			return []int{t.unkID}, []api.TokenSpan{{Start: word.start, End: word.end}}
		}
		return nil, nil
	}
}

// wordPieceTokenizeWithSpans implements WordPiece tokenization with offset tracking.
func (t *Tokenizer) wordPieceTokenizeWithSpans(word wordWithOffset) ([]int, []api.TokenSpan) {
	text := word.text
	if text == "" {
		return nil, nil
	}

	maxChars := t.tokenizer.Model.MaxInputCharsPerWord
	if maxChars == 0 {
		maxChars = 100
	}
	if len(text) > maxChars {
		if t.unkID >= 0 {
			return []int{t.unkID}, []api.TokenSpan{{Start: word.start, End: word.end}}
		}
		return nil, nil
	}

	prefix := t.tokenizer.Model.ContinuingSubwordPrefix
	if prefix == "" {
		prefix = "##"
	}

	var ids []int
	var offsets []api.TokenSpan
	runes := []rune(text)
	start := 0
	charLen := len(runes)

	for start < charLen {
		end := charLen
		found := false

		for start < end {
			substr := string(runes[start:end])
			if start > 0 {
				substr = prefix + substr
			}

			if id, ok := t.tokenizer.Model.Vocab[substr]; ok {
				ids = append(ids, id)

				// Calculate character offsets for this subword
				// Map from rune position to byte position within the word
				startByte := len(string(runes[:start]))
				endByte := len(string(runes[:end]))

				offsets = append(offsets, word.spanForSubword(startByte, endByte))
				found = true
				break
			}
			end--
		}

		if !found {
			if t.unkID >= 0 {
				return []int{t.unkID}, []api.TokenSpan{{Start: word.start, End: word.end}}
			}
			return nil, nil
		}
		start = end
	}

	return ids, offsets
}

// bpeTokenizeWithSpans implements BPE tokenization with offset tracking.
func (t *Tokenizer) bpeTokenizeWithSpans(word wordWithOffset) ([]int, []api.TokenSpan) {
	text := word.text
	if text == "" {
		return nil, nil
	}

	// Convert word to list of symbols with their character positions (rune indices)
	type symbolWithPos struct {
		text  string
		start int // rune position in word
		end   int // rune position in word
	}

	runes := []rune(text)
	symbols := make([]symbolWithPos, len(runes))
	for i, r := range runes {
		symbols[i] = symbolWithPos{
			text:  string(r),
			start: i,
			end:   i + 1,
		}
	}

	// Add end-of-word suffix if configured
	if t.tokenizer.Model.EndOfWordSuffix != "" && len(symbols) > 0 {
		symbols[len(symbols)-1].text += t.tokenizer.Model.EndOfWordSuffix
	}

	// If word is a single symbol that exists in vocab, return it
	if len(symbols) == 1 {
		if id, ok := t.tokenizer.Model.Vocab[symbols[0].text]; ok {
			return []int{id}, []api.TokenSpan{{Start: word.start, End: word.end}}
		}
	}

	// Apply BPE merges
	for len(symbols) > 1 {
		// Find best pair to merge
		bestPair := ""
		bestRank := -1
		bestIdx := -1

		for i := 0; i < len(symbols)-1; i++ {
			pair := symbols[i].text + " " + symbols[i+1].text
			if rank, ok := t.mergeRanks[pair]; ok {
				if bestRank == -1 || rank < bestRank {
					bestPair = pair
					bestRank = rank
					bestIdx = i
				}
			}
		}

		if bestIdx == -1 {
			break // No more merges possible
		}

		// Apply the merge
		merged := strings.Replace(bestPair, " ", "", 1)
		newSymbols := make([]symbolWithPos, 0, len(symbols)-1)
		newSymbols = append(newSymbols, symbols[:bestIdx]...)
		newSymbols = append(newSymbols, symbolWithPos{
			text:  merged,
			start: symbols[bestIdx].start,
			end:   symbols[bestIdx+1].end,
		})
		newSymbols = append(newSymbols, symbols[bestIdx+2:]...)
		symbols = newSymbols
	}

	// Convert symbols to IDs with offsets
	var ids []int
	var offsets []api.TokenSpan

	for _, sym := range symbols {
		if id, ok := t.tokenizer.Model.Vocab[sym.text]; ok {
			ids = append(ids, id)
		} else if t.unkID >= 0 {
			ids = append(ids, t.unkID)
		} else {
			continue
		}

		// Calculate offsets - map from rune position to byte position
		startByte := len(string(runes[:sym.start]))
		endByte := len(string(runes[:sym.end]))

		offsets = append(offsets, word.spanForSubword(startByte, endByte))
	}

	return ids, offsets
}

// unigramTokenizeWithSpans implements Unigram tokenization (Viterbi over log-prob scores,
// as SentencePiece / HF tokenizers do). Patched: upstream used greedy longest-match.
func (t *Tokenizer) unigramTokenizeWithSpans(word wordWithOffset) ([]int, []api.TokenSpan) {
	text := word.text
	if text == "" {
		return nil, nil
	}
	runes := []rune(text)
	n := len(runes)
	// byte offset of each rune boundary
	boff := make([]int, n+1)
	for i := range runes {
		boff[i+1] = boff[i] + len(string(runes[i]))
	}
	const maxPiece = 32
	negInf := math.Inf(-1)
	best := make([]float64, n+1)
	prev := make([]int, n+1)
	prevID := make([]int, n+1)
	for i := 1; i <= n; i++ {
		best[i] = negInf
	}
	minScore := 0.0
	for _, sc := range t.tokenizer.Model.Scores {
		if sc < minScore {
			minScore = sc
		}
	}
	unkScore := minScore - 10 // same convention as sentencepiece (kUnkPenalty = 10)
	for end := 1; end <= n; end++ {
		for start := end - 1; start >= 0 && end-start <= maxPiece; start-- {
			if best[start] == negInf {
				continue
			}
			id, ok := t.tokenizer.Model.Vocab[string(runes[start:end])]
			sc := 0.0
			if ok {
				if id < len(t.tokenizer.Model.Scores) {
					sc = t.tokenizer.Model.Scores[id]
				}
			} else if end-start == 1 && t.unkID >= 0 {
				id, sc = t.unkID, unkScore
			} else {
				continue
			}
			if cand := best[start] + sc; cand > best[end] {
				best[end], prev[end], prevID[end] = cand, start, id
			}
		}
	}
	var ids []int
	var offsets []api.TokenSpan
	for end := n; end > 0; end = prev[end] {
		start := prev[end]
		ids = append(ids, prevID[end])
		offsets = append(offsets, word.spanForSubword(boff[start], boff[end]))
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
		offsets[i], offsets[j] = offsets[j], offsets[i]
	}
	return ids, offsets
}
