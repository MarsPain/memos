package search

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"

	"github.com/pkg/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/usememos/memos/store"
)

const (
	// ProjectionVersion is the current corpus-projection version. Bump it when
	// the searchable-corpus rules or the markdown-to-text projection change;
	// documents built by another version are excludable from retrieval and are
	// rebuilt by reconciliation.
	ProjectionVersion int32 = 1
	// NormalizationVersion is the current normalization version. Bump it when
	// the normalization applied to title, tags, and body text changes.
	NormalizationVersion int32 = 1
)

// foldCaser applies Unicode case folding. It is immutable and safe for
// concurrent use.
var foldCaser = cases.Fold()

// normalize applies normalization version 1: Unicode NFC, case folding, and
// collapsing every whitespace run to a single space, trimmed at the edges.
func normalize(s string) string {
	return strings.TrimSpace(normalizeChunk(s))
}

// normalizeChunk applies normalization version 1 to one text chunk without
// trimming the edges: a chunk's leading or trailing whitespace is the word
// separator between adjacent chunks and must survive as exactly one space.
func normalizeChunk(s string) string {
	s = norm.NFC.String(s)
	s = foldCaser.String(s)
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inSpace = true
			continue
		}
		if inSpace {
			b.WriteByte(' ')
			inSpace = false
		}
		b.WriteRune(r)
	}
	if inSpace {
		b.WriteByte(' ')
	}
	return b.String()
}

// contentHash returns the hex SHA-256 of the source memo content. Title and
// tags derive from the content, so the content hash covers them.
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// buildDocument projects a memo into its derived search document: the
// H1-derived title, the extracted tags, and a plain-text projection of the
// markdown content, all normalized, plus the source-span mapping from
// projected content byte ranges back to source content byte ranges.
func (s *Service) buildDocument(m *store.Memo) (*store.AISearchDocument, error) {
	content := []byte(m.Content)
	extracted, err := s.markdown.ExtractAll(content)
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract memo metadata")
	}
	chunks, err := s.markdown.ExtractText(content)
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract memo text")
	}

	// Assemble the normalized body from the chunks. Adjacent chunks carry
	// their word-separating whitespace at the edges; a separator is inserted
	// wherever neither edge provides one (block boundaries and whitespace-only
	// chunks). The body never starts or ends with a space.
	var body strings.Builder
	spans := []*store.AISearchDocumentSpan{}
	pendingSpace := false
	prevTrailingSpace := false
	for _, chunk := range chunks {
		collapsed := normalizeChunk(chunk.Text)
		text := strings.TrimSpace(collapsed)
		if text == "" {
			// A whitespace-only chunk still separates the words around it.
			if body.Len() > 0 {
				pendingSpace = true
			}
			continue
		}
		if body.Len() > 0 && (chunk.Block || pendingSpace || prevTrailingSpace || collapsed[0] == ' ') {
			body.WriteByte(' ')
		}
		start := body.Len()
		body.WriteString(text)
		if chunk.HasSource {
			spans = append(spans, &store.AISearchDocumentSpan{
				ContentStart: start,
				ContentEnd:   body.Len(),
				SourceStart:  chunk.SourceStart,
				SourceEnd:    chunk.SourceEnd,
			})
		}
		pendingSpace = false
		prevTrailingSpace = collapsed[len(collapsed)-1] == ' '
	}

	tags := []string{}
	seen := map[string]struct{}{}
	for _, tag := range extracted.Tags {
		normalized := normalize(tag)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		tags = append(tags, normalized)
	}

	return &store.AISearchDocument{
		MemoID:               m.ID,
		MemoUID:              m.UID,
		MemoUpdatedTs:        m.UpdatedTs,
		ContentHash:          contentHash(m.Content),
		ProjectionVersion:    ProjectionVersion,
		NormalizationVersion: NormalizationVersion,
		Title:                normalize(extracted.Property.Title),
		Tags:                 tags,
		Content:              body.String(),
		Spans:                spans,
	}, nil
}
