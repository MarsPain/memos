package search

import (
	"strings"

	"github.com/usememos/memos/store"
)

const (
	// snippetContextRunes is the number of source runes kept around the
	// mapped match position.
	snippetContextRunes = 80
	// snippetFallbackRunes is the snippet length when no match position can
	// be mapped back to the source.
	snippetFallbackRunes = 160
)

// buildSourceSnippet quotes a window of the memo's current source content
// around the match and returns the window's byte range within the source.
// The anchor is a token of the normalized document content; its position
// maps back to a source byte range through the document's spans, and the
// snippet is cut from the source — never from the normalized projection.
// Within-chunk mapping is approximate when normalization changed byte
// lengths. When the anchor cannot be mapped (for example a title-only match,
// or an autolink chunk without a source range), the snippet falls back to
// the source's leading runes.
func buildSourceSnippet(document *store.AISearchDocument, source, anchor string) (string, int, int) {
	sourceRunes := []rune(source)
	if len(sourceRunes) == 0 {
		return "", 0, 0
	}

	sourcePos := -1
	if anchor != "" {
		if contentPos := strings.Index(document.Content, anchor); contentPos >= 0 {
			sourcePos = mapContentPosition(document.Spans, contentPos)
		}
	}
	if sourcePos < 0 || sourcePos >= len(source) {
		snippet, end := fallbackSnippet(sourceRunes)
		return snippet, 0, end
	}

	runePos := len([]rune(source[:sourcePos]))
	start := max(0, runePos-snippetContextRunes)
	end := min(len(sourceRunes), runePos+snippetContextRunes)
	var builder strings.Builder
	if start > 0 {
		builder.WriteString("…")
	}
	builder.WriteString(string(sourceRunes[start:end]))
	if end < len(sourceRunes) {
		builder.WriteString("…")
	}
	return builder.String(), len(string(sourceRunes[:start])), len(string(sourceRunes[:end]))
}

// mapContentPosition maps a byte position in the normalized document content
// back to the approximate byte position in the source content using the
// spans, or -1 when no span covers it. The projection builds spans in chunk
// order, so they are sorted by ContentStart and the scan can stop early.
func mapContentPosition(spans []*store.AISearchDocumentSpan, contentPos int) int {
	for _, span := range spans {
		if contentPos < span.ContentStart {
			break
		}
		if contentPos >= span.ContentEnd {
			continue
		}
		offset := contentPos - span.ContentStart
		spanLength := span.SourceEnd - span.SourceStart
		if offset >= spanLength {
			offset = max(spanLength-1, 0)
		}
		return span.SourceStart + offset
	}
	return -1
}

// fallbackSnippet quotes the source's leading runes and returns the byte end
// of the quoted window.
func fallbackSnippet(sourceRunes []rune) (string, int) {
	end := min(len(sourceRunes), snippetFallbackRunes)
	snippet := string(sourceRunes[:end])
	if end < len(sourceRunes) {
		snippet += "…"
	}
	return snippet, len(string(sourceRunes[:end]))
}
