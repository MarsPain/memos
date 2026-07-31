package search

import (
	"unicode/utf8"

	"github.com/usememos/memos/store"
)

// ChunkerVersion is the current chunker version. Bump it when the chunking
// rules change; chunks embedded under another version belong to another
// generation fingerprint.
const ChunkerVersion int32 = 1

// scaffoldChunkContentCap bounds the projected content bytes one chunk
// embeds. Scaffold A: one chunk per search document holding the whole body
// up to this cap.
// TODO(issue 02): replace with multi-chunk, span-mapped chunking.
const scaffoldChunkContentCap = 4096

// documentChunk is one chunk of a search document: the projection span it
// embeds and the source span it maps back to through the document's span
// mapping.
type documentChunk struct {
	ordinal      int32
	contentStart int32
	contentEnd   int32
	sourceStart  int32
	sourceEnd    int32
	text         string
}

// chunkDocument derives the embedding chunks of one search document.
// Scaffold A emits exactly one chunk covering the projected body up to
// scaffoldChunkContentCap, cut at a rune boundary, with the source span
// mapped through the same span mapping lexical retrieval uses.
// TODO(issue 02): replace Scaffold A with span-derived multi-chunking.
func chunkDocument(document *store.AISearchDocument) []documentChunk {
	content := document.Content
	if content == "" {
		return nil
	}
	end := len(content)
	if end > scaffoldChunkContentCap {
		end = scaffoldChunkContentCap
		for end > 0 && !utf8.RuneStart(content[end]) {
			end--
		}
	}

	sourceStart := mapContentPosition(document.Spans, 0)
	if sourceStart < 0 {
		sourceStart = 0
	}
	sourceEnd := sourceStart
	if end > 0 {
		if mapped := mapContentPosition(document.Spans, end-1); mapped >= 0 {
			sourceEnd = mapped + 1
		}
	}

	return []documentChunk{{
		ordinal:      0,
		contentStart: 0,
		contentEnd:   int32(end),
		sourceStart:  int32(sourceStart),
		sourceEnd:    int32(sourceEnd),
		text:         content[:end],
	}}
}
