package search

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/markdown"
	"github.com/usememos/memos/store"
)

func TestNormalize(t *testing.T) {
	require.Equal(t, "hello wörld", normalize("  Hello\t WÖRLD \n"))
	require.Equal(t, "a b c", normalize("a \n\t b   c"))
	require.Equal(t, "é", normalize("é")) // decomposed e + combining accent → NFC é
	require.Equal(t, "strasse", normalize("STRASSE"))
	require.Equal(t, "", normalize(" \n\t "))
}

func TestBuildDocument(t *testing.T) {
	markdownService := markdown.NewService(markdown.WithTagExtension())
	service := NewService(nil, nil, markdownService)

	t.Run("projects title, tags, body, and spans", func(t *testing.T) {
		m := &store.Memo{
			ID:        7,
			UID:       "memo-7",
			UpdatedTs: 1234,
			Content:   "# Trip Notes\n\nWe went to #Paris in 2024.\n\n```\ncode here\n```",
		}
		document, err := service.buildDocument(m)
		require.NoError(t, err)

		require.Equal(t, int32(7), document.MemoID)
		require.Equal(t, "memo-7", document.MemoUID)
		require.Equal(t, int64(1234), document.MemoUpdatedTs)
		require.Equal(t, contentHash(m.Content), document.ContentHash)
		require.Equal(t, ProjectionVersion, document.ProjectionVersion)
		require.Equal(t, NormalizationVersion, document.NormalizationVersion)
		require.Equal(t, "trip notes", document.Title)
		require.Equal(t, []string{"paris"}, document.Tags)
		require.Equal(t, "trip notes we went to #paris in 2024. code here", document.Content)

		// Spans are ordered, non-overlapping, and inside the content.
		previousEnd := 0
		for _, span := range document.Spans {
			require.GreaterOrEqual(t, span.ContentStart, previousEnd)
			require.Less(t, span.ContentStart, span.ContentEnd)
			require.LessOrEqual(t, span.ContentEnd, len(document.Content))
			previousEnd = span.ContentEnd
		}

		// The span covering "#paris" maps back to "#Paris" in the source.
		idx := strings.Index(document.Content, "#paris")
		require.NotEqual(t, -1, idx)
		var hit *store.AISearchDocumentSpan
		for _, span := range document.Spans {
			if span.ContentStart <= idx && idx < span.ContentEnd {
				hit = span
			}
		}
		require.NotNil(t, hit)
		sourceOffset := hit.SourceStart + (idx - hit.ContentStart)
		require.True(t, strings.EqualFold(m.Content[sourceOffset:sourceOffset+len("#paris")], "#paris"),
			"span should map the match back to the source position, got %q", m.Content[sourceOffset:])
	})

	t.Run("no H1 means no title", func(t *testing.T) {
		document, err := service.buildDocument(&store.Memo{ID: 8, UID: "memo-8", Content: "just a paragraph"})
		require.NoError(t, err)
		require.Equal(t, "", document.Title)
		require.Equal(t, "just a paragraph", document.Content)
	})

	t.Run("word spacing inside a block is preserved", func(t *testing.T) {
		for content, want := range map[string]string{
			"rebuild me":      "rebuild me",
			"foo *bar* baz":   "foo bar baz",
			"foo**bar**baz":   "foobarbaz",
			"foo `x y` baz":   "foo x y baz",
			"multi \n\t line": "multi line",
		} {
			document, err := service.buildDocument(&store.Memo{ID: 11, UID: "memo-11", Content: content})
			require.NoError(t, err)
			require.Equal(t, want, document.Content, "content %q", content)
		}
	})

	t.Run("tags deduplicate after normalization", func(t *testing.T) {
		document, err := service.buildDocument(&store.Memo{ID: 9, UID: "memo-9", Content: "#Paris and #PARIS and #paris"})
		require.NoError(t, err)
		require.Equal(t, []string{"paris"}, document.Tags)
	})

	t.Run("empty content yields an empty document", func(t *testing.T) {
		document, err := service.buildDocument(&store.Memo{ID: 10, UID: "memo-10", Content: ""})
		require.NoError(t, err)
		require.Equal(t, "", document.Title)
		require.Empty(t, document.Tags)
		require.Equal(t, "", document.Content)
		require.Empty(t, document.Spans)
	})
}
