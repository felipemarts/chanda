package write

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/felipemarts/chanda/internal/write/normalize"
)

type ch struct{ text, path string }

func chunksOf(cs []Chunk) []ch {
	out := make([]ch, len(cs))
	for i, c := range cs {
		out[i] = ch{c.Text, c.HeadingPath}
	}
	return out
}

// BASE.md step 6: "The document is split by structure (headings and
// paragraphs, no overlap)."
func TestChunkText_SplitsByStructure(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []ch
	}{
		{"plain paragraphs", "one\ntwo\n\nthree", []ch{{"one\ntwo", ""}, {"three", ""}}},
		{"heading path", "# A\n\npara a\n\n## B\n\npara b\n\n### C\npara c\n\n## D\n\npara d",
			[]ch{{"para a", "A"}, {"para b", "A > B"}, {"para c", "A > B > C"}, {"para d", "A > D"}}},
		{"new h1 resets path", "# A\n## B\n\nx\n\n# E\n\ny", []ch{{"x", "A > B"}, {"y", "E"}}},
		{"skipped level", "# A\n### C\n\nx", []ch{{"x", "A > C"}}},
		{"closing hashes trimmed", "## Title ##\n\nx", []ch{{"x", "Title"}}},
		{"hash without space is text", "#hashtag line", []ch{{"#hashtag line", ""}}},
		{"fenced code is one paragraph", "# A\n\n```go\nx := 1\n\n# not a heading\n```\n\nafter",
			[]ch{{"```go\nx := 1\n\n# not a heading\n```", "A"}, {"after", "A"}}},
		{"only headings", "# Title\n## Sub", []ch{{"# Title\n## Sub", ""}}},
		{"empty", "", []ch{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chunksOf(ChunkText(normalize.Text(tc.in), 2000))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

// Long paragraphs are split at sentence boundaries, never above the limit,
// with no overlap and no loss of words.
func TestChunkText_SplitsLongParagraphsAtSentences(t *testing.T) {
	s1 := "First sentence is here."
	s2 := "Second one follows now!"
	s3 := "Does a third exist?"
	got := chunksOf(ChunkText(s1+" "+s2+" "+s3, 50))
	want := []ch{{s1 + " " + s2, ""}, {s3, ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}

	long := strings.Repeat("palavra ", 100) + "fim."
	cs := ChunkText(normalize.Text(long), 60)
	var words []string
	for _, c := range cs {
		if n := utf8.RuneCountInString(c.Text); n > 60 {
			t.Fatalf("chunk over limit: %d", n)
		}
		words = append(words, strings.Fields(c.Text)...)
	}
	if strings.Join(words, " ") != normalize.Text(long) {
		t.Fatal("hard split lost or duplicated words")
	}
}

func TestChunkText_HashIsOverChunkText(t *testing.T) {
	for _, c := range ChunkText("# A\n\nalpha\n\nbeta", 2000) {
		if c.Hash != normalize.Hash(c.Text) || normalize.Text(c.Text) != c.Text {
			t.Fatalf("chunk %+v", c)
		}
	}
}

func TestFrontMatter_Date(t *testing.T) {
	tests := []struct {
		name, in, date, body string
	}{
		{"date", "---\ntitle: X\ndate: 2025-04-02\n---\n# H\n\nbody", "2025-04-02T00:00:00.000000000Z", "# H\n\nbody"},
		{"quoted rfc3339", "---\ndate: \"2025-04-02T10:00:00Z\"\n...\nbody", "2025-04-02T10:00:00.000000000Z", "body"},
		{"no date", "---\ntitle: X\n---\nbody", "", "body"},
		{"bad date ignored", "---\ndate: someday\n---\nbody", "", "body"},
		{"zone key ignored", "---\ntrust_zone: user\n---\nbody", "", "body"},
		{"unterminated is not front matter", "---\ndate: 2025-01-01\nbody", "", "---\ndate: 2025-01-01\nbody"},
		{"no front matter", "body", "", "body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, b := frontMatter(tc.in)
			if d != tc.date || b != tc.body {
				t.Fatalf("got %q %q", d, b)
			}
		})
	}
}
