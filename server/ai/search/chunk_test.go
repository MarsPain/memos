package search

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/store"
)

// requireChunksCoverContent asserts the chunk invariants every chunked
// document must keep: sequential ordinals, contiguous content ranges, valid
// UTF-8 texts, and exact coverage — the chunk texts concatenate to the full
// projected content, so nothing is ever silently truncated.
func requireChunksCoverContent(t *testing.T, document *store.AISearchDocument, chunks []documentChunk) {
	t.Helper()
	require.NotEmpty(t, chunks)
	var concatenated strings.Builder
	pos := 0
	for i, chunk := range chunks {
		require.Equal(t, int32(i), chunk.ordinal, "chunk %d ordinal", i)
		require.Equal(t, pos, int(chunk.contentStart), "chunk %d starts where chunk %d ended", i, i-1)
		require.Less(t, int(chunk.contentStart), int(chunk.contentEnd), "chunk %d is non-empty", i)
		require.LessOrEqual(t, int(chunk.contentEnd), len(document.Content), "chunk %d ends inside the content", i)
		require.Equal(t, document.Content[chunk.contentStart:chunk.contentEnd], chunk.text, "chunk %d text is its content range", i)
		require.True(t, utf8.ValidString(chunk.text), "chunk %d is valid UTF-8", i)
		concatenated.WriteString(chunk.text)
		pos = int(chunk.contentEnd)
	}
	require.Equal(t, document.Content, concatenated.String(), "chunks cover the content exactly")
}

func TestChunkDocumentEmptyContent(t *testing.T) {
	require.Nil(t, chunkDocument(&store.AISearchDocument{}))
}

func TestChunkDocumentShortDocument(t *testing.T) {
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

func TestChunkDocumentPacksProjectionSpans(t *testing.T) {
	// Spans smaller than the chunk size pack into one chunk; the chunk's
	// source span reaches from the first span's source start to the last
	// span's source end.
	document := &store.AISearchDocument{
		Content: "alpha beta gamma delta",
		Spans: []*store.AISearchDocumentSpan{
			{ContentStart: 0, ContentEnd: 5, SourceStart: 10, SourceEnd: 15},
			{ContentStart: 6, ContentEnd: 10, SourceStart: 20, SourceEnd: 24},
			{ContentStart: 11, ContentEnd: 16, SourceStart: 30, SourceEnd: 35},
			{ContentStart: 17, ContentEnd: 22, SourceStart: 40, SourceEnd: 45},
		},
	}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 1)
	require.Equal(t, int32(10), chunks[0].sourceStart)
	require.Equal(t, int32(45), chunks[0].sourceEnd)
	requireChunksCoverContent(t, document, chunks)
}

func TestChunkDocumentBreaksAtSpanBoundaries(t *testing.T) {
	// Two spans that do not pack into one chunk split between spans, not
	// mid-span: every chunk starts at a projection span boundary.
	content := strings.Repeat("a", 1000) + " " + strings.Repeat("b", 1000)
	document := &store.AISearchDocument{
		Content: content,
		Spans: []*store.AISearchDocumentSpan{
			{ContentStart: 0, ContentEnd: 1000, SourceStart: 100, SourceEnd: 1100},
			{ContentStart: 1001, ContentEnd: 2001, SourceStart: 2000, SourceEnd: 3000},
		},
	}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 2)
	require.LessOrEqual(t, len(chunks[0].text), chunkTargetContentBytes)
	require.Equal(t, int32(1001), chunks[1].contentStart, "the second chunk starts at the second span")
	require.Equal(t, int32(2000), chunks[1].sourceStart)
	require.Equal(t, int32(3000), chunks[1].sourceEnd)
	requireChunksCoverContent(t, document, chunks)
}

func TestChunkDocumentSplitsOversizedSpanAtRuneBoundary(t *testing.T) {
	// A span larger than the chunk size (for example one giant code block)
	// splits at rune boundaries; every split chunk stays within the span's
	// source range and the source positions do not go backwards.
	content := strings.Repeat("ab世", 800) // 5 bytes per repetition, 4000 bytes.
	document := &store.AISearchDocument{
		Content: content,
		Spans: []*store.AISearchDocumentSpan{
			{ContentStart: 0, ContentEnd: len(content), SourceStart: 1000, SourceEnd: 9000},
		},
	}

	chunks := chunkDocument(document)
	require.Greater(t, len(chunks), 1)
	previousSourceStart := int32(-1)
	for _, chunk := range chunks {
		require.LessOrEqual(t, len(chunk.text), chunkTargetContentBytes)
		require.GreaterOrEqual(t, chunk.sourceStart, int32(1000))
		require.LessOrEqual(t, chunk.sourceEnd, int32(9000))
		require.GreaterOrEqual(t, chunk.sourceStart, previousSourceStart)
		previousSourceStart = chunk.sourceStart
	}
	requireChunksCoverContent(t, document, chunks)
}

