package search

import (
	"unicode/utf8"

	"github.com/usememos/memos/store"
)

// ChunkerVersion is the current chunker version. Bump it when the chunking
// rules change; chunks embedded under another version belong to another
// generation fingerprint. Version 2 derives chunk boundaries from the
// search-document projection spans; version 1 was the scaffold's
// one-chunk-per-document rule.
const ChunkerVersion int32 = 2

const (
	// chunkTargetContentBytes is the target projected-content bytes per
	// chunk: a few hundred tokens, so multi-paragraph memos embed as several
	// focused chunks while short memos stay one chunk.
	chunkTargetContentBytes = 1536
	// maxChunksPerDocument bounds the chunks one memo contributes to a
	// generation. A document that would exceed the bound at the target size
	// re-chunks at a larger deterministic size, so oversized memos are
	// chunked, never silently truncated.
	maxChunksPerDocument = 64
)

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

// chunkDocument derives the embedding chunks of one search document from its
// projection spans: consecutive spans pack into chunks up to the chunk size,
// and a span larger than the chunk size splits at rune boundaries. The
// boundaries come from the projection spans instead of raw text, so every
// chunk maps back to source positions through the same span mapping lexical
// retrieval uses. Chunking is deterministic — the same projection version
// and content always produce identical chunk ordinals and spans, keeping the
// idempotent upsert key (generation, memo, source revision, chunk ordinal)
// stable — and total: the chunk texts always concatenate to the full
// projected content.
func chunkDocument(document *store.AISearchDocument) []documentChunk {
	content := document.Content
	if content == "" {
		return nil
	}
	sizeBytes := chunkContentSize(len(content))
	for {
		chunks := packDocumentChunks(document, sizeBytes)
		if len(chunks) <= maxChunksPerDocument {
			return chunks
		}
		// Too many undersized chunks: grow the chunk size by the overshoot
		// factor and repack. The size strictly grows and content packed at a
		// size reaching the content length is one chunk, so this terminates.
		sizeBytes = (sizeBytes*len(chunks) + maxChunksPerDocument - 1) / maxChunksPerDocument
	}
}

// chunkContentSize returns the deterministic chunk size for a document of
// the given projected length: the target, raised as needed so the document
// can pack into the per-document chunk bound.
func chunkContentSize(contentBytes int) int {
	return max(chunkTargetContentBytes, (contentBytes+maxChunksPerDocument-1)/maxChunksPerDocument)
}

// packDocumentChunks packs the document content into chunks of at most
// sizeBytes projected bytes each. The atomic packing units are the
// projection spans and the uncovered gaps between them; a unit longer than
// sizeBytes splits at a rune boundary first.
func packDocumentChunks(document *store.AISearchDocument, sizeBytes int) []documentChunk {
	pieces := contentPieces(document, sizeBytes)
	chunks := []documentChunk{}
	start := 0
	for _, piece := range pieces {
		if piece.end-start > sizeBytes {
			chunks = append(chunks, newDocumentChunk(int32(len(chunks)), document, start, piece.start))
			start = piece.start
		}
	}
	if start < len(document.Content) {
		chunks = append(chunks, newDocumentChunk(int32(len(chunks)), document, start, len(document.Content)))
	}
	return chunks
}

// contentPiece is one atomic packing unit: a byte range of the projected
// content that is either one projection span's range or one uncovered gap,
// never longer than the chunk size.
type contentPiece struct {
	start int
	end   int
}

// contentPieces splits the document content into the atomic packing units in
// order. The units cover the content exactly: each projection span's content
// range is one unit, each gap between spans (separator runs or source-less
// text such as autolinks) is one unit, and a unit longer than sizeBytes
// splits at rune boundaries.
func contentPieces(document *store.AISearchDocument, sizeBytes int) []contentPiece {
	content := document.Content
	unsplit := []contentPiece{}
	pos := 0
	for _, span := range document.Spans {
		if span.ContentStart < pos || span.ContentStart >= span.ContentEnd || span.ContentEnd > len(content) {
			// The projection emits spans sorted and sane; skip anything else
			// rather than corrupt the unit order.
			continue
		}
		if span.ContentStart > pos {
			unsplit = append(unsplit, contentPiece{start: pos, end: span.ContentStart})
		}
		unsplit = append(unsplit, contentPiece{start: span.ContentStart, end: span.ContentEnd})
		pos = span.ContentEnd
	}
	if pos < len(content) {
		unsplit = append(unsplit, contentPiece{start: pos, end: len(content)})
	}

	pieces := []contentPiece{}
	for _, unit := range unsplit {
		for unit.end-unit.start > sizeBytes {
			cut := unit.start + sizeBytes
			for cut > unit.start && !utf8.RuneStart(content[cut]) {
				cut--
			}
			if cut == unit.start {
				// A single rune exceeds the chunk size: it stays whole.
				_, width := utf8.DecodeRuneInString(content[unit.start:])
				cut = unit.start + width
			}
			pieces = append(pieces, contentPiece{start: unit.start, end: cut})
			unit.start = cut
		}
		pieces = append(pieces, unit)
	}
	return pieces
}

// newDocumentChunk builds one chunk over a content byte range, mapping the
// range back to source bytes through the document's span mapping — the same
// mechanism lexical retrieval uses to reach source positions. When an
// uncovered gap covers a chunk edge, the edge maps to the nearest covered
// position inside the chunk; a chunk with no covered position at all maps to
// the source origin.
func newDocumentChunk(ordinal int32, document *store.AISearchDocument, contentStart, contentEnd int) documentChunk {
	sourceStart := 0
	for pos := contentStart; pos < contentEnd; pos++ {
		if mapped := mapContentPosition(document.Spans, pos); mapped >= 0 {
			sourceStart = mapped
			break
		}
	}
	sourceEnd := sourceStart
	for pos := contentEnd - 1; pos >= contentStart; pos-- {
		if mapped := mapContentPosition(document.Spans, pos); mapped >= 0 {
			sourceEnd = mapped + 1
			break
		}
	}
	return documentChunk{
		ordinal:      ordinal,
		contentStart: int32(contentStart),
		contentEnd:   int32(contentEnd),
		sourceStart:  int32(sourceStart),
		sourceEnd:    int32(sourceEnd),
		text:         document.Content[contentStart:contentEnd],
	}
}
