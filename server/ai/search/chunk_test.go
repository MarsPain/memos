package search

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/store"
)

func TestChunkDocumentSingleChunk(t *testing.T) {
	document := &store.AISearchDocument{
		Content: "the owl hunts at dusk",
		Spans: []*store.AISearchDocumentSpan{
			{ContentStart: 0, ContentEnd: 21, SourceStart: 15, SourceEnd: 36},
		},
	}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 1)
	chunk := chunks[0]
	require.Equal(t, int32(0), chunk.ordinal)
	require.Equal(t, int32(0), chunk.contentStart)
	require.Equal(t, int32(len(document.Content)), chunk.contentEnd)
	require.Equal(t, document.Content, chunk.text)
	require.Equal(t, int32(15), chunk.sourceStart)
	require.Equal(t, int32(36), chunk.sourceEnd)
}

func TestChunkDocumentCapsContent(t *testing.T) {
	content := strings.Repeat("a", scaffoldChunkContentCap+100)
	document := &store.AISearchDocument{Content: content}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 1)
	require.Equal(t, scaffoldChunkContentCap, len(chunks[0].text))
	require.True(t, utf8.ValidString(chunks[0].text))
}

func TestChunkDocumentCutsAtRuneBoundary(t *testing.T) {
	// A multi-byte rune straddling the cap must not be split.
	content := strings.Repeat("a", scaffoldChunkContentCap-1) + "世界"
	document := &store.AISearchDocument{Content: content}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 1)
	require.True(t, utf8.ValidString(chunks[0].text))
	require.Less(t, len(chunks[0].text), scaffoldChunkContentCap)
}

func TestChunkDocumentEmptyContent(t *testing.T) {
	require.Nil(t, chunkDocument(&store.AISearchDocument{}))
}

func TestChunkDocumentWithoutSpans(t *testing.T) {
	document := &store.AISearchDocument{Content: "no source mapping"}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 1)
	require.Equal(t, int32(0), chunks[0].sourceStart)
	require.Equal(t, int32(0), chunks[0].sourceEnd)
}
