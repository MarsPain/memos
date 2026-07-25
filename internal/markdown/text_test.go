package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// joinChunks concatenates chunks the way the search corpus projection does:
// a single space before each chunk that starts a new block.
func joinChunks(chunks []TextChunk) string {
	var b strings.Builder
	for _, chunk := range chunks {
		if b.Len() > 0 && chunk.Block {
			b.WriteByte(' ')
		}
		b.WriteString(chunk.Text)
	}
	return b.String()
}

func TestExtractText(t *testing.T) {
	svc := NewService(WithTagExtension())

	t.Run("heading and paragraph", func(t *testing.T) {
		content := "# My Title\n\nThis is the first paragraph.\n\nSecond **bold** paragraph."
		chunks, err := svc.ExtractText([]byte(content))
		require.NoError(t, err)

		assert.Equal(t, "My Title This is the first paragraph. Second bold paragraph.", joinChunks(chunks))
		blockCount := 0
		for _, chunk := range chunks {
			if chunk.Block {
				blockCount++
			}
		}
		assert.Equal(t, 3, blockCount, "heading and two paragraphs each start a block")
		assert.True(t, chunks[0].Block)
	})

	t.Run("code blocks are included", func(t *testing.T) {
		content := "before\n\n```go\nfmt.Println(\"hi\")\n```\n\nafter"
		chunks, err := svc.ExtractText([]byte(content))
		require.NoError(t, err)

		require.Len(t, chunks, 3)
		assert.Equal(t, "before", chunks[0].Text)
		assert.Equal(t, "fmt.Println(\"hi\")\n", chunks[1].Text)
		assert.True(t, chunks[1].Block)
		assert.Equal(t, "after", chunks[2].Text)
		assert.True(t, chunks[2].Block)
	})

	t.Run("soft line break separates chunks", func(t *testing.T) {
		content := "line one\nline two"
		chunks, err := svc.ExtractText([]byte(content))
		require.NoError(t, err)

		assert.Equal(t, "line one line two", joinChunks(chunks))
		blockCount := 0
		for _, chunk := range chunks {
			if chunk.Block {
				blockCount++
			}
		}
		assert.Equal(t, 2, blockCount, "each line starts a separated chunk run")
	})

	t.Run("links keep their text and autolinks their URL", func(t *testing.T) {
		content := "see [the docs](https://example.com/docs) and <https://example.com>"
		chunks, err := svc.ExtractText([]byte(content))
		require.NoError(t, err)

		assert.Equal(t, "see the docs and https://example.com", joinChunks(chunks))
		// The autolink chunk has no exact source range.
		last := chunks[len(chunks)-1]
		assert.Equal(t, "https://example.com", last.Text)
		assert.False(t, last.HasSource)
	})

	t.Run("source ranges round trip to source bytes", func(t *testing.T) {
		contents := []string{
			"# Title with #tag\n\nParagraph with `code span` and #another tag.",
			"1. first\n2. second\n\n- bullet\n\n> quote\n\n| a | b |\n|---|---|\n| 1 | 2 |",
			"    indented code\n",
		}
		for _, content := range contents {
			chunks, err := svc.ExtractText([]byte(content))
			require.NoError(t, err)
			for _, chunk := range chunks {
				if !chunk.HasSource {
					continue
				}
				require.GreaterOrEqual(t, chunk.SourceStart, 0)
				require.LessOrEqual(t, chunk.SourceEnd, len(content))
				require.Less(t, chunk.SourceStart, chunk.SourceEnd)
				assert.Equal(t, chunk.Text, content[chunk.SourceStart:chunk.SourceEnd],
					"chunk %q must map back to its source bytes", chunk.Text)
			}
		}
	})

	t.Run("tag chunk covers the full #tag source", func(t *testing.T) {
		content := "notes on #travel/plans today"
		chunks, err := svc.ExtractText([]byte(content))
		require.NoError(t, err)

		var tagChunk *TextChunk
		for i := range chunks {
			if chunks[i].Text == "#travel/plans" {
				tagChunk = &chunks[i]
			}
		}
		require.NotNil(t, tagChunk)
		require.True(t, tagChunk.HasSource)
		assert.Equal(t, "#travel/plans", content[tagChunk.SourceStart:tagChunk.SourceEnd])
	})

	t.Run("empty content yields no chunks", func(t *testing.T) {
		chunks, err := svc.ExtractText([]byte(""))
		require.NoError(t, err)
		assert.Empty(t, chunks)
	})
}