func TestChunkDocumentExactlyAtChunkTarget(t *testing.T) {
	content := strings.Repeat("y", chunkTargetContentBytes)
	document := &store.AISearchDocument{
		Content: content,
		Spans:   []*store.AISearchDocumentSpan{{ContentStart: 0, ContentEnd: len(content), SourceStart: 0, SourceEnd: len(content)}},
	}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 1)
	require.Equal(t, chunkTargetContentBytes, len(chunks[0].text))
}

func TestChunkDocumentExactlyAtChunkCountCap(t *testing.T) {
	content := strings.Repeat("y", chunkTargetContentBytes*maxChunksPerDocument)
	document := &store.AISearchDocument{
		Content: content,
		Spans:   []*store.AISearchDocumentSpan{{ContentStart: 0, ContentEnd: len(content), SourceStart: 0, SourceEnd: len(content)}},
	}

	chunks := chunkDocument(document)
	require.Len(t, chunks, maxChunksPerDocument)
	for _, chunk := range chunks {
		require.Equal(t, chunkTargetContentBytes, len(chunk.text))
	}
	requireChunksCoverContent(t, document, chunks)
}

func TestChunkDocumentOversizedMemoNeverTruncates(t *testing.T) {
	// A memo beyond the count cap at the target size re-chunks at a larger
	// deterministic size: bounded chunk count, full coverage, no silent
	// truncation.
	content := strings.Repeat("x", chunkTargetContentBytes*maxChunksPerDocument+12345)
	document := &store.AISearchDocument{
		Content: content,
		Spans:   []*store.AISearchDocumentSpan{{ContentStart: 0, ContentEnd: len(content), SourceStart: 0, SourceEnd: len(content)}},
	}

	chunks := chunkDocument(document)
	require.LessOrEqual(t, len(chunks), maxChunksPerDocument)
	require.Greater(t, len(chunks), 1)
	requireChunksCoverContent(t, document, chunks)
}

func TestChunkDocumentMapsChunkEdgesThroughGaps(t *testing.T) {
	// Content ranges no span covers (separator runs or source-less text such
	// as autolinks) take no part in the source mapping: a chunk's source span
	// comes from the nearest covered positions inside the chunk.
	content := "aaaa " + strings.Repeat("m", 2000) + " bbbb"
	document := &store.AISearchDocument{
		Content: content,
		Spans: []*store.AISearchDocumentSpan{
			{ContentStart: 0, ContentEnd: 4, SourceStart: 0, SourceEnd: 4},
			{ContentStart: 2005, ContentEnd: 2009, SourceStart: 500, SourceEnd: 504},
		},
	}

	chunks := chunkDocument(document)
	requireChunksCoverContent(t, document, chunks)
	last := chunks[len(chunks)-1]
	require.Equal(t, int32(500), last.sourceStart, "a chunk starting in an uncovered gap maps to the next covered position")
	require.Equal(t, int32(504), last.sourceEnd)
}

func TestChunkDocumentWithoutSpans(t *testing.T) {
	document := &store.AISearchDocument{Content: "no source mapping"}

	chunks := chunkDocument(document)
	require.Len(t, chunks, 1)
	require.Equal(t, int32(0), chunks[0].sourceStart)
	require.Equal(t, int32(0), chunks[0].sourceEnd)
}

func TestChunkDocumentIsDeterministic(t *testing.T) {
	// The same projection version and content always produce identical chunk
	// ordinals and spans, keeping the idempotent upsert key stable.
	content := strings.Repeat("ab世", 1000) + " tail"
	document := &store.AISearchDocument{
		Content: content,
		Spans: []*store.AISearchDocumentSpan{
			{ContentStart: 0, ContentEnd: 2500, SourceStart: 0, SourceEnd: 2500},
			{ContentStart: 2501, ContentEnd: len(content), SourceStart: 3000, SourceEnd: 3000 + len(content) - 2501},
		},
	}

	first := chunkDocument(document)
	second := chunkDocument(document)
	require.Equal(t, first, second)
	requireChunksCoverContent(t, document, first)
}
